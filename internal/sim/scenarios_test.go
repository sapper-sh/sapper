package sim

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sapper-unbuilt/sapper/internal/manifest"
	"github.com/sapper-unbuilt/sapper/internal/ops"
	"github.com/sapper-unbuilt/sapper/internal/paths"
	"github.com/sapper-unbuilt/sapper/internal/report"
)

// damageDrill classifies a drill run after the repository was damaged: anything but
// SURVIVED is caught, because the damage was real.
func damageDrill(t *testing.T, e *env, id, name, damage string) {
	rep, err := e.breach(ops.DrillOptions{})
	o := outcome{ID: id, Name: name, Expected: "drill does not report SURVIVED"}
	o.Actual = fmt.Sprintf("%s after %s", verdict(rep), damage)
	if err != nil && !errors.Is(err, ops.ErrBreached) {
		o.Actual += ": " + firstLine(report.CleanLines(err.Error()))
	}
	o.Result = Caught
	if verdict(rep) == report.Survived {
		o.Result = Missed
	}
	record(t, o)
}

// largestPack returns the biggest file under data/, which holds file contents.
func (e *env) largestPack() string {
	var best string
	var size int64 = -1
	for _, p := range e.repoFiles("data") {
		if st, err := os.Stat(p); err == nil && st.Size() > size {
			best, size = p, st.Size()
		}
	}
	if best == "" {
		e.t.Fatal("no pack files")
	}
	return best
}

func TestS01BitRotInPack(t *testing.T) {
	e := newEnv(t)
	e.backup()
	p := e.largestPack()
	writable(t, p)
	b, _ := os.ReadFile(p)
	b[len(b)/2] ^= 0x01
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	damageDrill(t, e, "S01", "Bit rot: one bit flipped in the largest pack (the tree pack, also in restic's cache)", "one flipped bit")
}

func TestS02DeletedPack(t *testing.T) {
	e := newEnv(t)
	e.backup()
	if err := os.Remove(e.largestPack()); err != nil {
		t.Fatal(err)
	}
	damageDrill(t, e, "S02", "Pack file deleted from the repository", "pack deleted")
}

func TestS03TruncatedPack(t *testing.T) {
	e := newEnv(t)
	e.backup()
	p := e.largestPack()
	st, _ := os.Stat(p)
	writable(t, p)
	if err := os.Truncate(p, st.Size()/2); err != nil {
		t.Fatal(err)
	}
	damageDrill(t, e, "S03", "Pack file truncated (interrupted upload, full disk)", "pack truncated")
}

func TestS04DeletedIndex(t *testing.T) {
	e := newEnv(t)
	e.backup()
	for _, p := range e.repoFiles("index") {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	damageDrill(t, e, "S04", "Index files deleted", "index deleted")
}

func TestS05AttackerDeletesSnapshots(t *testing.T) {
	e := newEnv(t)
	e.backup()
	for _, p := range e.repoFiles("snapshots") {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	damageDrill(t, e, "S05", "Attacker deletes every snapshot (Code Spaces)", "snapshots deleted")
}

// encryptLive overwrites every live file with random bytes of the same size, as ransomware does.
func (e *env) encryptLive(fraction float64) int {
	m, err := manifest.Build(e.bp.VolumePaths())
	if err != nil {
		e.t.Fatal(err)
	}
	var files []string
	for p, en := range m.Entries {
		if en.Kind == manifest.KindFile {
			files = append(files, p)
		}
	}
	n := int(float64(len(files)) * fraction)
	for _, p := range files[:n] {
		st, _ := os.Stat(p)
		b := make([]byte, st.Size())
		_, _ = rand.Read(b)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			e.t.Fatal(err)
		}
	}
	return n
}

// anomalyBackup runs a backup after live data was damaged and classifies it: a backup
// that goes through without complaint is missed.
func anomalyBackup(t *testing.T, e *env, id, name string) {
	_, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{})
	o := outcome{ID: id, Name: name, Expected: "backup refuses with a warning until --force"}
	switch {
	case err == nil:
		o.Result, o.Actual = Missed, "backup accepted the damaged data silently"
	case errors.Is(err, ops.ErrSuspicious):
		o.Result, o.Actual = Caught, "backup refused: "+firstLine(err.Error())
	default:
		o.Result, o.Actual = Partial, "backup failed for another reason: "+firstLine(err.Error())
	}
	record(t, o)
}

