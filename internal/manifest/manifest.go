// Package manifest records file checksums for a set of directories and verifies a restored copy against them.
package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// Kind of a manifest entry.
const (
	KindFile    = "file"
	KindDir     = "dir"
	KindSymlink = "symlink"
)

// Entry describes one path.
type Entry struct {
	Kind   string      `json:"kind"`
	Size   int64       `json:"size,omitempty"`
	SHA256 string      `json:"sha256,omitempty"`
	Target string      `json:"target,omitempty"`
	Mode   fs.FileMode `json:"mode"`
}

// Manifest maps absolute paths to entries.
type Manifest struct {
	Roots   []string         `json:"roots"`
	Entries map[string]Entry `json:"entries"`
}

// Build walks roots (absolute paths) and records every entry. Symlinks are recorded, not followed.
func Build(roots []string) (*Manifest, error) {
	m := &Manifest{Entries: map[string]Entry{}}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return nil, fmt.Errorf("root %q is not absolute", root)
		}
		root = filepath.Clean(root)
		m.Roots = append(m.Roots, root)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			e, err := entryFor(p, d)
			if err != nil {
				return err
			}
			m.Entries[p] = e
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(m.Roots)
	return m, nil
}

func entryFor(p string, d fs.DirEntry) (Entry, error) {
	info, err := d.Info()
	if err != nil {
		return Entry{}, err
	}
	switch {
	case d.Type()&fs.ModeSymlink != 0:
		t, err := os.Readlink(p)
		return Entry{Kind: KindSymlink, Target: t, Mode: info.Mode().Perm()}, err
	case d.IsDir():
		return Entry{Kind: KindDir, Mode: info.Mode().Perm()}, nil
	case d.Type().IsRegular():
		sum, err := hashFile(p)
		return Entry{Kind: KindFile, Size: info.Size(), SHA256: sum, Mode: info.Mode().Perm()}, err
	default:
		return Entry{}, fmt.Errorf("%s: unsupported file type %v", p, d.Type())
	}
}

func hashFile(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Equal reports whether two manifests describe identical trees.
func Equal(a, b *Manifest) bool {
	if len(a.Entries) != len(b.Entries) {
		return false
	}
	for p, e := range a.Entries {
		if b.Entries[p] != e {
			return false
		}
	}
	return true
}

// Problem is one verification failure.
type Problem struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Result of verifying a restored tree.
type Result struct {
	Checked  int       `json:"checked"`
	Bytes    int64     `json:"bytes"`
	Problems []Problem `json:"problems"`
}

// OK reports whether verification found no problems.
func (r Result) OK() bool { return len(r.Problems) == 0 }

// Verify checks a tree restored under base, where each original absolute path p is expected at base+p.
// Paths present in the restore but absent from the manifest are reported as unexpected.
func Verify(m *Manifest, base string) (Result, error) {
	var r Result
	paths := make([]string, 0, len(m.Entries))
	for p := range m.Entries {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		want := m.Entries[p]
		got, err := statEntry(filepath.Join(base, p))
		r.Checked++
		switch {
		case errors.Is(err, fs.ErrNotExist):
			r.Problems = append(r.Problems, Problem{p, "missing"})
			continue
		case err != nil:
			return r, err
		}
		if reason := compare(want, got); reason != "" {
			r.Problems = append(r.Problems, Problem{p, reason})
			continue
		}
		r.Bytes += got.Size
	}
	for _, root := range m.Roots {
		rroot := filepath.Join(base, root)
		err := filepath.WalkDir(rroot, func(rp string, _ fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && rp == rroot {
					return nil
				}
				return err
			}
			orig := "/" + mustRel(base, rp)
			if _, ok := m.Entries[filepath.Clean(orig)]; !ok {
				r.Problems = append(r.Problems, Problem{filepath.Clean(orig), "unexpected in restore"})
			}
			return nil
		})
		if err != nil {
			return r, err
		}
	}
	return r, nil
}

func compare(want, got Entry) string {
	switch {
	case want.Kind != got.Kind:
		return fmt.Sprintf("type changed: %s -> %s", want.Kind, got.Kind)
	case want.Kind == KindFile && want.Size != got.Size:
		return fmt.Sprintf("size %d, expected %d", got.Size, want.Size)
	case want.SHA256 != got.SHA256:
		return "checksum mismatch"
	case want.Target != got.Target:
		return fmt.Sprintf("symlink target %q, expected %q", got.Target, want.Target)
	case want.Mode != got.Mode:
		return fmt.Sprintf("mode %v, expected %v", got.Mode, want.Mode)
	}
	return ""
}

func statEntry(p string) (Entry, error) {
	info, err := os.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	return entryFor(p, fs.FileInfoToDirEntry(info))
}

func mustRel(base, p string) string {
	r, err := filepath.Rel(base, p)
	if err != nil {
		return p
	}
	return r
}

// Save writes m as JSON with private permissions.
func Save(path string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Load reads a manifest written by Save.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Entries == nil {
		return nil, fmt.Errorf("%s: no entries", path)
	}
	return &m, nil
}
