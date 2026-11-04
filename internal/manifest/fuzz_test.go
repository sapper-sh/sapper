package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FuzzVerify feeds arbitrary manifests to Verify against a fixed restore tree and checks it
// never reads outside the restore directory: an outside file's bytes must never be counted.
func FuzzVerify(f *testing.F) {
	f.Add([]byte(`{"roots":["/r"],"entries":{"/r":{"kind":"dir","mode":493},"/r/f":{"kind":"file","size":2,"sha256":"x","mode":420}}}`))
	f.Add([]byte(`{"roots":["/r"],"entries":{"/../../../../../../../etc/hostname":{"kind":"file"}}}`))
	f.Add([]byte(`{"roots":["/"],"entries":{"/r/link/secret":{"kind":"file"}}}`))
	f.Add([]byte(`{"roots":["/r/.."],"entries":{"/r/../../outside":{"kind":"file"}}}`))
	base := f.TempDir()
	outside := f.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("0123456789abcdef"), 0o600); err != nil {
		f.Fatal(err)
	}
	must := func(err error) {
		if err != nil {
			f.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(base, "r"), 0o755))
	must(os.WriteFile(filepath.Join(base, "r", "f"), []byte("hi"), 0o644))
	must(os.Symlink(outside, filepath.Join(base, "r", "link")))
	f.Fuzz(func(t *testing.T, raw []byte) {
		var m Manifest
		if json.Unmarshal(raw, &m) != nil || m.Entries == nil {
			return
		}
		r, err := Verify(&m, base)
		if err != nil {
			return
		}
		if r.Bytes > 2 {
			t.Fatalf("Verify counted %d bytes; only base/r/f (2 bytes) is inside the restore", r.Bytes)
		}
		if r.Checked != len(m.Entries) {
			t.Fatalf("checked %d of %d entries", r.Checked, len(m.Entries))
		}
	})
}
