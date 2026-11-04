package report

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sapper-unbuilt/sapper/internal/art"
)

// MaxWidth is the widest the terminal report gets, even on a wide terminal: long lines
// are harder to read, and 80 columns survives being pasted into a ticket or chat.
const MaxWidth = 80

// shownFindings is how many findings the terminal and Markdown views list. The JSON
// report holds up to MaxFindings.
const shownFindings = 10

// TimeFormat is how report times are shown to people.
const TimeFormat = "2006-01-02 15:04:05 MST"

// View controls terminal rendering.
type View struct {
	Color bool
	Width int            // terminal columns; 0 means unknown
	Loc   *time.Location // zone for displayed times; nil means local
	Path  string         // where the report was saved, if it was
	Plain bool           // no art frame: verdict as plain lines, for grep and screen readers
}

func (v View) width() int {
	if v.Width <= 0 || v.Width > MaxWidth {
		return MaxWidth
	}
	return max(v.Width, 40)
}

func (v View) at(t time.Time) string {
	loc := v.Loc
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format(TimeFormat)
}

var spaced = map[string]string{NoVerdict: "N O   V E R D I C T"}

// Terminal renders the report for a person at a terminal: the verdict box first,
// then what was tested, the evidence, timings, recovery objectives, next steps and
// the integrity record.
func Terminal(r *Report, v View) string {
	r = r.forDisplay()
	w := v.width()
	var b strings.Builder
	box := art.Verdict{
		Survived: r.Verdict == Survived,
		Title:    "BREACH DRILL // " + r.Fortress,
		Word:     spaced[r.Verdict],
		Neutral:  r.Verdict == NoVerdict,
	}
	if v.Plain {
		fmt.Fprintf(&b, "%s  breach drill, %s\n", r.Verdict, r.Fortress)
		for _, l := range append(wrap(r.Headline, w), wrap(r.Why, w)...) {
			b.WriteString(l + "\n")
		}
	} else {
		inner := min(w, art.FrameWidth) - 4
		box.Lines = append(box.Lines, wrap(r.Headline, inner)...)
		box.Lines = append(box.Lines, wrap(r.Why, inner)...)
		b.WriteString(box.Render(v.Color))
	}

	s := sectioner{b: &b, w: w}
	s.head("What was tested")
	if r.Snapshot.ID != "" {
		snap := r.Snapshot.Short()
		if !r.Snapshot.Time.IsZero() {
			snap += ", taken " + v.at(r.Snapshot.Time)
		}
		s.kv("Snapshot", snap)
		s.kv("Full ID", r.Snapshot.ID)
	} else {
		s.kv("Snapshot", "none found")
	}
	s.kv("Apps", appsLine(r.Volumes))
	s.kv("Drill", fmt.Sprintf("%s, took %s", v.at(r.Started), Dur(ms(r.TookMS))))

	if r.Verdict != NoVerdict {
		e := r.Evidence
		s.head("Evidence")
		s.kv("Checked", fmt.Sprintf("%s, %s of file data", count(e.Entries, "entry", "entries"), Bytes(e.Bytes)))
		s.kv("Made up of", fmt.Sprintf("%s, %s, %s", count(e.Files, "file", "files"), count(e.Dirs, "directory", "directories"), count(e.Symlinks, "symlink", "symlinks")))
		s.kv("Matched", fmt.Sprintf("%s of %s (type, size, sha256, link, mode)", Num(e.Matched), Num(e.Entries)))
		if e.FindingsTotal == 0 {
			s.kv("Problems", "none")
		} else {
			s.kv("Problems", kindSummary(r))
			for i, f := range e.Findings {
				if i == shownFindings {
					s.line(fmt.Sprintf("    … and %s more; the JSON report lists them all.", Num(e.FindingsTotal-shownFindings)))
					break
				}
				s.line("    " + Fit(Clean(f.Path), w-4))
				if f.Expected != "" || f.Actual != "" {
					s.line("      " + KindLabel(kindOf(f)) + ": expected " + Fit(shortHashes(Clean(f.Expected)), w-40))
					s.line(strings.Repeat(" ", 6+len(KindLabel(kindOf(f)))) + "  actual   " + Fit(shortHashes(Clean(f.Actual)), w-40))
				} else {
					s.line("      " + Fit(Clean(f.Reason), w-6))
				}
			}
		}
		if e.MerkleRoot != "" {
			s.kv("Merkle root", ShortHash(e.MerkleRoot)+" over the manifest")
		}
	}
	s.head("Isolation")
	s.kv("Restore dir", yesNo(r.Isolation.RestoreOutsideLive, "outside live data, the repository and sapper state", "not confirmed; nothing was restored"))
	switch {
	case !r.Isolation.LiveChecked:
		s.kv("Live data", "not checked")
	case r.Isolation.LiveUntouched:
		s.kv("Live data", "unchanged during the drill (path, size, mode, mtime)")
	default:
		s.kv("Live data", count(r.Isolation.LiveChangedTotal, "path", "paths")+" changed during the drill; first "+Fit(Clean(r.Isolation.LiveChanged[0]), w-40))
	}
	if r.Isolation.KeptCopy != "" {
		s.kv("Kept copy", Fit(Clean(r.Isolation.KeptCopy), w-labelWidth))
	}

	if len(r.Phases) > 0 {
		s.head("Timings")
		for _, p := range r.Phases {
			s.line(fmt.Sprintf("  %-16s%7s", capFirst(p.Name), Dur(ms(p.MS))))
		}
	}
	s.head("Recovery")
	s.kv("Restore time", objective(r.Recovery.RTO, "to restore and verify (RTO)"))
	s.kv("Backup age", objective(r.Recovery.RPO, "since the backup (RPO)"))

	if r.Error != "" {
		s.head("Error")
		for _, l := range wrap(r.Error, w-2) {
			s.line("  " + l)
		}
	}
	s.head("What to do next")
	for i, step := range r.NextSteps {
		lines := wrap(step, w-5)
		for j, l := range lines {
			if j == 0 {
				s.line(fmt.Sprintf("  %d. %s", i+1, l))
			} else {
				s.line("     " + l)
			}
		}
	}
	s.head("Record")
	if r.Hash != "" {
		s.kv("Report", fmt.Sprintf("#%d, hash %s", r.Seq, ShortHash(r.Hash)))
		if r.Prev == Genesis {
			s.kv("Chain", "first report for this fortress")
		} else {
			s.kv("Chain", fmt.Sprintf("follows #%d, hash %s", r.Seq-1, ShortHash(r.Prev)))
		}
	}
	if r.Signature != nil {
		s.kv("Signed", "ed25519, key "+r.Signature.KeyID)
	} else {
		s.kv("Signed", "no")
	}
	if v.Path != "" {
		if utf8.RuneCountInString(v.Path) <= w-labelWidth {
			s.kv("Saved", v.Path)
		} else {
			s.line("  Saved")
			s.line("    " + Fit(v.Path, w-4))
		}
	}
	return b.String()
}

