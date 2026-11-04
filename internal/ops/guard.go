package ops

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sapper-sh/sapper/internal/manifest"
	"github.com/sapper-sh/sapper/internal/report"
)

// ErrSuspicious is returned when live data looks damaged compared with the last backup:
// a volume is empty, has shrunk sharply, or most files changed into random-looking bytes.
// Backing that up would push good snapshots out of retention, so backup stops unless forced.
var ErrSuspicious = errors.New("live data looks damaged compared with the last backup")

// Anomaly is one reason live data looks wrong.
type Anomaly struct {
	Kind   string `json:"kind"` // empty-volume, shrank, mass-change
	Volume string `json:"volume,omitempty"`
	Detail string `json:"detail"`
}

// Thresholds for Anomalies. They are deliberately coarse: they exist to stop the backup
// that follows a ransomware run, a missing mount or an rm -rf, not to judge normal churn.
const (
	// minFiles is the number of files a volume or fortress needs before shrink and
	// mass-change checks apply; tiny trees change wholesale all the time.
	minFiles = 4
	// shrinkFraction: fewer than this share of the previous files or bytes is a shrink.
	shrinkFraction = 0.5
	// changedFraction: at least this share of previous files changed or vanished.
	changedFraction = 0.5
	// randomFraction: at least this share of the changed or new files look random.
	randomFraction = 0.5
	// highEntropy is the bits per byte above which a sample looks encrypted or compressed.
	highEntropy = 7.0
	// entropySample is how much of each file is read to estimate entropy.
	entropySample = 64 << 10
	// minEntropySize: smaller files say too little about their entropy.
	minEntropySize = 512
)

// Anomalies compares the tree about to be backed up (cur) with the manifest of the last
// backup (prev, may be nil) for each volume root.
func Anomalies(prev, cur *manifest.Manifest, roots []string) []Anomaly {
	var out []Anomaly
	for _, root := range roots {
		root = filepath.Clean(root)
		cf, cb := countUnder(cur, root)
		var pf int
		var pb int64
		if prev != nil {
			pf, pb = countUnder(prev, root)
		}
		switch {
		case entriesUnder(cur, root) == 0:
			d := "the directory is empty"
			if pf > 0 {
				d = fmt.Sprintf("the directory is empty; the last backup had %d files", pf)
			}
			out = append(out, Anomaly{"empty-volume", root, d + ". If it is a mount point, check that the disk is mounted"})
		case pf >= minFiles && float64(cf) < float64(pf)*shrinkFraction:
			out = append(out, Anomaly{"shrank", root, fmt.Sprintf("only %d of the %d files from the last backup are left", cf, pf)})
		case pb >= 1<<20 && float64(cb) < float64(pb)*shrinkFraction:
			out = append(out, Anomaly{"shrank", root, fmt.Sprintf("%s now, %s at the last backup", report.Bytes(cb), report.Bytes(pb))})
		}
	}
	if prev == nil {
		return out
	}
	var prevFiles, touched int
	var candidates []string
	for p, e := range prev.Entries {
		if e.Kind != manifest.KindFile {
			continue
		}
		prevFiles++
		c, ok := cur.Entries[p]
		switch {
		case !ok:
			touched++
		case c.SHA256 != e.SHA256:
			touched++
			candidates = append(candidates, p)
		}
	}
	for p, e := range cur.Entries {
		if _, ok := prev.Entries[p]; !ok && e.Kind == manifest.KindFile {
			candidates = append(candidates, p) // new files, such as renamed *.locked copies
		}
	}
	if prevFiles < minFiles || touched < minFiles || float64(touched) < float64(prevFiles)*changedFraction {
		return out
	}
	sort.Strings(candidates)
	var sampled, random int
	for _, p := range candidates {
		if cur.Entries[p].Size < minEntropySize {
			continue
		}
		h, err := fileEntropy(p)
		if err != nil {
			continue
		}
		sampled++
		if h >= highEntropy {
			random++
		}
	}
	if sampled > 0 && float64(random) >= float64(sampled)*randomFraction {
		out = append(out, Anomaly{Kind: "mass-change", Detail: fmt.Sprintf(
			"%d of %d files changed or vanished since the last backup, and %d of %d changed or new files look encrypted (high entropy). This is what ransomware leaves behind",
			touched, prevFiles, random, sampled)})
	}
	return out
}

func entriesUnder(m *manifest.Manifest, root string) int {
	n := 0
	for p := range m.Entries {
		if p != root && strings.HasPrefix(p, root+string(filepath.Separator)) {
			n++
		}
	}
	return n
}

func countUnder(m *manifest.Manifest, root string) (files int, bytes int64) {
	for p, e := range m.Entries {
		if e.Kind == manifest.KindFile && strings.HasPrefix(p, root+string(filepath.Separator)) {
			files++
			bytes += e.Size
		}
	}
	return files, bytes
}

// fileEntropy estimates Shannon entropy in bits per byte from the start of the file.
func fileEntropy(p string) (float64, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, entropySample))
	if err != nil {
		return 0, err
	}
	return entropy(buf), nil
}

func entropy(b []byte) float64 {
	if len(b) == 0 {
		return 0
	}
	var counts [256]int
	for _, c := range b {
		counts[c]++
	}
	var h float64
	n := float64(len(b))
	for _, c := range counts {
		if c > 0 {
			q := float64(c) / n
			h -= q * math.Log2(q)
		}
	}
	return h
}

func describe(as []Anomaly) string {
	var parts []string
	for _, a := range as {
		if a.Volume != "" {
			parts = append(parts, a.Volume+": "+a.Detail)
		} else {
			parts = append(parts, a.Detail)
		}
	}
	return strings.Join(parts, "; ")
}
