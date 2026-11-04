package report

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// Store keeps a fortress's reports as NNNNNN.json (what the chain hashes) and
// NNNNNN.md (a copy for people) in one private directory.
type Store struct{ Dir string }

// maxReportFile bounds how much of a report file is read; reports are small, and a
// planted multi-gigabyte file should not exhaust memory.
const maxReportFile = 32 << 20

var reportName = regexp.MustCompile(`^([0-9]{6,9})\.json$`)

// Append seals r onto the end of the chain, signs it when key is not nil, and writes
// both files. It never overwrites an existing report: if another drill took the same
// number first, it reloads the chain and tries again.
func (s Store) Append(r *Report, key ed25519.PrivateKey) (string, error) {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return "", err
	}
	for range 5 {
		prev, err := s.Latest()
		if err != nil {
			return "", err
		}
		if err := r.Seal(prev); err != nil {
			return "", err
		}
		if key != nil {
			if err := r.Sign(key); err != nil {
				return "", err
			}
		}
		data, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return "", err
		}
		p := filepath.Join(s.Dir, fmt.Sprintf("%06d.json", r.Seq))
		err = writeExclusive(p, append(data, '\n'), 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		md := filepath.Join(s.Dir, fmt.Sprintf("%06d.md", r.Seq))
		if err := writeExclusive(md, []byte(Markdown(r)), 0o600); err != nil {
			return p, fmt.Errorf("report saved, Markdown copy not written: %w", err)
		}
		return p, nil
	}
	return "", errors.New("could not claim a report number; another drill keeps writing at the same time")
}

// Entry is one stored report and the file it came from.
type Entry struct {
	Path   string
	Seq    int
	Report *Report
	Err    error // set when the file could not be read or parsed
}

// List returns every report in sequence order. Unreadable files are returned with Err
// set rather than skipped, so verification can report them.
func (s Store) List() ([]Entry, error) {
	des, err := os.ReadDir(s.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		m := reportName.FindStringSubmatch(de.Name())
		if m == nil {
			continue
		}
		seq, _ := strconv.Atoi(m[1])
		e := Entry{Path: filepath.Join(s.Dir, de.Name()), Seq: seq}
		e.Report, e.Err = load(e.Path)
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// Latest returns the newest readable report, or nil when there are none. A newest
// file that cannot be read is an error: chaining past it would hide the damage.
func (s Store) Latest() (*Report, error) {
	all, err := s.List()
	if err != nil || len(all) == 0 {
		return nil, err
	}
	last := all[len(all)-1]
	if last.Err != nil {
		return nil, fmt.Errorf("latest report %s is unreadable, so a new one cannot be chained to it: %w", last.Path, last.Err)
	}
	return last.Report, nil
}

func load(path string) (*Report, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxReportFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxReportFile {
		return nil, errors.New("file too large")
	}
	var r Report
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("not valid report JSON: %w", err)
	}
	return &r, nil
}

// Issue is one problem found while verifying the chain.
type Issue struct {
	Seq     int    `json:"seq"`
	Path    string `json:"path"`
	Problem string `json:"problem"`
}

// VerifyChain checks that every report's hash matches its contents, that each one names
// its predecessor's hash, that numbers run without gaps from 1, and that signatures are
// valid. With pub set, every report must be signed by that key. With head set, the newest
// report's hash must start with it, which catches the newest reports being deleted.
func VerifyChain(entries []Entry, pub ed25519.PublicKey, head string) []Issue {
	var issues []Issue
	add := func(e Entry, format string, a ...any) {
		issues = append(issues, Issue{e.Seq, e.Path, fmt.Sprintf(format, a...)})
	}
	prevHash := Genesis
	for i, e := range entries {
		if e.Seq != i+1 {
			add(e, "expected report #%d here; reports are missing or renumbered", i+1)
		}
		if e.Err != nil {
			add(e, "cannot be read: %v", e.Err)
			prevHash = ""
			continue
		}
		r := e.Report
		if r.Seq != e.Seq {
			add(e, "file name says #%d but the report says #%d", e.Seq, r.Seq)
		}
		got, err := r.ComputeHash()
		switch {
		case err != nil:
			add(e, "cannot hash: %v", err)
		case got != r.Hash:
			add(e, "contents changed after it was written (hash %s, recorded %s)", ShortHash(got), ShortHash(Clean(r.Hash)))
		}
		if prevHash != "" && r.Prev != prevHash {
			add(e, "does not follow the report before it (prev %s, expected %s)", ShortHash(Clean(r.Prev)), ShortHash(prevHash))
		}
		if err := r.CheckSignature(pub); err != nil {
			add(e, "signature: %v", err)
		}
		prevHash = r.Hash
	}
	if head != "" {
		if len(entries) == 0 {
			issues = append(issues, Issue{Problem: "no reports, but a head hash was given"})
		} else if last := entries[len(entries)-1]; last.Report == nil || len(head) < 8 || len(last.Report.Hash) < len(head) || last.Report.Hash[:len(head)] != head {
			issues = append(issues, Issue{last.Seq, last.Path, "newest report does not match the recorded head hash; later reports may have been deleted"})
		}
	}
	return issues
}
