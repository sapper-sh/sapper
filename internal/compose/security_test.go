package compose

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRenderRefusesCronUnsafeDir(t *testing.T) {
	for _, dir := range []string{"/srv/50%off", "/srv/a\nb"} {
		bp := example(t)
		bp.Dir = dir
		if _, err := Render(bp); err == nil || !strings.Contains(err.Error(), "cron") {
			t.Errorf("Render with Dir %q: err = %v, want cron refusal", dir, err)
		}
	}
}

func TestServicesDropNewPrivileges(t *testing.T) {
	f, err := Build(example(t))
	if err != nil {
		t.Fatal(err)
	}
	for name, svc := range f.Services {
		if len(svc.SecurityOpt) != 1 || svc.SecurityOpt[0] != "no-new-privileges:true" {
			t.Errorf("%s security_opt = %v", name, svc.SecurityOpt)
		}
		for _, v := range svc.Volumes {
			if strings.Contains(v, "docker.sock") {
				t.Errorf("%s mounts the docker socket", name)
			}
		}
	}
}

func TestWriteTightensExistingDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes")
	}
	dir := filepath.Join(t.TempDir(), ".sapper")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Write(dir, Artifacts{"restic.env": []byte("x\n")}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(dir)
	if st.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v, want 0700", st.Mode().Perm())
	}
}

func TestRenderRefusesShellMetaInRepoLocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("restic.env is not sourced on Windows")
	}
	bp := example(t)
	bp.Dir = "/srv/$(id)"
	bp.Drill.Schedule = ""
	if _, err := Render(bp); err == nil || !strings.Contains(err.Error(), "shell metacharacters") {
		t.Fatalf("err = %v", err)
	}
}
