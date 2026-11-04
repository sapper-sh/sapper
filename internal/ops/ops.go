// Package ops implements backup and breach drills for a fortress.
package ops

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sapper-sh/sapper/internal/blueprint"
	"github.com/sapper-sh/sapper/internal/manifest"
	"github.com/sapper-sh/sapper/internal/paths"
	"github.com/sapper-sh/sapper/internal/report"
	"github.com/sapper-sh/sapper/internal/restic"
)

// Tag marks snapshots created by sapper.
const Tag = "sapper"

// Restic is the subset of the restic client ops needs.
type Restic interface {
	EnsureRepo(ctx context.Context) (bool, error)
	Backup(ctx context.Context, paths, tags []string) (string, error)
	Latest(ctx context.Context, tag string) (restic.Snapshot, error)
	Restore(ctx context.Context, id, target string) error
	Forget(ctx context.Context, tag string, keepLast int) error
}

// StateDir returns the per-user state directory for bp.
func StateDir(bp *blueprint.Blueprint) (string, error) {
	return paths.FortressState(bp.Fortress, bp.Dir)
}

func statePath(bp *blueprint.Blueprint, elem ...string) (string, error) {
	d, err := StateDir(bp)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{d}, elem...)...), nil
}

// BackupRecord is stored after a successful backup.
type BackupRecord struct {
	Snapshot string    `json:"snapshot"`
	Time     time.Time `json:"time"`
	Entries  int       `json:"entries"`
	Created  bool      `json:"repo_created"`
	// ManifestCopy is the snapshot holding the manifest copy in the repository.
	ManifestCopy string `json:"manifest_copy,omitempty"`
	// Forced lists the anomalies that --force overrode.
	Forced []Anomaly `json:"forced,omitempty"`
	// Forgotten lists snapshots removed by retention.
	Forgotten []string `json:"forgotten,omitempty"`
}

// BackupOptions tune a backup.
type BackupOptions struct {
	// Force backs up even when live data looks damaged, and lets backup create a new
	// repository where this host already had one.
	Force bool
	// SignKey, when set, signs the manifest copy stored in the repository.
	SignKey ed25519.PrivateKey
}

// ErrRepoMissing is returned when this host has backed up before but the repository is gone.
var ErrRepoMissing = errors.New("the repository this fortress backed up to is gone")

// Backup snapshots every volume and stores a checksum manifest keyed by snapshot ID, locally
// and as a copy inside the repository. It fails if the volumes change while the backup runs,
// because the manifest would not match, and refuses data that looks damaged unless forced.
func Backup(ctx context.Context, bp *blueprint.Blueprint, r Restic, opt BackupOptions) (BackupRecord, error) {
	paths := bp.VolumePaths()
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return BackupRecord{}, fmt.Errorf("volume %s: %w", p, err)
		}
		if !st.IsDir() {
			return BackupRecord{}, fmt.Errorf("volume %s: not a directory", p)
		}
	}
	before, err := manifest.Build(paths)
	if err != nil {
		return BackupRecord{}, err
	}
	last, hadBackup := LastBackup(bp)
	var prev *manifest.Manifest
	if hadBackup && validID(last.Snapshot) {
		if mp, err := statePath(bp, "manifests", last.Snapshot+".json"); err == nil {
			prev, _ = manifest.Load(mp)
		}
	}
	anomalies := Anomalies(prev, before, paths)
	if len(anomalies) > 0 && !opt.Force {
		return BackupRecord{}, fmt.Errorf("%w: %s. Nothing was backed up. If this is expected, run `sapper backup --force`", ErrSuspicious, describe(anomalies))
	}
	if rc, ok := r.(repoChecker); ok && hadBackup && !opt.Force {
		exists, err := rc.RepoExists(ctx)
		if err != nil {
			return BackupRecord{}, err
		}
		if !exists {
			return BackupRecord{}, fmt.Errorf("%w: snapshot %s was taken on %s, but %s has no repository now. It may have been deleted. Check the storage before anything else; `sapper backup --force` starts a new, empty repository",
				ErrRepoMissing, short(last.Snapshot), last.Time.Format(time.RFC3339), bp.RedactedRepo())
		}
	}
	created, err := r.EnsureRepo(ctx)
	if err != nil {
		return BackupRecord{}, err
	}
	id, err := r.Backup(ctx, paths, []string{Tag, "fortress:" + bp.Fortress})
	if err != nil {
		return BackupRecord{}, err
	}
	after, err := manifest.Build(paths)
	if err != nil {
		return BackupRecord{}, err
	}
	if !manifest.Equal(before, after) {
		return BackupRecord{}, fmt.Errorf("volumes changed during backup of snapshot %s; stop the apps (docker compose stop) and run backup again", short(id))
	}
	if !validID(id) {
		return BackupRecord{}, fmt.Errorf("restic returned an invalid snapshot id %q", id)
	}
	mp, err := statePath(bp, "manifests", id+".json")
	if err != nil {
		return BackupRecord{}, err
	}
	if err := manifest.Save(mp, before); err != nil {
		return BackupRecord{}, err
	}
	rec := BackupRecord{Snapshot: id, Time: time.Now().UTC(), Entries: len(before.Entries), Created: created}
	if opt.Force {
		rec.Forced = anomalies
	}
	if rec.ManifestCopy, err = saveCopy(ctx, r, bp.Fortress, id, before, opt.SignKey); err != nil {
		return rec, fmt.Errorf("snapshot %s saved, storing the manifest copy in the repository failed: %w", short(id), err)
	}
	if err := writeState(bp, "last-backup.json", rec); err != nil {
		return rec, err
	}
	if rec.Forgotten, err = retain(ctx, bp, r); err != nil {
		return rec, fmt.Errorf("snapshot %s saved, retention failed: %w", short(id), err)
	}
	return rec, writeState(bp, "last-backup.json", rec)
}

