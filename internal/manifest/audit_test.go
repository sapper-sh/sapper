package manifest

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestUnexpectedWalkRejectsSymlinkAboveRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires unprivileged symlinks")
	}
	base, outside := t.TempDir(), t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "files"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}
	res, err := Verify(&Manifest{Roots: []string{"/link/files"}}, base)
	if err != nil || len(res.Problems) != 1 || res.Problems[0].Kind != ProblemUnsafe {
		t.Fatalf("unsafe root: %+v, %v", res, err)
	}
}
