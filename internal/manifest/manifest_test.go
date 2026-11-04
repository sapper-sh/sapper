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

func TestMerkleRootChangesWithAnyField(t *testing.T) {
	base := &Manifest{Roots: []string{"/r"}, Entries: map[string]Entry{
		"/r":   {Kind: KindDir, Mode: 0o755},
		"/r/a": {Kind: KindFile, Size: 1, SHA256: "aa", Mode: 0o644},
		"/r/b": {Kind: KindSymlink, Target: "a", Mode: 0o777},
	}}
	root := base.MerkleRoot()
	if len(root) != 64 || root != base.MerkleRoot() {
		t.Fatalf("root %q not stable", root)
	}
	mutations := []func(m *Manifest){
		func(m *Manifest) { e := m.Entries["/r/a"]; e.SHA256 = "ab"; m.Entries["/r/a"] = e },
		func(m *Manifest) { e := m.Entries["/r/a"]; e.Mode = 0o600; m.Entries["/r/a"] = e },
		func(m *Manifest) { e := m.Entries["/r/b"]; e.Target = "c"; m.Entries["/r/b"] = e },
		func(m *Manifest) { m.Entries["/r/c"] = Entry{Kind: KindDir} },
		func(m *Manifest) { delete(m.Entries, "/r/b") },
	}
	for i, mut := range mutations {
		m := &Manifest{Roots: base.Roots, Entries: map[string]Entry{}}
		for k, v := range base.Entries {
			m.Entries[k] = v
		}
		mut(m)
		if m.MerkleRoot() == root {
			t.Errorf("mutation %d did not change the root", i)
		}
	}
}

func TestVerifyReportsExpectedAndActual(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Build([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	base := t.TempDir()
	dst := RestoredPath(base, src)
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dst, m.Entries[src].Mode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "f"), []byte("two"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Verify(m, base)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Problems) != 1 {
		t.Fatalf("problems = %+v", res.Problems)
	}
	p := res.Problems[0]
	if p.Kind != ProblemContent || p.Expected != m.Entries[filepath.Join(src, "f")].SHA256 || len(p.Actual) != 64 || p.Expected == p.Actual {
		t.Fatalf("problem = %+v", p)
	}
	if res.Checked != 2 || res.Matched != 1 || res.Files != 1 || res.Dirs != 1 {
		t.Fatalf("counts = %+v", res)
	}
}

func TestFingerprintDiff(t *testing.T) {
	d := t.TempDir()
	f := filepath.Join(d, "x")
	if err := os.WriteFile(f, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, err := Fingerprint([]string{d})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("22"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := Fingerprint([]string{d})
	if diff := FingerprintDiff(a, b); len(diff) != 1 || diff[0] != f {
		t.Fatalf("diff = %v", diff)
	}
	if diff := FingerprintDiff(b, b); len(diff) != 0 {
		t.Fatalf("diff of identical = %v", diff)
	}
}
