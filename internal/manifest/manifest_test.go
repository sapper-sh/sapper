package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vol")
	must(t, os.MkdirAll(filepath.Join(root, "sub", "empty"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("alpha"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "sub", "b.bin"), []byte{0, 1, 2}, 0o600))
	if err := os.Symlink("a.txt", filepath.Join(root, "link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	return root
}

// copyTree mirrors root under base+root, as restic restore does.
func copyTree(t *testing.T, root, base string) {
	t.Helper()
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		dst := RestoredPath(base, p)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			tgt, _ := os.Readlink(p)
			return os.Symlink(tgt, dst)
		case info.IsDir():
			return os.MkdirAll(dst, info.Mode().Perm())
		default:
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			return os.WriteFile(dst, data, info.Mode().Perm())
		}
	})
	must(t, err)
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, restored string)
		want   string
	}{
		{"identical", func(*testing.T, string) {}, ""},
		{"corrupted same size", func(t *testing.T, r string) {
			must(t, os.WriteFile(filepath.Join(r, "a.txt"), []byte("alphA"), 0o644))
		}, "checksum mismatch"},
		{"truncated", func(t *testing.T, r string) {
			must(t, os.WriteFile(filepath.Join(r, "a.txt"), []byte("al"), 0o644))
		}, "size 2, expected 5"},
		{"missing file", func(t *testing.T, r string) {
			must(t, os.Remove(filepath.Join(r, "sub", "b.bin")))
		}, "missing"},
		{"missing empty dir", func(t *testing.T, r string) {
			must(t, os.Remove(filepath.Join(r, "sub", "empty")))
		}, "missing"},
		{"extra file", func(t *testing.T, r string) {
			must(t, os.WriteFile(filepath.Join(r, "intruder"), nil, 0o644))
		}, "unexpected in restore"},
		{"mode changed", func(t *testing.T, r string) {
			if runtime.GOOS == "windows" {
				t.Skip("permissions are not compared on Windows")
			}
			must(t, os.Chmod(filepath.Join(r, "a.txt"), 0o600))
		}, "mode"},
		{"symlink retargeted", func(t *testing.T, r string) {
			must(t, os.Remove(filepath.Join(r, "link")))
			must(t, os.Symlink("sub", filepath.Join(r, "link")))
		}, "symlink target"},
		{"file replaced by dir", func(t *testing.T, r string) {
			must(t, os.Remove(filepath.Join(r, "a.txt")))
			must(t, os.Mkdir(filepath.Join(r, "a.txt"), 0o755))
		}, "type changed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := fixture(t)
			m, err := Build([]string{root})
			must(t, err)
			base := t.TempDir()
			copyTree(t, root, base)
			tc.mutate(t, RestoredPath(base, root))
			res, err := Verify(m, base)
			must(t, err)
			if tc.want == "" {
				if !res.OK() {
					t.Fatalf("unexpected problems: %+v", res.Problems)
				}
				if res.Checked != 6 || res.Bytes != 8 {
					t.Errorf("checked %d entries, %d bytes; want 6, 8", res.Checked, res.Bytes)
				}
				return
			}
			if res.OK() {
				t.Fatalf("expected problem %q, got none", tc.want)
			}
			found := false
			for _, p := range res.Problems {
				if strings.Contains(p.Reason, tc.want) {
					found = true
				}
			}
			if !found {
				t.Fatalf("problems %+v do not mention %q", res.Problems, tc.want)
			}
		})
	}
}

func TestBuildRejectsRelativeRoot(t *testing.T) {
	if _, err := Build([]string{"rel"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSaveLoadEqual(t *testing.T) {
	root := fixture(t)
	m, err := Build([]string{root})
	must(t, err)
	p := filepath.Join(t.TempDir(), "m.json")
	must(t, Save(p, m))
	got, err := Load(p)
	must(t, err)
	if !Equal(m, got) {
		t.Fatal("loaded manifest differs")
	}
	must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("beta!"), 0o644))
	m2, err := Build([]string{root})
	must(t, err)
	if Equal(m, m2) {
		t.Fatal("Equal missed a content change")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
