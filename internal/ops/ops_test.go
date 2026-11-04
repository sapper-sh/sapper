package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapper-sh/sapper/internal/blueprint"
	"github.com/sapper-sh/sapper/internal/manifest"
	"github.com/sapper-sh/sapper/internal/paths"
	"github.com/sapper-sh/sapper/internal/restic"
)

// fakeRestic stores one snapshot by copying volumes, so ops logic can be tested without restic.
type fakeRestic struct {
	snapDir  string
	snapID   string
	onBackup func()
}

func (f *fakeRestic) EnsureRepo(context.Context) (bool, error)  { return false, nil }
func (f *fakeRestic) Forget(context.Context, string, int) error { return nil }

func (f *fakeRestic) Backup(_ context.Context, ps, _ []string) (string, error) {
	for _, p := range ps {
		if err := copyDir(p, manifest.RestoredPath(f.snapDir, p)); err != nil {
			return "", err
		}
	}
	if f.onBackup != nil {
		f.onBackup()
	}
	f.snapID = "f00dfeed"
	return f.snapID, nil
}

func (f *fakeRestic) Latest(context.Context, string) (restic.Snapshot, error) {
	if f.snapID == "" {
		return restic.Snapshot{}, restic.ErrNoSnapshot
	}
	return restic.Snapshot{ID: f.snapID, Time: "2026-11-04T00:00:00Z"}, nil
}

func (f *fakeRestic) Restore(_ context.Context, _ string, target string) error {
	return copyDir(f.snapDir, target)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(out, data, info.Mode().Perm())
	})
}

// newFortress writes the example blueprint and fixture data into a temp dir.
func newFortress(t *testing.T) *blueprint.Blueprint {
	t.Helper()
	t.Setenv(paths.EnvStateDir, t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, blueprint.FileName), []byte(blueprint.Example), 0o600); err != nil {
		t.Fatal(err)
	}
	bp, err := blueprint.Load(filepath.Join(dir, blueprint.FileName))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range bp.VolumePaths() {
		if err := os.MkdirAll(filepath.Join(p, "nested"), 0o755); err != nil {
			t.Fatal(err)
		}
		content := strings.Repeat("payload-", i+1)
		if err := os.WriteFile(filepath.Join(p, "nested", "data.db"), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return bp
}

func TestBreachOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		damage  func(dir string) error
		wantErr error
	}{
		{"clean restore survives", nil, nil},
		{"corrupted file breaches", func(dir string) error {
			return filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
				if err == nil && info.Name() == "data.db" {
					return os.WriteFile(p, []byte("tampered"), 0o644)
				}
				return err
			})
		}, ErrBreached},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bp := newFortress(t)
			fr := &fakeRestic{snapDir: t.TempDir()}
			if _, err := Backup(context.Background(), bp, fr, BackupOptions{}); err != nil {
				t.Fatal(err)
			}
			rep, err := Breach(context.Background(), bp, fr, DrillOptions{BeforeVerify: tc.damage})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if rep.Survived() != (tc.wantErr == nil) {
				t.Fatalf("Survived = %v with problems %+v", rep.Survived(), rep.Problems)
			}
			last, ok := LastDrill(bp)
			if !ok || last.Snapshot != "f00dfeed" {
				t.Fatalf("last drill not recorded: %+v", last)
			}
		})
	}
}

func TestBreachWithoutSnapshot(t *testing.T) {
	bp := newFortress(t)
	_, err := Breach(context.Background(), bp, &fakeRestic{snapDir: t.TempDir()}, DrillOptions{})
	if !errors.Is(err, restic.ErrNoSnapshot) {
		t.Fatalf("err = %v, want ErrNoSnapshot", err)
	}
}

func TestBreachRefusesTempInsideVolume(t *testing.T) {
	bp := newFortress(t)
	fr := &fakeRestic{snapDir: t.TempDir()}
	if _, err := Backup(context.Background(), bp, fr, BackupOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err := Breach(context.Background(), bp, fr, DrillOptions{TempRoot: bp.VolumePaths()[0]})
	if err == nil || !strings.Contains(err.Error(), "refusing drill") {
		t.Fatalf("err = %v, want refusal", err)
	}
	entries, _ := os.ReadDir(bp.VolumePaths()[0])
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "sapper-drill-") {
			t.Fatalf("drill directory left inside live volume: %s", e.Name())
		}
	}
}

func TestBackupDetectsConcurrentChange(t *testing.T) {
	bp := newFortress(t)
	victim := filepath.Join(bp.VolumePaths()[0], "nested", "data.db")
	fr := &fakeRestic{snapDir: t.TempDir(), onBackup: func() { _ = os.WriteFile(victim, []byte("written mid-backup"), 0o644) }}
	_, err := Backup(context.Background(), bp, fr, BackupOptions{})
	if err == nil || !strings.Contains(err.Error(), "changed during backup") {
		t.Fatalf("err = %v, want change detection", err)
	}
}

func TestBackupMissingVolume(t *testing.T) {
	bp := newFortress(t)
	if err := os.RemoveAll(bp.VolumePaths()[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := Backup(context.Background(), bp, &fakeRestic{snapDir: t.TempDir()}, BackupOptions{}); err == nil {
		t.Fatal("expected error for missing volume")
	}
}
