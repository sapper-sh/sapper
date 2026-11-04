package report

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sapper-unbuilt/sapper/internal/manifest"
)

// The wording here follows a few rules: verdict first, then the one fact that decides
// it; short declarative sentences; real numbers instead of adjectives; every problem
// paired with a command or a decision; no exclamation marks and no reassurance padding.

func headline(r *Report, in Input) (string, string) {
	e := r.Evidence
	snap := r.Snapshot.Short()
	switch r.Verdict {
	case Survived:
		why := fmt.Sprintf("All %s in snapshot %s came back identical to the checksums taken at backup time.", count(e.Entries, "entry", "entries"), snap)
		if missed := r.TargetsMissed(); len(missed) > 0 {
			return "The latest backup restores correctly, but it missed its " + strings.Join(missed, " and ") + " target.", why
		}
		return "The latest backup restores correctly.", why
	case Breached:
		first := e.Findings[0]
		verb := "differ"
		if e.FindingsTotal == 1 {
			verb = "differs"
		}
		why := fmt.Sprintf("%s of %s %s from what was backed up (%s). First: %s, %s.",
			Num(e.FindingsTotal), count(e.Entries, "entry", "entries"), verb, kindSummary(r), Clean(first.Path), Clean(first.Reason))
		if e.FindingsTotal > e.Entries {
			why = fmt.Sprintf("%s found across %s (%s). First: %s, %s.",
				count(e.FindingsTotal, "problem", "problems"), count(e.Entries, "entry", "entries"), kindSummary(r), Clean(first.Path), Clean(first.Reason))
		}
		special := e.ByKind[manifest.ProblemOlder] + e.ByKind[manifest.ProblemManifest]
		var entryFinding *manifest.Problem
		for i := range e.Findings {
			if k := e.Findings[i].Kind; k != manifest.ProblemOlder && k != manifest.ProblemManifest {
				entryFinding = &e.Findings[i]
				break
			}
		}
		rest := ""
		if n := e.FindingsTotal - special; n > 0 && entryFinding != nil {
			rest = fmt.Sprintf(" %s of %s %s from the manifest. First: %s, %s.", Num(n), count(e.Entries, "entry", "entries"),
				map[bool]string{true: "differs", false: "differ"}[n == 1], Clean(entryFinding.Path), Clean(entryFinding.Reason))
		}
		switch {
		case e.ByKind[manifest.ProblemManifest] > 0:
			return "The checksum manifest on this machine was changed after the backup.",
				fmt.Sprintf("It differs from the copy stored in the repository when snapshot %s was taken.%s", snap, rest)
		case special == e.FindingsTotal:
			n := e.ByKind[manifest.ProblemOlder]
			return "The latest backup restores correctly, but an older snapshot does not.",
				fmt.Sprintf("Snapshot %s matched all %s. %s of the sampled older snapshots %s. First: %s, %s.", snap, count(e.Entries, "entry", "entries"),
					Num(n), map[bool]string{true: "fails", false: "fail"}[n == 1], Clean(first.Path), Clean(first.Actual))
		case special > 0:
			why = strings.TrimSpace(rest) + fmt.Sprintf(" %s also failed.", count(e.ByKind[manifest.ProblemOlder], "older snapshot", "older snapshots"))
		}
		return "The latest backup does not match the data it was taken from.", why
	}
	switch {
	case in.NoSnapshot:
		return "There is no backup to test yet.", "The repository does not exist yet or has no snapshot tagged by sapper."
	case in.ErrPhase == PhaseManifest:
		return "This backup cannot be checked from here.", fmt.Sprintf("The checksum manifest for snapshot %s is not on this machine.", snap)
	case in.ErrPhase == PhaseIsolation:
		return "The drill stopped before restoring anything.", "The restore directory would have overlapped live data, the repository or sapper's state."
	case in.ErrPhase == PhaseRestore:
		return "The latest backup could not be restored.", fmt.Sprintf("restic failed while restoring snapshot %s.", snap)
	}
	return "The drill stopped before it could reach a verdict.", "It failed during the " + orDefault(in.ErrPhase, "drill") + " step."
}

