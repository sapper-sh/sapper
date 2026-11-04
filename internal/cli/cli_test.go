package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapper-unbuilt/sapper/internal/blueprint"
	"github.com/sapper-unbuilt/sapper/internal/paths"
)

type result struct {
	code           int
	stdout, stderr string
}

func run(t *testing.T, stdin string, args ...string) result {
	t.Helper()
	var out, errb bytes.Buffer
	code := Main(args, strings.NewReader(stdin), &out, &errb)
	return result{code, out.String(), errb.String()}
}

// fortress creates a blueprint with fixture data and an isolated state dir; it returns the blueprint path.
func fortress(t *testing.T) string {
	t.Helper()
	t.Setenv(paths.EnvStateDir, t.TempDir())
	t.Setenv("SAPPER_RESTIC_PASSWORD", "")
	dir := t.TempDir()
	bpPath := filepath.Join(dir, blueprint.FileName)
	if r := run(t, "", "init", "-f", bpPath, "-q"); r.code != 0 {
		t.Fatalf("init: %+v", r)
	}
	bp, err := blueprint.Load(bpPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range bp.VolumePaths() {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "data"), []byte("fixture "+p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return bpPath
}

func TestCommands(t *testing.T) {
	bp := fortress(t)
	tests := []struct {
		name     string
		args     []string
		stdin    string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"validate", []string{"validate", "-f", bp}, "", 0, "valid (3 apps)", ""},
		{"validate json", []string{"validate", "-f", bp, "--json"}, "", 0, `"valid": true`, ""},
		{"missing blueprint", []string{"validate", "-f", bp + ".nope"}, "", ExitUsage, "", "run `sapper init`"},
		{"init refuses overwrite", []string{"init", "-f", bp}, "", ExitUsage, "", "--yes"},
		{"build dry run", []string{"build", "--dry-run", "-f", bp}, "", 0, "127.0.0.1:8081:80", ""},
		{"build bad runtime", []string{"build", "--up", "--runtime", "lxc", "-f", bp}, "", ExitUsage, "", "unknown runtime"},
		{"backup dry run", []string{"backup", "--dry-run", "-f", bp}, "", 0, "repository:", ""},
		{"backup without password", []string{"backup", "-f", bp}, "", ExitUsage, "", "SAPPER_RESTIC_PASSWORD"},
		{"empty stdin password", []string{"backup", "-f", bp, "--password-stdin"}, "\n", ExitUsage, "", "empty"},
		{"exclusive password flags", []string{"backup", "-f", bp, "--password-stdin", "--password-file", "x"}, "", ExitUsage, "", ""},
		{"unknown command suggests", []string{"breech"}, "", ExitUsage, "", "breach"},
		{"unknown flag", []string{"status", "--bogus"}, "", ExitUsage, "", "unknown flag"},
		{"status plain", []string{"status", "-f", bp, "--plain"}, "", 0, "drill      never", ""},
		{"status json", []string{"status", "-f", bp, "--json"}, "", 0, `"fortress": "my-fortress"`, ""},
		{"root shows art and help", []string{}, "", 0, "S·A·P·P·E·R", ""},
		{"completion bash", []string{"completion", "bash"}, "", 0, "bash completion", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, tc.stdin, tc.args...)
			if r.code != tc.wantCode {
				t.Fatalf("exit %d, want %d\nstdout: %s\nstderr: %s", r.code, tc.wantCode, r.stdout, r.stderr)
			}
			if !strings.Contains(r.stdout, tc.wantOut) {
				t.Errorf("stdout missing %q:\n%s", tc.wantOut, r.stdout)
			}
			if !strings.Contains(r.stderr, tc.wantErr) {
				t.Errorf("stderr missing %q:\n%s", tc.wantErr, r.stderr)
			}
			if strings.Contains(r.stdout, "\x1b[") {
				t.Errorf("escape codes written to a non-terminal")
			}
		})
	}
}

func TestBuildWritesArtifacts(t *testing.T) {
	bp := fortress(t)
	if r := run(t, "", "build", "-f", bp); r.code != 0 {
		t.Fatalf("%+v", r)
	}
	for _, f := range []string{"docker-compose.yml", "restic.env", "backup-paths.txt", "drill.cron"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(bp), ".sapper", f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

// TestDrillExitCodes drives the CLI against a real restic repository.
func TestDrillExitCodes(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	bp := fortress(t)
	pwFile := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pwFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "", "breach", "-f", bp, "--password-file", pwFile); r.code != ExitUnverified {
		t.Fatalf("breach before backup: exit %d, want %d\n%s", r.code, ExitUnverified, r.stderr)
	}
	r := run(t, "", "backup", "-f", bp, "--password-file", pwFile, "-v")
	if r.code != 0 || !strings.Contains(r.stderr, "$ restic backup") || strings.Contains(r.stderr, "secret") {
		t.Fatalf("backup: %+v", r)
	}
	r = run(t, "", "breach", "-f", bp, "--password-file", pwFile)
	if r.code != 0 || !strings.Contains(r.stdout, "S U R V I V E D") {
		t.Fatalf("clean breach: %+v", r)
	}
	r = run(t, "secret\n", "breach", "-f", bp, "--password-stdin", "--json")
	var out struct{ Verdict string }
	if r.code != 0 || json.Unmarshal([]byte(r.stdout), &out) != nil || out.Verdict != "SURVIVED" {
		t.Fatalf("json breach: %+v", r)
	}

	tamperManifest(t)
	r = run(t, "", "breach", "-f", bp, "--password-file", pwFile)
	if r.code != ExitBreached || !strings.Contains(r.stdout, "B R E A C H E D") || !strings.Contains(r.stdout, "checksum mismatch") {
		t.Fatalf("tampered breach: %+v", r)
	}
	if r := run(t, "", "status", "-f", bp, "--plain"); !strings.Contains(r.stdout, "BREACHED") {
		t.Fatalf("status after breach: %s", r.stdout)
	}
}

// tamperManifest flips one recorded checksum so the next drill must fail.
func tamperManifest(t *testing.T) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(os.Getenv(paths.EnvStateDir), "fortresses", "*", "manifests", "*.json"))
	if len(matches) != 1 {
		t.Fatalf("expected one manifest, found %v", matches)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	for _, e := range m["entries"].(map[string]any) {
		if entry := e.(map[string]any); entry["kind"] == "file" {
			entry["sha256"] = strings.Repeat("0", 64)
			break
		}
	}
	data, _ = json.Marshal(m)
	if err := os.WriteFile(matches[0], data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestHumanBytes(t *testing.T) {
	tests := map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 1536: "1.5 KiB", 1 << 20: "1.0 MiB"}
	for in, want := range tests {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
