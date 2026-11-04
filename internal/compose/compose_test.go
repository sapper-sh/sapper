package compose

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/sapper-sh/sapper/internal/blueprint"
)

func example(t *testing.T) *blueprint.Blueprint {
	return exampleIn(t, t.TempDir())
}

func exampleIn(t *testing.T, dir string) *blueprint.Blueprint {
	t.Helper()
	bp, err := blueprint.Parse([]byte(blueprint.Example))
	if err != nil {
		t.Fatal(err)
	}
	bp.Dir = dir
	if err := bp.Validate(); err != nil {
		t.Fatal(err)
	}
	return bp
}

func TestBuild(t *testing.T) {
	dir := t.TempDir()
	d := func(p string) string { return filepath.Join(dir, "data", filepath.FromSlash(p)) }
	f, err := Build(exampleIn(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		svc, port string
		volumes   []string
	}{
		{"vaultwarden", "127.0.0.1:8081:80", []string{d("vaultwarden") + ":/data"}},
		{"filebrowser", "127.0.0.1:8082:80", []string{d("filebrowser/files") + ":/srv", d("filebrowser/database") + ":/database"}},
		{"memos", "127.0.0.1:8083:5230", []string{d("memos") + ":/var/opt/memos"}},
	}
	for _, tc := range tests {
		s, ok := f.Services[tc.svc]
		if !ok {
			t.Fatalf("service %s missing", tc.svc)
		}
		if s.Ports[0] != tc.port {
			t.Errorf("%s port = %s, want %s", tc.svc, s.Ports[0], tc.port)
		}
		if strings.Join(s.Volumes, ",") != strings.Join(tc.volumes, ",") {
			t.Errorf("%s volumes = %v, want %v", tc.svc, s.Volumes, tc.volumes)
		}
	}
}

func TestRenderRoundTrip(t *testing.T) {
	a, err := Render(example(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"docker-compose.yml", "restic.env", "backup-paths.txt", "drill.cron"} {
		if _, ok := a[name]; !ok {
			t.Errorf("artifact %s missing", name)
		}
	}
	var f File
	if err := yaml.Unmarshal(a["docker-compose.yml"], &f); err != nil {
		t.Fatalf("compose output is not valid YAML: %v", err)
	}
	if f.Name != "my-fortress" || len(f.Services) != 3 {
		t.Errorf("round trip got name %q with %d services", f.Name, len(f.Services))
	}
	if strings.Contains(string(a["restic.env"]), "RESTIC_PASSWORD=") {
		t.Error("restic.env must not contain a password")
	}
}

func TestWritePermissions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".sapper")
	if err := Write(dir, Artifacts{"x": []byte("y")}); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, "x"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", st.Mode().Perm())
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{"/a b": "'/a b'", "it's": `'it'\''s'`}
	for in, want := range tests {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