// retain applies keep_last, but never forgets the newest snapshot that passed a drill:
// if every recent backup is of damaged data, the last good one is still there.
func retain(ctx context.Context, bp *blueprint.Blueprint, r Restic) ([]string, error) {
	keep := bp.Backup.KeepLast
	if keep <= 0 {
		return nil, nil
	}
	sl, ok1 := r.(snapshotLister)
	fg, ok2 := r.(idForgetter)
	if !ok1 || !ok2 {
		return nil, r.Forget(ctx, Tag, keep)
	}
	snaps, err := sl.Snapshots(ctx, Tag)
	if err != nil {
		return nil, err
	}
	verified, _ := LastVerified(bp)
	mine := snaps[:0:0]
	for _, s := range snaps {
		if hasTag(s, "fortress:"+bp.Fortress) && samePaths(s.Paths, bp.VolumePaths()) {
			mine = append(mine, s)
		}
	}
	snaps = mine
	var drop []string
	for i, s := range snaps {
		if i >= len(snaps)-keep || s.ID == verified.Snapshot || !validID(s.ID) {
			continue
		}
		drop = append(drop, s.ID)
	}
	if len(drop) == 0 {
		return nil, nil
	}
	dropped := map[string]bool{}
	for _, id := range drop {
		dropped[id] = true
	}
	if copies, err := sl.Snapshots(ctx, ManifestTag); err == nil {
		for _, c := range copies {
			if dropped[copyTarget(c)] && validID(c.ID) {
				drop = append(drop, c.ID)
			}
		}
	}
	if err := fg.ForgetIDs(ctx, drop); err != nil {
		return nil, err
	}
	for id := range dropped {
		if mp, err := statePath(bp, "manifests", id+".json"); err == nil {
			_ = os.Remove(mp)
		}
	}
	var out []string
	for _, s := range snaps {
		if dropped[s.ID] {
			out = append(out, s.ID)
		}
	}
	return out, nil
}

func samePaths(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a, b = append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if filepath.Clean(a[i]) != filepath.Clean(b[i]) {
			return false
		}
	}
	return true
}

// VerifiedRecord names the newest snapshot that survived a drill.
type VerifiedRecord struct {
	Snapshot string    `json:"snapshot"`
	Time     time.Time `json:"time"`
}

// LastVerified reads the newest snapshot that survived a drill, if any.
func LastVerified(bp *blueprint.Blueprint) (VerifiedRecord, bool) {
	var r VerifiedRecord
	return r, readState(bp, "last-verified.json", &r) == nil
}

// DrillOptions tune a breach drill.
type DrillOptions struct {
	// TempRoot is where the throwaway restore directory is created; empty means os.TempDir().
	TempRoot string
	// Keep leaves the restored copy on disk for inspection.
	Keep bool
	// BeforeVerify runs after restore and before verification. Tests use it to inject damage.
	BeforeVerify func(restoreDir string) error
	// SignKey, when set, signs the evidence report.
	SignKey ed25519.PrivateKey
	// Now overrides the clock for tests.
	Now func() time.Time
	// Snapshot drills this snapshot (ID or unique prefix) instead of the latest.
	Snapshot string
	// Sample also drills up to this many randomly chosen older snapshots.
	Sample int
	// ManifestKey, when set, requires the manifest copy in the repository to be signed by it.
	ManifestKey ed25519.PublicKey
	// FreeBytes reports free space under a directory; nil uses the file system. Tests
	// replace it to simulate a full disk.
	FreeBytes func(dir string) (uint64, error)
}

