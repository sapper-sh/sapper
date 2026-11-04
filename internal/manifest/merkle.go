package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

// MerkleRoot returns a hex sha256 Merkle root over every manifest entry, in path order.
// Leaves and inner nodes are domain-separated as in RFC 6962 (0x00 leaf, 0x01 node) so a
// leaf can never be passed off as a node. An odd node at the end of a level is promoted.
// The root changes if any path, type, size, checksum, link target or mode changes, so two
// reports with the same root verified the same tree.
func (m *Manifest) MerkleRoot() string {
	paths := make([]string, 0, len(m.Entries))
	for p := range m.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	level := make([][]byte, 0, len(paths))
	for _, p := range paths {
		e := m.Entries[p]
		h := sha256.New()
		h.Write([]byte{0})
		for _, f := range []string{p, e.Kind, strconv.FormatInt(e.Size, 10), e.SHA256, e.Target, strconv.FormatUint(uint64(e.Mode), 8)} {
			// Length-prefix each field so "ab"+"c" and "a"+"bc" hash differently.
			h.Write([]byte(strconv.Itoa(len(f)) + ":" + f))
		}
		level = append(level, h.Sum(nil))
	}
	if len(level) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}
	for len(level) > 1 {
		next := make([][]byte, 0, (len(level)+1)/2)
		for i := 0; i < len(level); i += 2 {
			if i+1 == len(level) {
				next = append(next, level[i])
				continue
			}
			h := sha256.New()
			h.Write([]byte{1})
			h.Write(level[i])
			h.Write(level[i+1])
			next = append(next, h.Sum(nil))
		}
		level = next
	}
	return hex.EncodeToString(level[0])
}

// Fingerprint is a cheap stat-only summary of the trees under roots: path, type, size,
// mode and modification time. Breach drills take one before restoring and one after
// verifying, to show that live data was not touched. It reads no file contents.
func Fingerprint(roots []string) (map[string]string, error) {
	out := map[string]string{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) && p == root {
					return nil
				}
				return err
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			out[p] = info.Mode().String() + " " + strconv.FormatInt(info.Size(), 10) + " " + strconv.FormatInt(info.ModTime().UnixNano(), 10)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// FingerprintDiff returns the paths whose fingerprint differs between a and b, sorted.
func FingerprintDiff(a, b map[string]string) []string {
	var out []string
	for p, v := range a {
		if b[p] != v {
			out = append(out, p)
		}
	}
	for p := range b {
		if _, ok := a[p]; !ok {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}