func kindSummary(r *Report) string {
	var parts []string
	for _, k := range kindsPresent(r) {
		parts = append(parts, fmt.Sprintf("%d %s", r.Evidence.ByKind[k], KindLabel(k)))
	}
	return strings.Join(parts, ", ")
}

// KindLabel is the short human name for a finding kind.
func KindLabel(k string) string {
	switch k {
	case manifest.ProblemContent:
		return "changed content"
	case manifest.ProblemSize:
		return "changed size"
	case manifest.ProblemMissing:
		return "missing"
	case manifest.ProblemUnexpected:
		return "unexpected"
	case manifest.ProblemType:
		return "changed type"
	case manifest.ProblemSymlink:
		return "wrong link target"
	case manifest.ProblemMode:
		return "wrong permissions"
	case manifest.ProblemUnsafe:
		return "unsafe path"
	case manifest.ProblemManifest:
		return "manifest disagrees with repository copy"
	case manifest.ProblemOlder:
		return "older snapshot failed"
	}
	return k
}

func nextSteps(r *Report, in Input) []string {
	var s []string
	snap := r.Snapshot.Short()
	repo := r.Repo
	if repo == "" {
		repo = "<repository>"
	}
	if r.Verdict == NoVerdict {
		switch {
		case in.NoSnapshot:
			s = append(s, "Run `sapper backup`, then `sapper breach` to test it.")
		case in.ErrPhase == PhaseManifest:
			s = append(s,
				"Run the drill on the machine that took the backup; manifests are kept only in its local state directory.",
				"Or take a new backup here with `sapper backup` and drill that one.")
		case in.ErrPhase == PhaseIsolation:
			s = append(s, "Pass `--temp-dir` with a directory outside every data volume, the repository and the state directory.")
		case in.ErrPhase == PhaseRestore || in.ErrPhase == PhaseLocate:
			s = append(s,
				fmt.Sprintf("Check that the repository is reachable and the password is right: `restic -r %s snapshots`.", repo),
				"Rerun with `-v` to see each restic command sapper runs.")
		default:
			s = append(s, "Rerun with `-v` to see each restic command sapper runs.")
		}
		return s
	}
	for _, k := range kindsPresent(r) {
		switch k {
		case manifest.ProblemContent, manifest.ProblemSize:
			s = append(s,
				fmt.Sprintf("Do not restore the changed files from snapshot %s. Keep the snapshot for investigation and do not `restic forget` it yet.", snap),
				fmt.Sprintf("Run `restic -r %s check --read-data` to find out whether the repository itself is damaged.", repo),
				"If that check is clean, the manifest may have been edited after the backup. Compare its modification time in the state directory with the snapshot time.")
		case manifest.ProblemMissing:
			s = append(s, fmt.Sprintf("Run `restic -r %s ls %s` to check whether the snapshot holds the missing entries.", repo, snap))
		case manifest.ProblemUnexpected:
			s = append(s, "Inspect the unexpected entries with `sapper breach --keep`. A snapshot should not gain files after it is taken.")
		case manifest.ProblemType, manifest.ProblemSymlink, manifest.ProblemMode:
			s = append(s, "Check that the repository's storage keeps permissions and symlinks (some network shares do not), then back up again.")
		case manifest.ProblemManifest:
			s = append(s, "The manifest in the state directory and the copy stored in the repository at backup time disagree. One of them was edited. Find out who can write the state directory before trusting either; a signed copy (`sapper backup --sign-key`) settles which one is genuine.")
		case manifest.ProblemOlder:
			s = append(s, fmt.Sprintf("An older snapshot did not restore cleanly. Run `restic -r %s check --read-data` and keep that snapshot until you know why; the latest one passing does not mean the repository is healthy.", repo))
		case manifest.ProblemUnsafe:
			s = append(s, "The restore held a path that leads outside the restore directory or through a symlink. sapper did not follow it. Treat the repository as untrusted until you know who wrote to it.")
		default:
			s = append(s, "Inspect the restored copy with `sapper breach --keep`.")
		}
	}
	if r.Verdict == Breached {
		s = append(s, "Take a fresh backup with `sapper backup` and drill it. Keep the damaged snapshot until you understand the cause.")
	}
	if r.Isolation.LiveChecked && !r.Isolation.LiveUntouched {
		s = append(s, fmt.Sprintf("%s in live data changed during the drill. sapper writes only to the restore directory, so a running app most likely made the change. If the apps were stopped, find out what did.",
			capFirst(count(r.Isolation.LiveChangedTotal, "path", "paths"))))
	}
	rto, rpo := r.Recovery.RTO, r.Recovery.RPO
	if rto.Met != nil && !*rto.Met {
		s = append(s, fmt.Sprintf("Restore and verify took %s against an RTO target of %s. Move the repository closer to this host or use faster storage. If the target was a guess, measure a few drills and set it from those.",
			Dur(ms(rto.MeasuredMS)), Dur(ms(rto.TargetMS))))
	}
	if rpo.Met != nil && !*rpo.Met {
		s = append(s, fmt.Sprintf("The latest backup was %s old against an RPO target of %s. Run `sapper backup` now and schedule backups at least every %s.",
			Dur(ms(rpo.MeasuredMS)), Dur(ms(rpo.TargetMS)), Dur(ms(rpo.TargetMS))))
	}
	if rto.TargetMS == 0 && rpo.TargetMS == 0 {
		s = append(s, "Set `targets: {rto: ..., rpo: ...}` in the blueprint so drills also check that recovery is fast and recent enough.")
	}
	if len(s) == 0 {
		s = append(s, "Nothing to fix.")
	}
	return s
}

