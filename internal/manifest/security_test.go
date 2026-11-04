package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A restored tree (or a tampered manifest) must not make Verify read files outside the restore directory.
func TestVerifyDoesNotFollowRestoredSymlinkParents(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("s3cret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(base, "r"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "r", "a")); err != nil {
		t.Fatal(err)
	}
	m := &Manifest{Roots: []string{"/r"}, Entries: map[string]Entry{
		"/r/a/secret": {Kind: KindFile, Size: 6, Mode: 0o600},
	}}
	r, err := Verify(m, base)
	if err != nil {
		t.Fatal(err)
	}
	if !hasReason(r, "/r/a/secret", "parent is a symlink") {
		t.Fatalf("problems = %+v", r.Problems)
	}
	if r.Bytes != 0 {
		t.Fatalf("read %d bytes through a symlink", r.Bytes)
	}
}

func TestVerifyRejectsManifestPathsOutsideRoots(t *testing.T) {
	base := t.TempDir()
	escape := string(filepath.Separator) + filepath.Join("..", "..", "..", "..", "..", "..", "etc", "hostname")
	m := &Manifest{Roots: []string{filepath.FromSlash("/r")}, Entries: map[string]Entry{
		escape:                            {Kind: KindFile},
		filepath.FromSlash("/other/file"): {Kind: KindFile},
	}}
	r, err := Verify(m, base)
	if err != nil {
		t.Fatal(err)
	}
	if !hasReason(r, escape, "outside backup roots") || !hasReason(r, filepath.FromSlash("/other/file"), "outside backup roots") {
		t.Fatalf("problems = %+v", r.Problems)
	}
}

func hasReason(r Result, path, reason string) bool {
	for _, p := range r.Problems {
		if p.Path == path && strings.Contains(p.Reason, reason) {
			return true
		}
	}
	return false
}