func TestS06RansomwareEncryptsLiveData(t *testing.T) {
	e := newEnv(t)
	e.backup()
	e.encryptLive(1)
	anomalyBackup(t, e, "S06", "Ransomware encrypts live data, then the nightly backup runs")
}

func TestS07UnmountedVolume(t *testing.T) {
	e := newEnv(t)
	e.backup()
	// An unmounted mount point is an empty directory.
	p := e.bp.VolumePaths()[0]
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	anomalyBackup(t, e, "S07", "Volume not mounted: backup sees an empty directory")
}

func TestS08MassDeletion(t *testing.T) {
	e := newEnv(t)
	e.backup()
	// rm -rf * at the root of the project, stopped part way (Toy Story 2).
	for _, p := range e.bp.VolumePaths() {
		if err := os.RemoveAll(filepath.Join(p, "docs")); err != nil {
			t.Fatal(err)
		}
	}
	anomalyBackup(t, e, "S08", "rm -rf deletes most files before the backup (Toy Story 2)")
}

func TestS09ManifestLostWithHost(t *testing.T) {
	e := newEnv(t)
	e.backup()
	// A new machine: same blueprint and repository, empty state directory.
	t.Setenv(paths.EnvStateDir, t.TempDir())
	rep, err := e.breach(ops.DrillOptions{})
	o := outcome{ID: "S09", Name: "Host lost: drill from a new machine with no local manifest", Expected: "drill still verifies, using the manifest copy kept in the repository"}
	o.Actual = verdict(rep)
	if err != nil && !errors.Is(err, ops.ErrBreached) {
		o.Actual += ": " + firstLine(err.Error())
	}
	switch {
	case rep.Survived() && err == nil:
		o.Result = Caught
	case verdict(rep) == report.NoVerdict:
		o.Result = Partial
	default:
		o.Result = Missed
	}
	record(t, o)
}

func TestS10ManifestEdited(t *testing.T) {
	e := newEnv(t)
	rec := e.backup()
	sd, _ := ops.StateDir(e.bp)
	mp := filepath.Join(sd, "manifests", rec.Snapshot+".json")
	m, err := manifest.Load(mp)
	if err != nil {
		t.Fatal(err)
	}
	for p, en := range m.Entries {
		if en.Kind == manifest.KindFile {
			en.SHA256 = strings.Repeat("0", 64)
			m.Entries[p] = en
			break
		}
	}
	if err := manifest.Save(mp, m); err != nil {
		t.Fatal(err)
	}
	rep, _ := e.breach(ops.DrillOptions{})
	o := outcome{ID: "S10", Name: "Local manifest edited after the backup", Expected: "drill does not report SURVIVED"}
	o.Actual = verdict(rep)
	o.Result = Caught
	if rep.Survived() {
		o.Result = Missed
	}
	record(t, o)
}

