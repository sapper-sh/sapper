package report

import (
	"fmt"
	"strings"
	"time"
)

// Markdown renders the report as a document a person can paste into a ticket, wiki
// or chat. It holds the same facts as the terminal view. File paths from the snapshot
// are untrusted, so they are cleaned and always placed in code spans.
func Markdown(r *Report) string {
	r = r.forDisplay()
	var b strings.Builder
	p := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	at := func(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }

	p("# Breach drill: %s %s\n\n", mdText(r.Fortress), mdText(r.Verdict))
	p("**%s** %s\n\n", mdText(r.Headline), mdText(r.Why))

	p("## What was tested\n\n")
	if r.Snapshot.ID != "" {
		p("- Snapshot: %s (full ID %s)", code(r.Snapshot.Short()), code(r.Snapshot.ID))
		if !r.Snapshot.Time.IsZero() {
			p(", taken %s", at(r.Snapshot.Time))
		}
		p("\n")
	} else {
		p("- Snapshot: none found\n")
	}
	p("- Repository: %s\n", code(r.Repo))
	p("- Drill started %s and took %s\n", at(r.Started), Dur(ms(r.TookMS)))
	for _, v := range r.Volumes {
		p("- %s / %s: %s\n", mdText(v.App), mdText(v.Mount), code(v.Path))
	}

	if r.Verdict != NoVerdict {
		e := r.Evidence
		p("\n## Evidence\n\n")
		p("- Checked %s: %s, %s, %s; %s of file data.\n", count(e.Entries, "entry", "entries"),
			count(e.Files, "file", "files"), count(e.Dirs, "directory", "directories"), count(e.Symlinks, "symlink", "symlinks"), Bytes(e.Bytes))
		p("- Matched %s of %s on type, size, sha256, link target and permissions.\n", Num(e.Matched), Num(e.Entries))
		if e.MerkleRoot != "" {
			p("- Merkle root over the manifest: %s\n", code(e.MerkleRoot))
		}
		if e.FindingsTotal == 0 {
			p("- Problems: none.\n")
		} else {
			p("- Problems: %s.\n\n", kindSummary(r))
			for i, f := range e.Findings {
				if i == shownFindings {
					p("\n%s more in the JSON report.\n", Num(e.FindingsTotal-shownFindings))
					break
				}
				p("%d. %s: %s", i+1, code(f.Path), mdText(Clean(f.Reason)))
				if f.Expected != "" || f.Actual != "" {
					p("  \n   expected %s  \n   actual %s", code(f.Expected), code(f.Actual))
				}
				p("\n")
			}
		}
	}

	p("\n## Isolation\n\n")
	p("- Restore directory: %s.\n", yesNo(r.Isolation.RestoreOutsideLive, "checked to be outside live data, the repository and sapper state", "not confirmed; nothing was restored"))
	switch {
	case !r.Isolation.LiveChecked:
		p("- Live data: not checked.\n")
	case r.Isolation.LiveUntouched:
		p("- Live data: unchanged during the drill (compared path, size, mode and mtime before and after).\n")
	default:
		p("- Live data: %s changed during the drill, first %s.\n", count(r.Isolation.LiveChangedTotal, "path", "paths"), code(r.Isolation.LiveChanged[0]))
	}

	if len(r.Phases) > 0 {
		p("\n## Timings\n\n| Phase | Time |\n|---|---|\n")
		for _, ph := range r.Phases {
			p("| %s | %s |\n", ph.Name, Dur(ms(ph.MS)))
		}
	}
	p("\n## Recovery objectives\n\n")
	p("- RTO (restore and verify): %s\n", objective(r.Recovery.RTO, ""))
	p("- RPO (backup age at drill time): %s\n", objective(r.Recovery.RPO, ""))

	if r.Error != "" {
		p("\n## Error\n\n%s\n", code(r.Error))
	}
	p("\n## What to do next\n\n")
	for i, s := range r.NextSteps {
		p("%d. %s\n", i+1, s)
	}
	p("\n## Record\n\n")
	p("- Report #%d, sha256 %s\n", r.Seq, code(r.Hash))
	p("- Previous report: %s\n", code(r.Prev))
	if r.Signature != nil {
		p("- Signed with ed25519 key %s\n", code(r.Signature.KeyID))
	} else {
		p("- Not signed\n")
	}
	p("\nCheck the chain with `sapper reports verify`. This file is a copy for people; the JSON next to it is what the chain hashes.\n")
	return b.String()
}

// code wraps untrusted text in a Markdown code span that it cannot break out of.
func code(s string) string {
	s = Clean(s)
	if s == "" {
		return "(none)"
	}
	fence := "`"
	for strings.Contains(s, fence) {
		fence += "`"
	}
	pad := ""
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		pad = " "
	}
	return fence + pad + s + pad + fence
}

// mdText escapes characters that Markdown would treat as markup in prose.
func mdText(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", "&lt;", ">", "&gt;", "|", `\|`)
	return r.Replace(s)
}
