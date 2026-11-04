package ops

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapper-sh/sapper/internal/manifest"
)

func tree(t *testing.T, files map[string][]byte) (string, *manifest.Manifest) {
	t.Helper()
	root := t.TempDir()
	for name, b := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := manifest.Build([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	return root, m
}

func text(n int) []byte { return []byte(strings.Repeat("the quick brown fox ", n)) }

func random(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func kinds(as []Anomaly) string {
	var k []string
	for _, a := range as {
		k = append(k, a.Kind)
	}
	return strings.Join(k, ",")
}

func TestAnomalies(t *testing.T) {
	files := map[string][]byte{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f"} {
		files[n+".txt"] = text(100)
	}
	root, prev := tree(t, files)

	if got := kinds(Anomalies(prev, prev, []string{root})); got != "" {
		t.Fatalf("unchanged tree: %s", got)
	}
	if got := kinds(Anomalies(nil, prev, []string{root})); got != "" {
		t.Fatalf("first backup of a full tree: %s", got)
	}

	// Ordinary edits to text files are not suspicious, even all of them.
	for n := range files {
		_ = os.WriteFile(filepath.Join(root, n), text(101), 0o644)
	}
	cur, _ := manifest.Build([]string{root})
	if got := kinds(Anomalies(prev, cur, []string{root})); got != "" {
		t.Fatalf("text edits flagged: %s", got)
	}

	// Ransomware: every file replaced by random bytes.
	for n := range files {
		_ = os.WriteFile(filepath.Join(root, n), random(t, 2000), 0o644)
	}
	cur, _ = manifest.Build([]string{root})
	if got := kinds(Anomalies(prev, cur, []string{root})); got != "mass-change" {
		t.Fatalf("encrypted tree: %q", got)
	}

	// Ransomware that renames: originals gone, *.locked copies added.
	for n := range files {
		_ = os.Remove(filepath.Join(root, n))
		_ = os.WriteFile(filepath.Join(root, n+".locked"), random(t, 2000), 0o644)
	}
	cur, _ = manifest.Build([]string{root})
	if got := kinds(Anomalies(prev, cur, []string{root})); got != "mass-change" {
		t.Fatalf("renamed encrypted tree: %q", got)
	}

	// rm -rf: most files gone.
	for _, e := range []string{"a", "b", "c", "d", "e", "f"} {
		_ = os.Remove(filepath.Join(root, e+".txt.locked"))
	}
	_ = os.WriteFile(filepath.Join(root, "a.txt"), text(100), 0o644)
	cur, _ = manifest.Build([]string{root})
	if got := kinds(Anomalies(prev, cur, []string{root})); got != "shrank" {
		t.Fatalf("mostly deleted: %q", got)
	}

	// Unmounted: empty directory, even on a first backup.
	empty := t.TempDir()
	cur, _ = manifest.Build([]string{empty})
	if got := kinds(Anomalies(nil, cur, []string{empty})); got != "empty-volume" {
		t.Fatalf("empty: %q", got)
	}
}

func TestEntropy(t *testing.T) {
	if h := entropy(text(100)); h > 5 {
		t.Fatalf("text entropy %.2f", h)
	}
	if h := entropy(random(t, 4096)); h < 7.5 {
		t.Fatalf("random entropy %.2f", h)
	}
	if entropy(nil) != 0 {
		t.Fatal("empty input")
	}
}

func TestManifestCopyRoundTripAndSignature(t *testing.T) {
	_, m := tree(t, map[string][]byte{"x": text(3)})
	pub, priv, _ := ed25519.GenerateKey(nil)
	otherPub, _, _ := ed25519.GenerateKey(nil)
	id := strings.Repeat("ab", 32)

	unsigned, err := encodeCopy(id, m, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, signed, err := decodeCopy(unsigned, id, nil)
	if err != nil || signed || got.Sum() != m.Sum() {
		t.Fatalf("unsigned round trip: %v signed=%v", err, signed)
	}
	if _, _, err := decodeCopy(unsigned, id, pub); !errors.Is(err, ErrManifestSignature) {
		t.Fatalf("unsigned copy accepted with a required key: %v", err)
	}
	if _, _, err := decodeCopy(unsigned, strings.Repeat("cd", 32), nil); err == nil {
		t.Fatal("copy accepted for another snapshot")
	}

	sig, _ := encodeCopy(id, m, priv)
	if _, signed, err := decodeCopy(sig, id, pub); err != nil || !signed {
		t.Fatalf("signed copy: %v", err)
	}
	if _, _, err := decodeCopy(sig, id, otherPub); !errors.Is(err, ErrManifestSignature) {
		t.Fatalf("copy signed by another key accepted: %v", err)
	}
	tampered := strings.Replace(string(sig), `"kind":"file"`, `"kind":"dir"`, 1)
	if _, _, err := decodeCopy([]byte(tampered), id, nil); err == nil {
		t.Fatal("edited copy accepted")
	}
}

func TestCheckSpace(t *testing.T) {
	_, m := tree(t, map[string][]byte{"x": text(1000)})
	if err := checkSpace(t.TempDir(), m, func(string) (uint64, error) { return 10, nil }); !errors.Is(err, ErrNoSpace) {
		t.Fatalf("full disk: %v", err)
	}
	if err := checkSpace(t.TempDir(), m, func(string) (uint64, error) { return 1 << 30, nil }); err != nil {
		t.Fatalf("plenty of space: %v", err)
	}
	if err := checkSpace(t.TempDir(), m, func(string) (uint64, error) { return 0, errors.New("unknown") }); err != nil {
		t.Fatalf("unknown space must not block: %v", err)
	}
}