func TestS11ReportEdited(t *testing.T) {
	e := newEnv(t)
	e.backup()
	if _, err := e.breach(ops.DrillOptions{}); err != nil {
		t.Fatal(err)
	}
	store, _ := ops.Reports(e.bp)
	all, _ := store.List()
	p := all[0].Path
	b, _ := os.ReadFile(p)
	writable(t, p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(b), `"took_ms": `, `"took_ms": 1`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	all, _ = store.List()
	issues := report.VerifyChain(all, nil, "")
	o := outcome{ID: "S11", Name: "Saved drill report edited", Expected: "reports verify flags the edit"}
	o.Actual = fmt.Sprintf("%d chain issue(s)", len(issues))
	o.Result = Caught
	if len(issues) == 0 {
		o.Result = Missed
	}
	record(t, o)
}

func TestS12NewestReportDeleted(t *testing.T) {
	e := newEnv(t)
	e.backup()
	var head string
	for range 2 {
		rep, err := e.breach(ops.DrillOptions{})
		if err != nil {
			t.Fatal(err)
		}
		head = rep.Report.Hash[:12]
	}
	store, _ := ops.Reports(e.bp)
	all, _ := store.List()
	last := all[len(all)-1].Path
	_ = os.Remove(last)
	_ = os.Remove(strings.TrimSuffix(last, ".json") + ".md")
	all, _ = store.List()
	issues := report.VerifyChain(all, nil, head)
	o := outcome{ID: "S12", Name: "Newest drill report deleted to hide a failure", Expected: "reports verify --head flags the truncation"}
	o.Actual = fmt.Sprintf("%d chain issue(s) with --head", len(issues))
	o.Result = Caught
	if len(issues) == 0 {
		o.Result = Missed
	}
	record(t, o)
}

func TestS13DatabaseWrittenDuringBackup(t *testing.T) {
	e := newEnv(t)
	db := filepath.Join(e.bp.VolumePaths()[0], "app.db")
	ctx, cancel := context.WithCancel(e.ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ctx.Err() == nil; i++ {
			_ = os.WriteFile(db, []byte(fmt.Sprintf("SQLite format 3\x00 page %d", i)), 0o644)
			time.Sleep(time.Millisecond)
		}
	}()
	_, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{})
	cancel()
	<-done
	o := outcome{ID: "S13", Name: "Database file written while the backup runs", Expected: "backup refuses; no manifest is saved for a torn copy"}
	if err != nil && strings.Contains(err.Error(), "changed during backup") {
		o.Result, o.Actual = Caught, "backup refused: volumes changed during backup"
	} else {
		o.Result, o.Actual = Missed, fmt.Sprintf("backup returned %v", err)
	}
	record(t, o)
}

func TestS14BackupKilledMidRun(t *testing.T) {
	e := newEnv(t)
	first := e.backup()
	// Enough new data that restic is still running when it is killed.
	big := make([]byte, 64<<20)
	_, _ = rand.Read(big)
	if err := os.WriteFile(filepath.Join(e.bp.VolumePaths()[0], "big.bin"), big, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(e.ctx, 300*time.Millisecond)
	_, err := ops.Backup(ctx, e.bp, e.c, ops.BackupOptions{})
	cancel()
	o := outcome{ID: "S14", Name: "Backup killed mid-run (SIGKILL), lock left behind", Expected: "no manifest for the partial run; next backup and drill work"}
	if err == nil {
		o.Result, o.Actual = Partial, "backup finished before it could be killed"
		record(t, o)
		return
	}
	rec, berr := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{})
	if berr != nil {
		o.Result, o.Actual = Missed, "next backup failed: "+firstLine(berr.Error())
		record(t, o)
		return
	}
	rep, derr := e.breach(ops.DrillOptions{})
	if derr == nil && rep.Snapshot == rec.Snapshot && rec.Snapshot != first.Snapshot {
		o.Result, o.Actual = Caught, "killed run left no record; next backup and drill "+verdict(rep)
	} else {
		o.Result, o.Actual = Missed, fmt.Sprintf("drill after kill: %s %v", verdict(rep), derr)
	}
	record(t, o)
}

// holdLock starts a restic process that keeps a lock on the repository until stop is called.
func (e *env) holdLock() (stop func()) {
	cmd := exec.Command(e.c.Bin, "backup", "--stdin", "--stdin-filename", "hold")
	cmd.Env = append(os.Environ(), "RESTIC_REPOSITORY="+e.repo, "RESTIC_PASSWORD="+password)
	w, err := cmd.StdinPipe()
	if err != nil {
		e.t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		e.t.Fatal(err)
	}
	for i := 0; i < 100 && len(e.repoFiles("locks")) == 0; i++ {
		time.Sleep(50 * time.Millisecond)
	}
	return func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); w.Close() }
}

