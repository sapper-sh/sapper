package ops

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapper-unbuilt/sapper/internal/manifest"
	"github.com/sapper-unbuilt/sapper/internal/report"
	"github.com/sapper-unbuilt/sapper/internal/restic"
)

// TestEndToEndRealRestic runs backup and breach against a real restic repository.
func TestEndToEndRealRestic(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	bp := newFortress(t)
	bp.Backup.Repository = filepath.Join(t.TempDir(), "repo")
	ctx := context.Background()
	var log bytes.Buffer
	c, err := restic.New(bp.RepoLocation(), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	c.Log = &log

	live, err := manifest.Build(bp.VolumePaths())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := Backup(ctx, bp, c, BackupOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !rec.Created || rec.Snapshot == "" {
		t.Fatalf("unexpected backup record %+v", rec)
	}
	if !bytes.Contains(log.Bytes(), []byte("$ restic backup")) || bytes.Contains(log.Bytes(), []byte("test-password")) {
		t.Fatalf("command log wrong:\n%s", log.String())
	}

	rep, err := Breach(ctx, bp, c, DrillOptions{})
	if err != nil {
		t.Fatalf("clean drill failed: %v %+v", err, rep.Problems)
	}
	if rep.Snapshot != rec.Snapshot || rep.Checked != len(live.Entries) {
		t.Fatalf("report %+v does not match backup %+v (%d entries)", rep, rec, len(live.Entries))
	}
	if r := rep.Report; r.Verdict != report.Survived || r.Snapshot.ID != rec.Snapshot || r.Evidence.Matched != len(live.Entries) ||
		!r.Isolation.LiveUntouched || r.Evidence.MerkleRoot != live.MerkleRoot() || r.Snapshot.Time.IsZero() || rep.ReportErr != nil {
		t.Fatalf("survived report: %+v (save error %v)", r, rep.ReportErr)
	}

	var corrupted string
	rep, err = Breach(ctx, bp, c, DrillOptions{BeforeVerify: func(dir string) error {
		corrupted = manifest.RestoredPath(dir, filepath.Join(bp.VolumePaths()[1], "nested", "data.db"))
		return os.WriteFile(corrupted, []byte("bitrot"), 0o644)
	}})
	if !errors.Is(err, ErrBreached) || rep.Survived() {
		t.Fatalf("corrupted drill: err = %v survived = %v", err, rep.Survived())
	}
	f := rep.Report.Evidence.Findings
	if rep.Report.Verdict != report.Breached || len(f) != 1 || f[0].Kind != manifest.ProblemSize ||
		!strings.Contains(f[0].Path, filepath.Join("nested", "data.db")) || !strings.Contains(f[0].Actual, "6 bytes") {
		t.Fatalf("breached report findings: %+v", f)
	}
	store, _ := Reports(bp)
	all, _ := store.List()
	if issues := report.VerifyChain(all, nil, rep.Report.Hash); len(all) != 2 || len(issues) != 0 {
		t.Fatalf("chain of %d: %+v", len(all), issues)
	}
	if _, statErr := os.Stat(corrupted); !os.IsNotExist(statErr) {
		t.Fatalf("throwaway restore not removed: %v", statErr)
	}

	after, err := manifest.Build(bp.VolumePaths())
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Equal(live, after) {
		t.Fatal("live data changed during drills")
	}
}

// TestDrillDoesNotTrustResticCache damages the pack that holds the snapshot's trees. restic
// keeps tree blobs in its local cache, so a restore that uses the cache still succeeds;
// a drill must read the repository itself.
func TestDrillDoesNotTrustResticCache(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	bp := newFortress(t)
	bp.Backup.Repository = filepath.Join(t.TempDir(), "repo")
	t.Setenv("RESTIC_CACHE_DIR", t.TempDir())
	ctx := context.Background()
	c, err := restic.New(bp.RepoLocation(), "test-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(ctx, bp, c, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	var packs []string
	_ = filepath.WalkDir(filepath.Join(bp.RepoLocation(), "data"), func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			packs = append(packs, p)
		}
		return nil
	})
	for _, p := range packs {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b[len(b)/2] ^= 1
		_ = os.Chmod(p, 0o600)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rep, err := Breach(ctx, bp, c, DrillOptions{})
	if err == nil || rep.Report.Verdict == report.Survived {
		t.Fatalf("drill survived a repository with every pack damaged: %v", err)
	}
}