// Brief is a one-line summary for logs, cron mail and status bars.
func Brief(r *Report) string {
	r = r.forDisplay()
	parts := []string{r.Verdict, r.Fortress}
	if r.Snapshot.ID != "" {
		parts = append(parts, "snapshot "+r.Snapshot.Short())
	}
	switch r.Verdict {
	case NoVerdict:
		parts = append(parts, r.Headline)
	default:
		parts = append(parts, fmt.Sprintf("%s/%s matched", Num(r.Evidence.Matched), Num(r.Evidence.Entries)))
		if r.Evidence.FindingsTotal > 0 {
			parts = append(parts, count(r.Evidence.FindingsTotal, "problem", "problems"))
		}
		parts = append(parts, "restore "+Dur(ms(r.Recovery.RTO.MeasuredMS)))
		if r.Recovery.RPO.Known {
			parts = append(parts, "age "+strings.ReplaceAll(Dur(ms(r.Recovery.RPO.MeasuredMS)), " ", ""))
		}
		if m := r.TargetsMissed(); len(m) > 0 {
			parts = append(parts, "missed "+strings.Join(m, "+"))
		}
	}
	if r.Seq > 0 {
		parts = append(parts, fmt.Sprintf("report #%d", r.Seq))
	}
	return strings.Join(parts, "  ")
}

