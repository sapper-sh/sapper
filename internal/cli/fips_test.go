package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestFIPSBuild builds sapper with GOFIPS140=v1.0.0 (the module covered by CMVP
// certificate #5247), checks that --version reports FIPS mode without claiming
// validation, and that ed25519 key generation still works in that mode.
func TestFIPSBuild(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	gobin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go toolchain not found")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "sapper")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command(gobin, "build", "-o", bin, "../../cmd/sapper")
	build.Env = append(os.Environ(), "GOFIPS140=v1.0.0", "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("GOFIPS140=v1.0.0 build not available here: %v\n%s", err, out)
	}
	out, err := exec.Command(bin, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("--version: %v\n%s", err, out)
	}
	v := string(out)
	if !strings.Contains(v, "FIPS 140-3 mode") || !strings.Contains(v, "v1.0.0") || !strings.Contains(v, "not FIPS validated") {
		t.Fatalf("--version in FIPS build: %q", v)
	}
	key := filepath.Join(dir, "drill.key")
	cmd := exec.Command(bin, "keygen", "--out", key)
	cmd.Env = append(os.Environ(), "XDG_CONFIG_HOME="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("keygen in FIPS mode: %v\n%s", err, out)
	}
	if v := string(must(exec.Command(bin, "--version").Output())); !strings.Contains(v, "FIPS") {
		t.Fatalf("FIPS mode not on by default in a GOFIPS140 build: %q", v)
	}
	plain := string(must(exec.Command(gobin, "run", "../../cmd/sapper", "--version").Output()))
	if strings.Contains(plain, "FIPS") {
		t.Fatalf("default build claims FIPS mode: %q", plain)
	}
}

func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}
