package blueprint

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Regression tests for values that would inject extra lines or fields into the
// rendered compose file, restic.env, backup-paths.txt or drill.cron.
func TestValidateRejectsInjection(t *testing.T) {
	tests := []struct {
		name    string
		edit    func(string) string
		wantErr string
	}{
		{"newline in volume", rep("data: memos", `data: "memos\n/root/.ssh"`), "control characters"},
		{"colon in volume", rep("data: memos", `data: "memos:/etc:rw"`), "must not contain ':'"},
		{"comma in volume", rep("data: memos", `data: "memos,x"`), "must not contain ':'"},
		{"dash volume", rep("data: memos", `data: "-memos"`), "must not start with '-'"},
		{"newline in repository", rep("repository: ./backups/restic", `repository: "./r\nRESTIC_PASSWORD_COMMAND=sh -c id"`), "backup.repository: must not contain control"},
		{"NUL in data_root", rep("data_root: ./data", `data_root: "./d\x00x"`), "data_root"},
	}
	if runtime.GOOS != "windows" {
		tests = append(tests,
			struct {
				name    string
				edit    func(string) string
				wantErr string
			}{"docker socket", rep("data: memos", "data: /var/run/docker.sock"), "sensitive host path"},
			struct {
				name    string
				edit    func(string) string
				wantErr string
			}{"filesystem root", rep("data: memos", "data: /"), "filesystem root"},
			struct {
				name    string
				edit    func(string) string
				wantErr string
			}{"etc", rep("data: memos", "data: /etc/ssl"), "sensitive host path"},
			struct {
				name    string
				edit    func(string) string
				wantErr string
			}{"other socket", rep("data: memos", "data: /srv/x/agent.sock"), "socket"},
		)
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bp, err := Parse([]byte(tc.edit(Example)))
			if err != nil {
				t.Fatal(err)
			}
			bp.Dir = t.TempDir()
			err = bp.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadRejectsHugeBlueprint(t *testing.T) {
	p := filepath.Join(t.TempDir(), FileName)
	big := Example + "# " + strings.Repeat("x", MaxSize) + "\n"
	if err := os.WriteFile(p, []byte(big), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("Load error = %v, want size limit", err)
	}
	if _, err := Parse([]byte(big)); err == nil {
		t.Fatal("Parse accepted oversized blueprint")
	}
}

func TestParseBillionLaughs(t *testing.T) {
	// yaml.v3 >= 3.0.0 bounds alias expansion; make sure that stays true for our decoder.
	y := "a: &a [\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\",\"x\"]\n"
	prev := "a"
	for _, n := range []string{"b", "c", "d", "e", "f", "g", "h", "i"} {
		y += n + ": &" + n + " [*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + ",*" + prev + "]\n"
		prev = n
	}
	if _, err := Parse([]byte(y)); err == nil {
		t.Fatal("expected error")
	}
}

func TestRedactRepo(t *testing.T) {
	for in, want := range map[string]string{
		"rest:https://alice:hunter2@backup.example:8000/repo": "rest:https://alice:***@backup.example:8000/repo",
		"s3:https://s3.example/bucket":                        "s3:https://s3.example/bucket",
		"rest:https://alice@host/":                            "rest:https://alice@host/",
		"sftp:me@host:/r":                                     "sftp:me@host:/r",
		"/srv/backups/restic":                                 "/srv/backups/restic",
		"rest:https://u:p@ss@host/x?y=@":                      "rest:https://u:***@host/x?y=@",
	} {
		if got := RedactRepo(in); got != want {
			t.Errorf("RedactRepo(%q) = %q, want %q", in, got, want)
		}
	}
}

// restic passes the sftp host to ssh as its first argument; a leading '-' becomes an ssh
// option such as ProxyCommand (compare kopia GHSA-2q4c-3mrw-63c3).
func TestValidateRejectsRepoOptionInjection(t *testing.T) {
	bad := []string{
		"sftp:-oProxyCommand=touch$IFS/tmp/pwned:/x",
		"sftp://-oProxyCommand=id/x",
		"sftp:-oProxyCommand=id@host:/x",
		"sftp:me@-oProxyCommand=id:/x",
		"sftp:me@host x:/r",
		"rclone:--config=/tmp/evil:x",
		"-r",
	}
	for _, repo := range bad {
		bp, err := Parse([]byte(rep("repository: ./backups/restic", "repository: '"+repo+"'")(Example)))
		if err != nil {
			t.Fatal(err)
		}
		bp.Dir = t.TempDir()
		if err := bp.Validate(); err == nil || !strings.Contains(err.Error(), "backup.repository") {
			t.Errorf("%q: err = %v", repo, err)
		}
	}
	for _, repo := range []string{"sftp:me@host:/srv/restic", "sftp://me@host:2222//srv/restic", "rclone:b2prod:bucket/path", "s3:https://s3.example/bucket-1"} {
		bp, _ := Parse([]byte(rep("repository: ./backups/restic", "repository: '"+repo+"'")(Example)))
		bp.Dir = t.TempDir()
		if err := bp.Validate(); err != nil {
			t.Errorf("%q rejected: %v", repo, err)
		}
	}
}