func TestS15RepositoryLocked(t *testing.T) {
	e := newEnv(t)
	e.bp.Backup.KeepLast = 1 // so the backup has to prune, which needs an exclusive lock
	e.backup()
	stop := e.holdLock()
	defer stop()
	_, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{})
	o := outcome{ID: "S15", Name: "Repository locked by another restic process", Expected: "backup stops and says how to find and clear the lock"}
	switch {
	case err == nil:
		o.Result, o.Actual = Missed, "backup reported success while the repository was locked"
	case errors.Is(err, ops.ErrLocked) && strings.Contains(err.Error(), "unlock"):
		o.Result, o.Actual = Caught, firstLine(err.Error())
	default:
		o.Result, o.Actual = Partial, "raw restic error: "+firstLine(err.Error())
	}
	record(t, o)
}

func TestS16WrongPassword(t *testing.T) {
	e := newEnv(t)
	e.backup()
	e.c = e.client("not-the-password")
	rep, err := e.breach(ops.DrillOptions{})
	o := outcome{ID: "S16", Name: "Wrong password or lost key", Expected: "NO VERDICT naming the password as the cause"}
	o.Actual = verdict(rep)
	if err != nil {
		o.Actual += ": " + firstLine(err.Error())
	}
	switch {
	case verdict(rep) == report.Survived:
		o.Result = Missed
	case errors.Is(err, ops.ErrWrongPassword):
		o.Result = Caught
	default:
		o.Result = Partial
	}
	record(t, o)
}

// wrapRestic points the client at a shell script that runs restic with a prefix.
func (e *env) wrapRestic(script string) {
	p := filepath.Join(e.t.TempDir(), "restic-wrapper")
	body := "#!/bin/sh\nREAL=" + e.c.Bin + "\n" + script + "\n"
	if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
		e.t.Fatal(err)
	}
	e.c.Bin = p
}

func TestS17DiskFullDuringRestore(t *testing.T) {
	e := newEnv(t)
	e.backup()
	real := e.c.Bin
	// SIMULATED: no root for a tiny tmpfs, so restic runs with a 4 KiB file size limit,
	// which fails writes the way a full disk does.
	e.wrapRestic(`if [ "$1" = restore ]; then ulimit -f 8; fi; exec "$REAL" "$@"`)
	rep, err := e.breach(ops.DrillOptions{})
	e.c.Bin = real
	o := outcome{ID: "S17", Name: "Disk fills up during the drill restore (simulated: file size limit)", Expected: "NO VERDICT, never SURVIVED"}
	o.Actual = verdict(rep)
	if err != nil {
		o.Actual += ": " + firstLine(err.Error())
	}
	o.Result = Caught
	if verdict(rep) == report.Survived {
		o.Result = Missed
	}
	record(t, o)
}

func TestS18ClockSkew(t *testing.T) {
	e := newEnv(t)
	real := e.c.Bin
	// SIMULATED: one backup ran on a host whose clock was a year fast.
	e.wrapRestic(`if [ "$1" = backup ]; then shift; exec "$REAL" backup --time "` + time.Now().AddDate(1, 0, 0).Format("2006-01-02 15:04:05") + `" "$@"; fi; exec "$REAL" "$@"`)
	skewed := e.backup()
	e.c.Bin = real
	e.write(filepath.Join(e.bp.VolumePaths()[0], "docs", "new.txt"), "written after the clock was fixed")
	fresh := e.backup()
	rep, _ := e.breach(ops.DrillOptions{})
	o := outcome{ID: "S18", Name: "Clock skew: a snapshot stamped a year in the future", Expected: "drill warns about the future timestamp instead of trusting it as newest"}
	o.Actual = fmt.Sprintf("drilled %s (skewed %s, fresh %s)", rep.Snapshot[:min(8, len(rep.Snapshot))], skewed.Snapshot[:8], fresh.Snapshot[:8])
	warned := rep.Report != nil && strings.Contains(strings.Join(rep.Report.Warnings, " "), "future")
	switch {
	case warned:
		o.Result = Caught
		o.Actual += "; warned"
	default:
		o.Result = Missed
	}
	record(t, o)
}

