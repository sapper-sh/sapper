package report

import (
	"crypto/ed25519"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRotationAndRevocation checks a chain signed by an old key and then a new one:
// it verifies with both keys, fails with only the new key, and fails when the old
// key is revoked.
func TestRotationAndRevocation(t *testing.T) {
	oldPub, oldKey, _ := ed25519.GenerateKey(nil)
	newPub, newKey, _ := ed25519.GenerateKey(nil)
	s := Store{Dir: filepath.Join(t.TempDir(), "reports")}
	for i, k := range []ed25519.PrivateKey{oldKey, oldKey, newKey} {
		in := input()
		in.Started = t0.Add(time.Duration(i) * time.Hour)
		if _, err := s.Append(Build(in), k); err != nil {
			t.Fatal(err)
		}
	}
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if issues := VerifyChainWith(all, VerifyOptions{Keys: []ed25519.PublicKey{oldPub, newPub}}); len(issues) != 0 {
		t.Fatalf("both keys: %+v", issues)
	}
	if issues := VerifyChainWith(all, VerifyOptions{Keys: []ed25519.PublicKey{newPub}}); len(issues) != 2 {
		t.Fatalf("new key only: want 2 issues, got %+v", issues)
	}
	rev := map[string]bool{KeyID(oldPub): true}
	issues := VerifyChainWith(all, VerifyOptions{Keys: []ed25519.PublicKey{oldPub, newPub}, Revoked: rev})
	if len(issues) != 2 || !strings.Contains(issues[0].Problem, "revoked") {
		t.Fatalf("revoked old key: %+v", issues)
	}
	if issues := VerifyChainWith(all, VerifyOptions{Revoked: rev}); len(issues) != 2 {
		t.Fatalf("revocation must apply without pinned keys too: %+v", issues)
	}
}

func TestParseRevocations(t *testing.T) {
	m, err := ParseRevocations([]byte("# revoked 2026-11-04, laptop lost\n0123456789ABCDEF  # old key\n\nfedcba9876543210\n"))
	if err != nil || len(m) != 2 || !m["0123456789abcdef"] || !m["fedcba9876543210"] {
		t.Fatalf("got %v, %v", m, err)
	}
	for _, bad := range []string{"0123", "0123456789abcdeg", "0123456789abcdef extra"} {
		if _, err := ParseRevocations([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
