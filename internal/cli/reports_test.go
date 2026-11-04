package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapper-unbuilt/sapper/internal/paths"
)

// TestReportsEndToEnd drives drills, signing and chain verification through the CLI with real restic.
func TestReportsEndToEnd(t *testing.T) {
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	bp := fortress(t)
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "drill.key")
	r := run(t, "", "keygen", "--out", key)
	privPEM, _ := os.ReadFile(key)
	privBody := strings.Split(string(privPEM), "\n")[1]
	if r.code != 0 || strings.Contains(r.stdout+r.stderr, privBody) || !strings.Contains(r.stdout, "key id") {
		t.Fatalf("keygen: %+v", r)
	}
	if r := run(t, "", "keygen", "--out", key); r.code == 0 {
		t.Fatal("keygen overwrote a key")
	}

	r = run(t, "", "breach", "-f", bp, "--password-file", pwFile, "--plain")
	if r.code != ExitUnverified || !strings.Contains(r.stdout, "NO VERDICT") || !strings.Contains(r.stdout, "There is no backup to test yet.") {
		t.Fatalf("no-snapshot drill: %+v", r)
	}
	if r := run(t, "", "backup", "-f", bp, "--password-file", pwFile); r.code != 0 {
		t.Fatalf("backup: %+v", r)
	}
	r = run(t, "", "breach", "-f", bp, "--password-file", pwFile, "--sign-key", key)
	if r.code != 0 {
		t.Fatalf("signed drill: %+v", r)
	}
	for _, want := range []string{"S U R V I V E D", "The latest backup restores correctly.", "What was tested", "Evidence", "Timings", "Restore time", "What to do next", "Signed        ed25519"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("survived output missing %q:\n%s", want, r.stdout)
		}
	}
	if strings.Index(r.stdout, "S U R V I V E D") > strings.Index(r.stdout, "What was tested") {
		t.Error("verdict is not first")
	}
	for _, l := range strings.Split(r.stdout, "\n") {
		if n := len([]rune(l)); n > 80 {
			t.Errorf("line of %d columns: %q", n, l)
		}
	}
	r = run(t, "", "breach", "-f", bp, "--password-file", pwFile, "--brief")
	if r.code != 0 || strings.Count(r.stdout, "\n") != 1 || !strings.HasPrefix(r.stdout, "SURVIVED  my-fortress  snapshot ") {
		t.Fatalf("brief: %q", r.stdout)
	}
	tamperManifest(t)
	r = run(t, "", "breach", "-f", bp, "--password-file", pwFile, "--json")
	var rep struct {
		Verdict  string
		Seq      int
		Evidence struct {
			Findings []struct{ Kind, Expected, Actual string }
		}
		NextSteps []string `json:"next_steps"`
	}
	if r.code != ExitBreached || json.Unmarshal([]byte(r.stdout), &rep) != nil || rep.Verdict != "BREACHED" || rep.Seq != 4 ||
		// The edited local manifest no longer matches the copy kept in the repository, and
		// the restored file no longer matches the edited checksum.
		len(rep.Evidence.Findings) != 2 || rep.Evidence.Findings[0].Kind != "manifest-mismatch" ||
		rep.Evidence.Findings[1].Kind != "content" || rep.Evidence.Findings[1].Expected != strings.Repeat("0", 64) {
		t.Fatalf("breached json: %+v\n%s", rep, r.stdout)
	}

	r = run(t, "", "reports", "-f", bp)
	if r.code != 0 || strings.Count(r.stdout, "\n") != 5 || !strings.Contains(r.stdout, "NO VERDICT") || !strings.Contains(r.stdout, "BREACHED") {
		t.Fatalf("reports: %+v", r)
	}
	if r := run(t, "", "reports", "show", "4", "-f", bp, "--markdown"); r.code != 0 || !strings.HasPrefix(r.stdout, "# Breach drill: my-fortress BREACHED") {
		t.Fatalf("show markdown: %+v", r)
	}
	r = run(t, "", "reports", "verify", "-f", bp)
	if r.code != 0 || !strings.Contains(r.stdout, "Chain intact: 4 reports") {
		t.Fatalf("verify: %+v", r)
	}
	// Only report #2 was signed, so pinning the key flags the other three.
	if r := run(t, "", "reports", "verify", "-f", bp, "--pub-key", key+".pub"); r.code != ExitBreached || strings.Count(r.stdout, "not signed") != 3 {
		t.Fatalf("verify pinned: %+v", r)
	}

	matches, _ := filepath.Glob(filepath.Join(os.Getenv(paths.EnvStateDir), "fortresses", "*", "reports", "000004.json"))
	if len(matches) != 1 {
		t.Fatalf("report files: %v", matches)
	}
	data, _ := os.ReadFile(matches[0])
	if err := os.WriteFile(matches[0], []byte(strings.Replace(string(data), `"verdict": "BREACHED"`, `"verdict": "SURVIVED"`, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	r = run(t, "", "reports", "verify", "-f", bp)
	if r.code != ExitBreached || !strings.Contains(r.stdout, "#4") || !strings.Contains(r.stdout, "contents changed") {
		t.Fatalf("tampered verify: %+v", r)
	}
}