// forDisplay returns a copy with every free-text field passed through Clean. Reports
// read back from disk are only as trustworthy as the state directory, and a hand-edited
// one must not be able to drive the terminal with escape sequences.
func (r *Report) forDisplay() *Report {
	c := *r
	c.Fortress, c.Verdict, c.Headline, c.Why, c.Error, c.Repo = Clean(c.Fortress), Clean(c.Verdict), Clean(c.Headline), Clean(c.Why), Clean(c.Error), Clean(c.Repo)
	c.Snapshot.ID, c.Hash, c.Prev = Clean(c.Snapshot.ID), Clean(c.Hash), Clean(c.Prev)
	c.NextSteps = cleanAll(c.NextSteps)
	c.Isolation.LiveChanged = cleanAll(c.Isolation.LiveChanged)
	c.Isolation.KeptCopy = Clean(c.Isolation.KeptCopy)
	c.Volumes = append([]Volume(nil), c.Volumes...)
	for i, v := range c.Volumes {
		c.Volumes[i] = Volume{Clean(v.App), Clean(v.Mount), Clean(v.Path)}
	}
	c.Phases = append([]Phase(nil), c.Phases...)
	for i := range c.Phases {
		c.Phases[i].Name = Clean(c.Phases[i].Name)
	}
	if c.Signature != nil {
		sig := *c.Signature
		sig.KeyID = Clean(sig.KeyID)
		c.Signature = &sig
	}
	c.Evidence.ByKind = map[string]int{}
	for k, n := range r.Evidence.ByKind {
		c.Evidence.ByKind[Clean(k)] = n
	}
	return &c
}

func cleanAll(s []string) []string {
	out := make([]string, len(s))
	for i, x := range s {
		out[i] = Clean(x)
	}
	return out
}

func objective(o Objective, what string) string {
	if !o.Known {
		if o.TargetMS > 0 {
			return "not measured; target " + Dur(ms(o.TargetMS))
		}
		return "not measured"
	}
	s := Dur(ms(o.MeasuredMS))
	if what != "" {
		s += " " + what
	}
	switch {
	case o.Met == nil:
		return s + "; no target set"
	case *o.Met:
		return s + "; target " + Dur(ms(o.TargetMS)) + ", met"
	}
	return s + "; target " + Dur(ms(o.TargetMS)) + ", missed"
}

func appsLine(vols []Volume) string {
	var order []string
	mounts := map[string][]string{}
	for _, v := range vols {
		if _, ok := mounts[v.App]; !ok {
			order = append(order, v.App)
		}
		mounts[v.App] = append(mounts[v.App], v.Mount)
	}
	var parts []string
	for _, a := range order {
		parts = append(parts, fmt.Sprintf("%s (%s)", a, strings.Join(mounts[a], ", ")))
	}
	if len(parts) == 0 {
		return "none"
	}
	return fmt.Sprintf("%s; %s", strings.Join(parts, ", "), count(len(vols), "volume", "volumes"))
}

func yesNo(ok bool, yes, no string) string {
	if ok {
		return yes
	}
	return no
}

var hexRun = regexp.MustCompile(`\b[0-9a-f]{64}\b`)

func shortHashes(s string) string {
	return hexRun.ReplaceAllStringFunc(s, ShortHash)
}

type sectioner struct {
	b *strings.Builder
	w int
}

func (s sectioner) head(t string) { fmt.Fprintf(s.b, "\n%s\n", t) }
func (s sectioner) line(l string) { s.b.WriteString(l + "\n") }

const labelWidth = 16

// kv writes "  Label        value", wrapping the value under itself.
func (s sectioner) kv(label, value string) {
	pad := "  " + label + strings.Repeat(" ", max(labelWidth-2-utf8.RuneCountInString(label), 1))
	lines := wrap(value, s.w-labelWidth)
	for i, l := range lines {
		if i == 0 {
			s.line(pad + l)
		} else {
			s.line(strings.Repeat(" ", labelWidth) + l)
		}
	}
}

// wrap breaks s into lines of at most w runes at spaces. Words longer than w are cut
// in the middle with Fit so nothing overflows.
func wrap(s string, w int) []string {
	var out []string
	var cur string
	for _, word := range strings.Fields(s) {
		if utf8.RuneCountInString(word) > w {
			word = Fit(word, w)
		}
		switch {
		case cur == "":
			cur = word
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
	}
	if cur != "" || len(out) == 0 {
		out = append(out, cur)
	}
	return out
}