// ErrNoSpace is returned when the drill's temp directory cannot hold the restore.
var ErrNoSpace = errors.New("not enough free space for the drill restore")

// clockSlack is how far in the future a snapshot may be stamped before the drill warns.
const clockSlack = 5 * time.Minute

// DrillReport is the outcome of a breach drill.
type DrillReport struct {
	Fortress   string             `json:"fortress"`
	Snapshot   string             `json:"snapshot"`
	SnapshotAt string             `json:"snapshot_time"`
	Time       time.Time          `json:"time"`
	Duration   time.Duration      `json:"duration_ns"`
	Checked    int                `json:"checked"`
	Bytes      int64              `json:"bytes"`
	Problems   []manifest.Problem `json:"problems"`
	RestoreDir string             `json:"restore_dir,omitempty"`
	ReportSeq  int                `json:"report_seq,omitempty"`

	// Report is the evidence report for this drill. Every drill that starts produces one,
	// including drills that stop before a verdict.
	Report *report.Report `json:"-"`
	// ReportPath is where the report was saved; ReportErr says why it was not.
	ReportPath string `json:"-"`
	ReportErr  error  `json:"-"`
}

// Survived reports whether the restore matched the manifest exactly.
func (d DrillReport) Survived() bool { return len(d.Problems) == 0 }

// ErrWrongPassword and ErrLocked come from restic; see the restic package.
var (
	ErrWrongPassword = restic.ErrWrongPassword
	ErrLocked        = restic.ErrLocked
)

// ErrBreached is returned when a drill finds problems.
var ErrBreached = errors.New("breach drill failed")

// Breach restores the latest snapshot into a fresh temporary directory and verifies it.
// Live volumes are never written: the restore target is checked to lie outside every volume and the repository.
// Whatever the outcome, it builds an evidence report and appends it to the fortress's report chain.
func Breach(ctx context.Context, bp *blueprint.Blueprint, r Restic, opt DrillOptions) (DrillReport, error) {
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	start := now()
	rep := DrillReport{Fortress: bp.Fortress, Time: start.UTC()}
	in := report.Input{
		Fortress:  bp.Fortress,
		Repo:      bp.RedactedRepo(),
		Volumes:   volumes(bp),
		Started:   start,
		TargetRTO: bp.Targets.RTODuration(),
		TargetRPO: bp.Targets.RPODuration(),
	}
	err := drill(ctx, bp, r, opt, &rep, &in)
	rep.Duration = now().Sub(start)
	in.Took = rep.Duration
	if err != nil {
		// restic's messages can echo the repository string; never let a URL password reach a report.
		msg := err.Error()
		if loc, red := bp.RepoLocation(), bp.RedactedRepo(); loc != red {
			msg = strings.ReplaceAll(msg, loc, red)
		}
		in.Err = errors.New(msg)
		in.NoSnapshot = errors.Is(err, restic.ErrNoSnapshot) || errors.Is(err, restic.ErrNoRepository)
	}
	rep.Report = report.Build(in)
	if store, serr := Reports(bp); serr != nil {
		rep.ReportErr = serr
	} else {
		rep.ReportPath, rep.ReportErr = store.Append(rep.Report, opt.SignKey)
		rep.ReportSeq = rep.Report.Seq
	}
	if err != nil {
		return rep, err
	}
	if err := writeState(bp, "last-drill.json", rep); err != nil {
		return rep, err
	}
	if !rep.Survived() {
		return rep, ErrBreached
	}
	return rep, nil
}

