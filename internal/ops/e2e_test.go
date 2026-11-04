package ops

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sapper-unbuilt/sapper/internal/manifest"
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
	rec, err := Backup(ctx, bp, c)
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

	var corrupted string
	rep, err = Breach(ctx, bp, c, DrillOptions{BeforeVerify: func(dir string) error {
		corrupted = manifest.RestoredPath(dir, filepath.Join(bp.VolumePaths()[1], "nested", "data.db"))
		return os.WriteFile(corrupted, []byte("bitrot"), 0o644)
	}})
	if !errors.Is(err, ErrBreached) || rep.Survived() {
		t.Fatalf("corrupted drill: err = %v survived = %v", err, rep.Survived())
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
