package cli

import (
	"os"
	"strings"
	"testing"
)

func TestBackupDryRunRedactsRepoPassword(t *testing.T) {
	bp := fortress(t)
	raw, err := os.ReadFile(bp)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "repository: ./backups/restic", "repository: rest:https://alice:hunter2@backup.example/repo", 1)
	if err := os.WriteFile(bp, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"backup", "--dry-run", "-f", bp}, {"backup", "--dry-run", "--json", "-f", bp}} {
		r := run(t, "", args...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
		if strings.Contains(r.stdout+r.stderr, "hunter2") {
			t.Errorf("%v leaked repository password:\n%s%s", args, r.stdout, r.stderr)
		}
		if !strings.Contains(r.stdout, "alice:***@backup.example") {
			t.Errorf("%v: redacted repository missing:\n%s", args, r.stdout)
		}
	}
}
