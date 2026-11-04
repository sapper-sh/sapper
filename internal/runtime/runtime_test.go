package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		wantErr bool
	}{{"docker", false}, {"podman", false}, {"lxc", true}, {"", true}}
	for _, tc := range tests {
		if _, err := New(tc.name, nil); (err != nil) != tc.wantErr {
			t.Errorf("New(%q) err = %v, wantErr %v", tc.name, err, tc.wantErr)
		}
	}
}

func TestHint(t *testing.T) {
	tests := []struct {
		stderr, want string
	}{
		{"permission denied while trying to connect to the Docker daemon socket at unix:///var/run/docker.sock", "usermod -aG docker"},
		{"Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?", "systemctl start docker"},
		{"docker: 'compose' is not a docker command.", "compose plugin"},
		{"some other failure", ""},
	}
	for _, tc := range tests {
		got := Hint("docker", tc.stderr)
		if tc.want == "" && got != "" || !strings.Contains(got, tc.want) {
			t.Errorf("Hint(%q) = %q, want containing %q", tc.stderr, got, tc.want)
		}
	}
}

func TestUpRefusesNonLinux(t *testing.T) {
	r := &Runtime{Name: "docker", goos: "darwin"}
	if err := r.Up(context.Background(), "f", "p"); !errors.Is(err, ErrNotLinux) {
		t.Fatalf("err = %v, want ErrNotLinux", err)
	}
}

func TestComposeArgs(t *testing.T) {
	got := strings.Join(ComposeArgs("/x/docker-compose.yml", "fort"), " ")
	if got != "compose -f /x/docker-compose.yml -p fort up -d" {
		t.Fatalf("got %q", got)
	}
}

func TestWSL(t *testing.T) {
	if !isWSLVersion("Linux version 5.15.153.1-microsoft-standard-WSL2") || isWSLVersion("Linux version 6.8.0-generic") {
		t.Fatal("isWSLVersion misdetects")
	}
	if got := hint("docker", "Cannot connect to the Docker daemon at unix:///var/run/docker.sock", true); !strings.Contains(got, "WSL integration") {
		t.Fatalf("WSL hint = %q", got)
	}
}

func TestUpWindowsPointsToWSL(t *testing.T) {
	r := &Runtime{Name: "docker", goos: "windows"}
	err := r.Up(context.Background(), "f", "p")
	if !errors.Is(err, ErrNotLinux) || !strings.Contains(err.Error(), "WSL2") {
		t.Fatalf("err = %v", err)
	}
}
