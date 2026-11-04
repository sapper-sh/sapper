package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExampleIsValid(t *testing.T) {
	bp, err := Parse([]byte(Example))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bp.Dir = dir
	if err := bp.Validate(); err != nil {
		t.Fatalf("example blueprint invalid: %v", err)
	}
	if got, want := bp.HostPath("memos"), filepath.Join(dir, "data", "memos"); got != want {
		t.Errorf("HostPath = %q, want %q", got, want)
	}
	if got, want := bp.RepoLocation(), filepath.Join(dir, "backups", "restic"); got != want {
		t.Errorf("RepoLocation = %q, want %q", got, want)
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(string) string
		wantErr string
	}{
		{"bad version", rep("version: 1", "version: 2"), "version: must be 1"},
		{"bad fortress name", rep("fortress: my-fortress", "fortress: My_Fortress"), "fortress:"},
		{"missing data_root", rep("data_root: ./data", "data_root: \"\""), "data_root: required"},
		{"unknown app", rep("  memos:\n", "  ghost:\n"), "apps.ghost: not in catalogue"},
		{"duplicate port", rep("port: 8083", "port: 8081"), "already used"},
		{"port out of range", rep("port: 8083", "port: 70000"), "out of range"},
		{"missing mount", rep("      database: filebrowser/database\n", ""), "apps.filebrowser.volumes.database: required"},
		{"unknown mount", rep("      data: memos\n", "      data: memos\n      extra: x\n"), "unknown mount"},
		{"overlapping volumes", rep("data: memos", "data: vaultwarden/sub"), "overlaps"},
		{"repo inside volume", rep("repository: ./backups/restic", "repository: ./data/memos/repo"), "backup.repository"},
		{"bad env name", rep("password_env: SAPPER_RESTIC_PASSWORD", "password_env: lower"), "password_env"},
		{"negative keep", rep("keep_last: 14", "keep_last: -1"), "keep_last"},
		{"bad cron fields", rep(`"30 3 * * 0"`, `"30 3 *"`), "5 fields"},
		{"bad cron chars", rep(`"30 3 * * 0"`, `"30 3 * * sun"`), "invalid characters"},
		{"remote repo ok", rep("repository: ./backups/restic", "repository: sftp:me@host:/r"), ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bp, err := Parse([]byte(tc.edit(Example)))
			if err != nil {
				t.Fatal(err)
			}
			bp.Dir = t.TempDir()
			err = bp.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	if _, err := Parse([]byte(Example + "surprise: 1\n")); err == nil {
		t.Fatal("expected error for unknown key")
	}
}

func TestLoadResolvesDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, FileName)
	if err := os.WriteFile(p, []byte(Example), 0o600); err != nil {
		t.Fatal(err)
	}
	bp, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if bp.Dir != dir {
		t.Errorf("Dir = %q, want %q", bp.Dir, dir)
	}
	if n := len(bp.VolumePaths()); n != 4 {
		t.Errorf("VolumePaths has %d entries, want 4", n)
	}
}

func TestWithin(t *testing.T) {
	tests := []struct {
		parent, child string
		want          bool
	}{
		{"/a", "/a/b", true},
		{"/a", "/a", false},
		{"/a", "/ab", false},
		{"/a/b", "/a", false},
		{"/a", "/a/../b", false},
	}
	for _, tc := range tests {
		tc.parent, tc.child = filepath.FromSlash(tc.parent), filepath.FromSlash(tc.child)
		if got := within(tc.parent, tc.child); got != tc.want {
			t.Errorf("within(%q,%q) = %v, want %v", tc.parent, tc.child, got, tc.want)
		}
	}
}

func rep(old, new string) func(string) string {
	return func(s string) string {
		if !strings.Contains(s, old) {
			panic("fixture missing " + old)
		}
		return strings.Replace(s, old, new, 1)
	}
}

func TestTargets(t *testing.T) {
	good := map[string]time.Duration{"": 0, "90s": 90 * time.Second, "15m": 15 * time.Minute, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour}
	for in, want := range good {
		if got, err := ParseTarget(in); err != nil || got != want {
			t.Errorf("ParseTarget(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"0s", "-5m", "soon", "1.5d", "0d", "400d", "9000h", "15m\n", "d"} {
		if _, err := ParseTarget(in); err == nil {
			t.Errorf("ParseTarget(%q) accepted", in)
		}
	}
	bp, err := Parse([]byte(Example))
	if err != nil {
		t.Fatal(err)
	}
	bp.Targets = Targets{RTO: "never", RPO: "-1h"}
	err = bp.Validate()
	if err == nil || !strings.Contains(err.Error(), "targets.rto") || !strings.Contains(err.Error(), "targets.rpo") {
		t.Fatalf("Validate = %v", err)
	}
	if _, err := Parse([]byte(Example + "  rtx: 5m\n")); err == nil {
		t.Fatal("unknown key under targets accepted")
	}
}