func ms(n int64) time.Duration { return time.Duration(n) * time.Millisecond }

// Dur formats a duration the way a person would say it: 340ms, 4.2s, 3m 12s, 5h 3m, 9d 4h.
func Dur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d >= 48*time.Hour:
		d = d.Round(time.Hour)
	case d >= time.Hour:
		d = d.Round(time.Minute)
	case d >= 10*time.Second:
		d = d.Round(time.Second)
	}
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < 10*time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return twoUnits(int(d.Minutes()), "m", int(d.Seconds())%60, "s")
	case d < 48*time.Hour:
		return twoUnits(int(d.Hours()), "h", int(d.Minutes())%60, "m")
	}
	return twoUnits(int(d.Hours())/24, "d", int(d.Hours())%24, "h")
}

func twoUnits(a int, au string, b int, bu string) string {
	if b == 0 {
		return strconv.Itoa(a) + au
	}
	return fmt.Sprintf("%d%s %d%s", a, au, b, bu)
}

// Bytes formats n in binary units.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// Num formats n with thousands separators.
func Num(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		return "-" + s
	}
	return s
}

func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return Num(n) + " " + many
}

func capFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Clean makes untrusted text (file names from a restored snapshot, restic error output)
// safe to print. Control characters, bidirectional overrides and invalid UTF-8 would
// otherwise let a crafted file name rewrite the terminal or hide text, so any string
// containing them is shown Go-quoted with escapes instead.
func Clean(s string) string {
	for _, r := range s {
		if r == utf8.RuneError || (!unicode.IsGraphic(r) && r != ' ') {
			return strconv.QuoteToGraphic(s)
		}
	}
	if !utf8.ValidString(s) {
		return strconv.QuoteToGraphic(s)
	}
	return s
}

// CleanLines is Clean for multi-line text such as a tool's error output: each line is
// cleaned on its own and the lines are joined with " | ", so the result is one line.
func CleanLines(s string) string {
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(strings.TrimSuffix(l, "\r")); l != "" {
			out = append(out, Clean(l))
		}
	}
	return strings.Join(out, " | ")
}

// Fit shortens s to at most n runes by cutting the middle, keeping the start and the
// file name at the end, which are the parts people recognise.
func Fit(s string, n int) string {
	r := []rune(s)
	if len(r) <= n || n < 5 {
		return s
	}
	tail := (n - 1) * 2 / 3
	head := n - 1 - tail
	return string(r[:head]) + "…" + string(r[len(r)-tail:])
}

// ShortHash shortens a sha256 for display: first 12 hex characters.
func ShortHash(h string) string {
	if len(h) == 64 {
		return h[:12] + "…"
	}
	return h
}