func TestS19RetentionPrunesVerifiedSnapshot(t *testing.T) {
	e := newEnv(t)
	e.bp.Backup.KeepLast = 1
	good := e.backup()
	if _, err := e.breach(ops.DrillOptions{}); err != nil {
		t.Fatal(err)
	}
	e.encryptLive(1)
	_, _ = ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{Force: true})
	o := outcome{ID: "S19", Name: "Retention prunes the only verified snapshot", Expected: "the last snapshot that passed a drill is kept"}
	snaps, err := e.c.Snapshots(e.ctx, ops.Tag)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, s := range snaps {
		kept = kept || s.ID == good.Snapshot
	}
	if kept {
		o.Result, o.Actual = Caught, "verified snapshot kept"
	} else {
		o.Result, o.Actual = Missed, "verified snapshot pruned by keep_last 1"
	}
	record(t, o)
}

func TestS20OlderSnapshotDamaged(t *testing.T) {
	e := newEnv(t)
	e.backup()
	before := map[string]bool{}
	for _, p := range e.repoFiles("data") {
		before[p] = true
	}
	x := filepath.Join(e.bp.VolumePaths()[0], "only-in-a.bin")
	b := make([]byte, 1<<20)
	_, _ = rand.Read(b)
	if err := os.WriteFile(x, b, 0o644); err != nil {
		t.Fatal(err)
	}
	older := e.backup()
	var aPacks []string
	for _, p := range e.repoFiles("data") {
		if !before[p] {
			aPacks = append(aPacks, p)
		}
	}
	_ = os.Remove(x)
	// Removing the 1 MiB file halves the volume, which backup treats as suspicious.
	if _, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{Force: true}); err != nil {
		t.Fatal(err)
	}
	for _, p := range aPacks {
		_ = os.Remove(p)
	}
	rep, err := e.breach(ops.DrillOptions{Sample: 3})
	o := outcome{ID: "S20", Name: "An older snapshot is damaged; the latest is fine", Expected: "drill samples older snapshots and reports the damaged one"}
	o.Actual = verdict(rep)
	if rep.Report != nil && len(rep.Report.Older) > 0 {
		for _, s := range rep.Report.Older {
			if s.ID == older.Snapshot && s.Verdict != report.Survived {
				o.Result, o.Actual = Caught, fmt.Sprintf("%s; older snapshot %s %s", verdict(rep), older.Snapshot[:8], s.Verdict)
			}
		}
	}
	if o.Result == "" {
		o.Result = Missed
		o.Actual += fmt.Sprintf(" (err %v); older snapshot never drilled", err)
	}
	record(t, o)
}

func TestS21BackupsSilentlyStopped(t *testing.T) {
	e := newEnv(t)
	e.backup()
	later := time.Now().Add(10 * 24 * time.Hour)
	rep, _ := e.breach(ops.DrillOptions{Now: func() time.Time { return later }})
	o := outcome{ID: "S21", Name: "Backups silently stopped for 10 days (GitLab)", Expected: "drill reports the RPO target missed"}
	missed := rep.Report != nil && strings.Contains(strings.Join(rep.Report.TargetsMissed(), ","), "RPO")
	o.Actual = fmt.Sprintf("%s; targets missed %v", verdict(rep), rep.Report.TargetsMissed())
	o.Result = Caught
	if !missed {
		o.Result = Missed
	}
	record(t, o)
}

