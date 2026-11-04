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
	"runtime"
	"sort"
	"strings"
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

// Problem kinds. Reports group findings by kind and suggest a next step for each.
const (
	ProblemMissing    = "missing"     // in the manifest, absent from the restore
	ProblemUnexpected = "unexpected"  // in the restore, absent from the manifest
	ProblemContent    = "content"     // same size, different sha256
	ProblemSize       = "size"        // different size
	ProblemType       = "type"        // file became a directory, and so on
	ProblemSymlink    = "symlink"     // symlink points somewhere else
	ProblemMode       = "mode"        // permission bits differ
	ProblemUnsafe     = "unsafe-path" // manifest path escapes, or a restored parent is a symlink
)

// Problem is one verification failure. Expected and Actual hold the compared values
// (hashes, sizes, modes, link targets) when there are any.
type Problem struct {
	Path     string `json:"path"`
	Reason   string `json:"reason"`
	Kind     string `json:"kind,omitempty"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
}

// Result of verifying a restored tree.
type Result struct {
	Checked  int       `json:"checked"`
	Matched  int       `json:"matched"`
	Files    int       `json:"files"`
	Dirs     int       `json:"dirs"`
	Symlinks int       `json:"symlinks"`
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
		r.Checked++
		switch want.Kind {
		case KindFile:
			r.Files++
		case KindDir:
			r.Dirs++
		case KindSymlink:
			r.Symlinks++
		}
		if !underRoot(m.Roots, p) {
			r.Problems = append(r.Problems, Problem{Path: p, Kind: ProblemUnsafe, Reason: "manifest path outside backup roots"})
			continue
		}
		rp := RestoredPath(base, p)
		if !inside(base, rp) {
			r.Problems = append(r.Problems, Problem{Path: p, Kind: ProblemUnsafe, Reason: "manifest path escapes restore directory"})
			continue
		}
		if link := symlinkAncestor(base, rp); link != "" {
			// Never follow a restored symlink: it could point anywhere on this host.
			r.Problems = append(r.Problems, Problem{Path: p, Kind: ProblemUnsafe, Reason: "parent is a symlink in restore: " + link})
			continue
		}
		got, err := statEntry(rp)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			r.Problems = append(r.Problems, Problem{Path: p, Kind: ProblemMissing, Reason: "missing", Expected: want.Kind})
			continue
		case err != nil:
			return r, err
		}
		if pr, bad := compare(want, got); bad {
			pr.Path = p
			r.Problems = append(r.Problems, pr)
			continue
		}
		r.Matched++
		r.Bytes += got.Size
	}
	expected := make(map[string]bool, len(m.Entries))
	for p := range m.Entries {
		expected[RestoredPath(base, p)] = true
	}
	for _, root := range m.Roots {
		rroot := RestoredPath(base, root)
		err := filepath.WalkDir(rroot, func(rp string, _ fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) && rp == rroot {
					return nil
				}
				return err
			}
			if !expected[rp] {
				r.Problems = append(r.Problems, Problem{Path: rp, Kind: ProblemUnexpected, Reason: "unexpected in restore"})
			}
			return nil
		})
		if err != nil {
			return r, err
		}
	}
	return r, nil
}

// underRoot reports whether p is a clean absolute path equal to or inside one of roots.
func underRoot(roots []string, p string) bool {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return false
	}
	for _, root := range roots {
		if root == p || inside(root, p) {
			return true
		}
	}
	return false
}

// inside reports whether child is strictly inside parent, lexically.
func inside(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// symlinkAncestor returns the first directory between base (exclusive) and p (exclusive) that is a symlink.
func symlinkAncestor(base, p string) string {
	rel, err := filepath.Rel(base, filepath.Dir(p))
	if err != nil || rel == "." {
		return ""
	}
	cur := base
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		if info, err := os.Lstat(cur); err == nil && info.Mode()&fs.ModeSymlink != 0 {
			return cur
		}
	}
	return ""
}

func compare(want, got Entry) (Problem, bool) {
	switch {
	case want.Kind != got.Kind:
		return Problem{Kind: ProblemType, Reason: fmt.Sprintf("type changed: %s -> %s", want.Kind, got.Kind), Expected: want.Kind, Actual: got.Kind}, true
	case want.Kind == KindFile && want.Size != got.Size:
		return Problem{Kind: ProblemSize, Reason: fmt.Sprintf("size %d, expected %d", got.Size, want.Size),
			Expected: fmt.Sprintf("%d bytes, sha256 %s", want.Size, want.SHA256), Actual: fmt.Sprintf("%d bytes, sha256 %s", got.Size, got.SHA256)}, true
	case want.SHA256 != got.SHA256:
		return Problem{Kind: ProblemContent, Reason: "checksum mismatch", Expected: want.SHA256, Actual: got.SHA256}, true
	case want.Target != got.Target:
		return Problem{Kind: ProblemSymlink, Reason: fmt.Sprintf("symlink target %q, expected %q", got.Target, want.Target), Expected: want.Target, Actual: got.Target}, true
	case runtime.GOOS != "windows" && want.Mode != got.Mode:
		return Problem{Kind: ProblemMode, Reason: fmt.Sprintf("mode %v, expected %v", got.Mode, want.Mode), Expected: want.Mode.String(), Actual: got.Mode.String()}, true
	}
	return Problem{}, false
}

func statEntry(p string) (Entry, error) {
	info, err := os.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	return entryFor(p, fs.FileInfoToDirEntry(info))
}

// RestoredPath returns where restic places original absolute path p when restoring into base.
// On Windows restic maps a volume such as C: to a directory named C.
func RestoredPath(base, p string) string {
	if v := filepath.VolumeName(p); v != "" {
		return filepath.Join(base, strings.TrimSuffix(v, ":"), p[len(v):])
	}
	return filepath.Join(base, p)
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
