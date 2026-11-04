package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBreachStrict checks that --strict turns a missed RTO into exit 7, and that the
// same drill without --strict still exits 0.
func TestBreachStrict(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	bp := fortress(t)
	data, err := os.ReadFile(bp)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "rto: 30m") {
		t.Fatal("example blueprint has no rto target")
	}
	if err := os.WriteFile(bp, []byte(strings.Replace(string(data), "rto: 30m", "rto: 1ns", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	pw := filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pw, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "", "backup", "-f", bp, "--password-file", pw); r.code != 0 {
		t.Fatalf("backup: %+v", r)
	}
	if r := run(t, "", "breach", "-f", bp, "--password-file", pw, "--brief"); r.code != ExitOK {
		t.Fatalf("without --strict a missed target must still exit 0: %+v", r)
	}
	r := run(t, "", "breach", "-f", bp, "--password-file", pw, "--brief", "--strict")
	if r.code != ExitTargets || !strings.Contains(r.stderr, "RTO") {
		t.Fatalf("--strict: want exit %d naming RTO, got %+v", ExitTargets, r)
	}
}

// TestVerifyRevokedFlag checks the --revoked flag end to end against a signed chain.
func TestVerifyRevokedFlag(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	bp := fortress(t)
	dir := t.TempDir()
	pw := filepath.Join(dir, "pw")
	if err := os.WriteFile(pw, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "drill.key")
	r := run(t, "", "keygen", "--out", key, "--json")
	if r.code != 0 {
		t.Fatalf("keygen: %+v", r)
	}
	id := r.stdout[strings.Index(r.stdout, `"key_id": "`)+11:][:16]
	if run(t, "", "backup", "-f", bp, "--password-file", pw).code != 0 {
		t.Fatal("backup failed")
	}
	if r := run(t, "", "breach", "-f", bp, "--password-file", pw, "--sign-key", key, "--brief"); r.code != 0 {
		t.Fatalf("breach: %+v", r)
	}
	if r := run(t, "", "reports", "-f", bp, "verify", "--pub-key", key+".pub"); r.code != 0 {
		t.Fatalf("verify: %+v", r)
	}
	rev := filepath.Join(dir, "revoked.txt")
	if err := os.WriteFile(rev, []byte(id+"  # test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := run(t, "", "reports", "-f", bp, "verify", "--revoked", rev); r.code != ExitBreached || !strings.Contains(r.stdout, "revoked") {
		t.Fatalf("verify --revoked: %+v", r)
	}
	if r := run(t, "", "reports", "-f", bp, "verify", "--pub-key", key+".pub", "--revoked", rev); r.code != ExitUsage {
		t.Fatalf("trusting a revoked key must be a usage error: %+v", r)
	}
}