func TestS22RepositoryVanished(t *testing.T) {
	e := newEnv(t)
	e.backup()
	if err := os.RemoveAll(e.repo); err != nil {
		t.Fatal(err)
	}
	_, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{})
	o := outcome{ID: "S22", Name: "Repository deleted; next backup would quietly start a new empty one", Expected: "backup refuses to create a new repository where one existed"}
	if err == nil {
		o.Result, o.Actual = Missed, "backup initialised a fresh repository; the loss went unreported"
	} else {
		o.Result, o.Actual = Caught, firstLine(err.Error())
	}
	record(t, o)
}

func TestS23RepositoryDeletableFromHost(t *testing.T) {
	e := newEnv(t)
	e.backup()
	warn := ops.Preflight(e.ctx, e.bp, e.c, ops.PreflightOptions{})
	o := outcome{ID: "S23", Name: "Ransomware on this host could delete the repository (Conti, Veeam targeting)", Expected: "pre-flight warns that the repository is deletable from this host"}
	o.Result, o.Actual = Missed, "no warning"
	for _, c := range warn.Checks {
		if c.Name == "append-only" && !c.OK {
			o.Result, o.Actual = Caught, c.Detail
		}
	}
	record(t, o)
}

func TestS24DiskFullPreflight(t *testing.T) {
	e := newEnv(t)
	e.backup()
	// SIMULATED: the free-space probe reports 1 KiB free.
	pf := ops.Preflight(e.ctx, e.bp, e.c, ops.PreflightOptions{FreeBytes: func(string) (uint64, error) { return 1024, nil }})
	o := outcome{ID: "S24", Name: "Not enough free space for the drill restore (simulated statfs)", Expected: "pre-flight fails before restoring"}
	o.Result, o.Actual = Missed, "no space check"
	for _, c := range pf.Checks {
		if c.Name == "space" && !c.OK {
			o.Result, o.Actual = Caught, c.Detail
		}
	}
	record(t, o)
}

func TestS25ForgedManifestCopy(t *testing.T) {
	e := newEnv(t)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{SignKey: priv})
	if err != nil {
		t.Fatal(err)
	}
	// An attacker who has the repository password stores a newer, unsigned manifest copy
	// for the same snapshot, and the drill runs from a new machine with no local manifest.
	body := `{"roots":[],"entries":{}}`
	sum := sha256.Sum256([]byte(body))
	forged := fmt.Sprintf(`{"schema":1,"snapshot":%q,"sha256":%q,"manifest":%s}`, rec.Snapshot, hex.EncodeToString(sum[:]), body)
	if _, err := e.c.SaveBlob(e.ctx, []byte(forged), []string{ops.ManifestTag, "for:" + rec.Snapshot}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(paths.EnvStateDir, t.TempDir())
	rep, err := e.breach(ops.DrillOptions{ManifestKey: pub})
	o := outcome{ID: "S25", Name: "Attacker with the repository password plants a forged manifest copy", Expected: "with --pub-key, the drill refuses the unsigned copy"}
	o.Actual = verdict(rep)
	if err != nil {
		o.Actual += ": " + firstLine(err.Error())
	}
	o.Result = Missed
	if errors.Is(err, ops.ErrManifestSignature) {
		o.Result = Caught
	}
	record(t, o)
}

func TestS26SlowRansomware(t *testing.T) {
	e := newEnv(t)
	e.backup()
	// Encrypts a quarter of the files per night, staying under the mass-change threshold.
	n := e.encryptLive(0.25)
	_, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{})
	o := outcome{ID: "S26", Name: "Slow ransomware: a quarter of files encrypted before each backup", Expected: "a warning (known gap: below the 50% threshold)"}
	switch {
	case errors.Is(err, ops.ErrSuspicious):
		o.Result, o.Actual = Caught, "backup refused"
	case err == nil:
		o.Result, o.Actual = Gap, fmt.Sprintf("backup accepted %d encrypted files; the drill will still pass because the backup matches what was on disk", n)
	default:
		o.Result, o.Actual = Partial, firstLine(err.Error())
	}
	record(t, o)
}