// drill runs the steps of a breach drill, timing each one into in.Phases.
func drill(ctx context.Context, bp *blueprint.Blueprint, r Restic, opt DrillOptions, rep *DrillReport, in *report.Input) error {
	step := func(name string, f func() error) error {
		t := time.Now()
		err := f()
		in.Phases = append(in.Phases, report.Phase{Name: name, MS: time.Since(t).Milliseconds()})
		if err != nil {
			in.ErrPhase = name
		}
		return err
	}
	var snap restic.Snapshot
	var all []restic.Snapshot
	err := step(report.PhaseLocate, func() error {
		var err error
		if snap, all, err = locate(ctx, r, opt.Snapshot); err != nil {
			return err
		}
		if !validID(snap.ID) {
			return fmt.Errorf("restic returned an invalid snapshot id %q", snap.ID)
		}
		return nil
	})
	rep.Snapshot, rep.SnapshotAt = snap.ID, snap.Time
	if validID(snap.ID) {
		in.SnapshotID, in.SnapshotAt = snap.ID, snap.Time
	}
	if err != nil {
		return err
	}
	if t := snap.When(); !t.IsZero() && t.After(in.Started.Add(clockSlack)) {
		in.Warnings = append(in.Warnings, fmt.Sprintf(
			"Snapshot %s is stamped %s, %s in the future. The clock of the host that took it was wrong, so restic sorts it as the newest even if later backups exist. Fix the clock (NTP), then run `sapper breach --snapshot <id>` on the snapshot you expect.",
			short(snap.ID), t.UTC().Format(time.RFC3339), report.Dur(t.Sub(in.Started).Round(time.Minute))))
	}
	var m *manifest.Manifest
	var mismatch *manifest.Problem
	if err := step(report.PhaseManifest, func() error {
		var err error
		m, mismatch, err = loadManifest(ctx, bp, r, snap.ID, opt.ManifestKey, in)
		if err != nil {
			return err
		}
		in.MerkleRoot = m.MerkleRoot()
		return nil
	}); err != nil {
		return err
	}
	var dir string
	if err := step(report.PhaseIsolation, func() error {
		var err error
		if dir, err = os.MkdirTemp(opt.TempRoot, "sapper-drill-*"); err != nil {
			return err
		}
		if err := checkIsolated(dir, bp); err != nil {
			return err
		}
		return checkSpace(dir, m, opt.FreeBytes)
	}); dir != "" {
		if !opt.Keep {
			defer os.RemoveAll(dir)
		} else {
			rep.RestoreDir, in.KeptCopy = dir, dir
		}
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	in.Isolated = true
	before, ferr := manifest.Fingerprint(bp.VolumePaths())
	if err := step(report.PhaseRestore, func() error { return r.Restore(ctx, snap.ID, dir) }); err != nil {
		return err
	}
	if opt.BeforeVerify != nil {
		if err := opt.BeforeVerify(dir); err != nil {
			return err
		}
	}
	var res manifest.Result
	if err := step(report.PhaseVerify, func() error {
		var err error
		res, err = manifest.Verify(m, dir)
		return err
	}); err != nil {
		return err
	}
	if mismatch != nil {
		res.Problems = append(res.Problems, *mismatch)
	}
	if opt.Sample > 0 {
		_ = step(PhaseOlder, func() error {
			older, problems := drillOlder(ctx, bp, r, opt, snap.ID, all, in)
			in.Older = older
			res.Problems = append(res.Problems, problems...)
			return nil
		})
	}
	rep.Checked, rep.Bytes, rep.Problems = res.Checked, res.Bytes, res.Problems
	in.Result = &res
	if ferr == nil {
		_ = step(report.PhaseLiveCheck, func() error {
			after, err := manifest.Fingerprint(bp.VolumePaths())
			if err != nil {
				return nil
			}
			in.LiveCheck, in.LiveDiff = true, manifest.FingerprintDiff(before, after)
			return nil
		})
	}
	if len(res.Problems) == 0 {
		_ = writeState(bp, "last-verified.json", VerifiedRecord{Snapshot: snap.ID, Time: in.Started.UTC()})
	}
	return nil
}

// PhaseOlder is the drill phase that restores sampled older snapshots.
const PhaseOlder = "older snapshots"

// locate finds the snapshot to drill: the given ID or prefix, or the latest.
func locate(ctx context.Context, r Restic, want string) (restic.Snapshot, []restic.Snapshot, error) {
	sl, ok := r.(snapshotLister)
	if !ok {
		if want != "" {
			return restic.Snapshot{}, nil, errors.New("this restic client cannot list snapshots")
		}
		s, err := r.Latest(ctx, Tag)
		return s, nil, err
	}
	all, err := sl.Snapshots(ctx, Tag)
	if err != nil {
		return restic.Snapshot{}, nil, err
	}
	if len(all) == 0 {
		return restic.Snapshot{}, nil, restic.ErrNoSnapshot
	}
	if want == "" {
		return all[len(all)-1], all, nil
	}
	var hits []restic.Snapshot
	for _, s := range all {
		if strings.HasPrefix(s.ID, want) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return restic.Snapshot{}, all, fmt.Errorf("no sapper snapshot with ID %s", report.Clean(want))
	case 1:
		return hits[0], all, nil
	}
	return restic.Snapshot{}, all, fmt.Errorf("snapshot ID %s is ambiguous; give more characters", report.Clean(want))
}

// loadManifest returns the manifest for snapshot id: the local one, checked against the
// copy in the repository, or the repository copy alone when this machine has none.
func loadManifest(ctx context.Context, bp *blueprint.Blueprint, r Restic, id string, pub ed25519.PublicKey, in *report.Input) (*manifest.Manifest, *manifest.Problem, error) {
	mp, err := statePath(bp, "manifests", id+".json")
	if err != nil {
		return nil, nil, err
	}
	local, lerr := manifest.Load(mp)
	remote, signed, found, rerr := loadCopy(ctx, r, id, pub)
	switch {
	case rerr != nil && (errors.Is(rerr, ErrManifestSignature) || errors.Is(rerr, ErrManifestConflict) || lerr != nil):
		return nil, nil, fmt.Errorf("manifest for snapshot %s: repository copy: %w", short(id), rerr)
	case rerr != nil:
		in.Warnings = append(in.Warnings, "The manifest copy in the repository could not be read ("+report.CleanLines(rerr.Error())+"). The drill used the local manifest only.")
	}
	if lerr != nil {
		if !found {
			return nil, nil, fmt.Errorf("manifest for snapshot %s: %w", short(id), lerr)
		}
		how := "unsigned"
		if signed {
			how = "signed"
		}
		in.Warnings = append(in.Warnings, fmt.Sprintf("No local manifest for snapshot %s on this machine; the drill used the %s copy stored in the repository at backup time.", short(id), how))
		return remote, nil, nil
	}
	if pub != nil && !found {
		return nil, nil, fmt.Errorf("manifest for snapshot %s: %w: no copy in the repository to check the signature of", short(id), ErrManifestSignature)
	}
	if found && rerr == nil && remote.Sum() != local.Sum() {
		p := &manifest.Problem{Path: "manifest " + short(id), Kind: manifest.ProblemManifest,
			Reason:   "local manifest differs from the copy stored in the repository at backup time",
			Expected: "merkle " + remote.MerkleRoot(), Actual: "merkle " + local.MerkleRoot()}
		if signed {
			// The signed copy is the one to trust; verify against it.
			return remote, p, nil
		}
		return local, p, nil
	}
	if !found {
		in.Warnings = append(in.Warnings, "The repository holds no manifest copy for this snapshot (taken before sapper stored one). A drill from another machine could not check it.")
	}
	return local, nil, nil
}

// checkSpace refuses a restore that cannot fit in the temp directory.
func checkSpace(dir string, m *manifest.Manifest, free func(string) (uint64, error)) error {
	if free == nil {
		free = freeBytes
	}
	var need uint64
	for _, e := range m.Entries {
		if e.Size > 0 {
			need += uint64(e.Size)
		}
	}
	have, err := free(dir)
	if err != nil {
		return nil // unknown on this platform; restic will report a full disk itself
	}
	if have < need+need/10 {
		return fmt.Errorf("%w: %s needs about %s, %s is free in %s. Pass --temp-dir on a larger disk",
			ErrNoSpace, "the restore", report.Bytes(int64(need)), report.Bytes(int64(min(have, 1<<62))), filepath.Dir(dir))
	}
	return nil
}

// drillOlder restores and verifies up to opt.Sample randomly chosen snapshots older than latest.
func drillOlder(ctx context.Context, bp *blueprint.Blueprint, r Restic, opt DrillOptions, latest string, all []restic.Snapshot, in *report.Input) ([]report.OlderResult, []manifest.Problem) {
	var pool []restic.Snapshot
	for _, s := range all {
		if s.ID != latest && validID(s.ID) {
			pool = append(pool, s)
		}
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	pool = pool[:min(opt.Sample, len(pool))]
	restic.SortByTime(pool)
	var out []report.OlderResult
	var problems []manifest.Problem
	for _, s := range pool {
		o := report.OlderResult{ID: s.ID, Time: s.When().UTC()}
		var scratch report.Input
		m, mismatch, err := loadManifest(ctx, bp, r, s.ID, opt.ManifestKey, &scratch)
		if err != nil {
			o.Verdict, o.Error = report.NoVerdict, "no manifest to check against"
			out = append(out, o)
			continue
		}
		res, err := restoreVerify(ctx, bp, r, opt, s.ID, m)
		switch {
		case err != nil:
			o.Verdict, o.Error = report.Breached, "restore failed: "+firstLine(err.Error())
			o.Problems = 1
		default:
			if mismatch != nil {
				res.Problems = append(res.Problems, *mismatch)
			}
			o.Checked, o.Problems = res.Checked, len(res.Problems)
			o.Verdict = report.Survived
			if o.Problems > 0 {
				o.Verdict = report.Breached
			}
		}
		if o.Verdict == report.Breached {
			detail := fmt.Sprintf("%d problems", o.Problems)
			if o.Error != "" {
				detail = o.Error
			}
			problems = append(problems, manifest.Problem{Path: "snapshot " + short(s.ID), Kind: manifest.ProblemOlder,
				Reason: "older snapshot did not restore cleanly", Expected: "restores and matches its manifest", Actual: detail})
		}
		out = append(out, o)
	}
	return out, problems
}

func restoreVerify(ctx context.Context, bp *blueprint.Blueprint, r Restic, opt DrillOptions, id string, m *manifest.Manifest) (manifest.Result, error) {
	dir, err := os.MkdirTemp(opt.TempRoot, "sapper-drill-*")
	if err != nil {
		return manifest.Result{}, err
	}
	defer os.RemoveAll(dir)
	if err := checkIsolated(dir, bp); err != nil {
		return manifest.Result{}, err
	}
	if err := checkSpace(dir, m, opt.FreeBytes); err != nil {
		return manifest.Result{}, err
	}
	if err := r.Restore(ctx, id, dir); err != nil {
		return manifest.Result{}, err
	}
	return manifest.Verify(m, dir)
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return report.Fit(s, 160)
}

// volumes lists every backed-up host directory with the app and mount it belongs to.
func volumes(bp *blueprint.Blueprint) []report.Volume {
	var out []report.Volume
	for _, name := range bp.AppNames() {
		app := bp.Apps[name]
		mounts := make([]string, 0, len(app.Volumes))
		for k := range app.Volumes {
			mounts = append(mounts, k)
		}
		sort.Strings(mounts)
		for _, k := range mounts {
			out = append(out, report.Volume{App: name, Mount: k, Path: bp.HostPath(app.Volumes[k])})
		}
	}
	return out
}

// Reports returns the report store for bp.
func Reports(bp *blueprint.Blueprint) (report.Store, error) {
	sd, err := StateDir(bp)
	if err != nil {
		return report.Store{}, err
	}
	return report.Store{Dir: filepath.Join(sd, "reports")}, nil
}

// checkIsolated refuses restore targets that overlap live data or the repository.
func checkIsolated(dir string, bp *blueprint.Blueprint) error {
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	sd, err := StateDir(bp)
	if err != nil {
		return err
	}
	guarded := append(bp.VolumePaths(), sd, filepath.Join(bp.Dir, ".sapper"))
	if loc := bp.RepoLocation(); filepath.IsAbs(loc) {
		guarded = append(guarded, loc)
	}
	for _, g := range guarded {
		if real, err := filepath.EvalSymlinks(g); err == nil {
			g = real
		}
		if overlaps(dir, g) {
			return fmt.Errorf("refusing drill: restore directory %s overlaps %s", dir, g)
		}
	}
	return nil
}

func overlaps(a, b string) bool {
	return a == b || inside(a, b) || inside(b, a)
}

func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// LastBackup reads the last backup record, if any.
func LastBackup(bp *blueprint.Blueprint) (BackupRecord, bool) {
	var r BackupRecord
	return r, readState(bp, "last-backup.json", &r) == nil
}

// LastDrill reads the last drill report, if any.
func LastDrill(bp *blueprint.Blueprint) (DrillReport, bool) {
	var r DrillReport
	return r, readState(bp, "last-drill.json", &r) == nil
}

func writeState(bp *blueprint.Blueprint, name string, v any) error {
	p, err := statePath(bp, name)
	if err != nil {
		return err
	}
	return writeJSON(p, v)
}

func readState(bp *blueprint.Blueprint, name string, v any) error {
	p, err := statePath(bp, name)
	if err != nil {
		return err
	}
	return readJSON(p, v)
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// validID reports whether id is a restic snapshot ID (lowercase hex). IDs become file names
// in the state directory, so anything else could escape it.
func validID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f') {
			return false
		}
	}
	return true
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
