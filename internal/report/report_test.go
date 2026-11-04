package report

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sapper-unbuilt/sapper/internal/art"
	"github.com/sapper-unbuilt/sapper/internal/manifest"
)

const snapID = "3f9a1c2e7b5d4c0a9e8f1d2c3b4a5f6e7d8c9b0a1f2e3d4c5b6a7f8e9d0c1b2a"

var t0 = time.Date(2026, 11, 4, 9, 30, 0, 0, time.UTC)

func input() Input {
	return Input{
		Fortress:   "my-fortress",
		Repo:       "/srv/backups/restic",
		Volumes:    []Volume{{"filebrowser", "database", "/srv/data/filebrowser/database"}, {"filebrowser", "files", "/srv/data/filebrowser/files"}, {"vaultwarden", "data", "/srv/data/vaultwarden"}},
		SnapshotID: snapID,
		SnapshotAt: "2026-11-04T03:30:00.123456789Z",
		Started:    t0,
		Took:       2400 * time.Millisecond,
		Phases:     []Phase{{PhaseLocate, 180}, {PhaseManifest, 3}, {PhaseIsolation, 1}, {PhaseRestore, 1900}, {PhaseVerify, 280}, {PhaseLiveCheck, 20}},
		Result:     &manifest.Result{Checked: 1204, Matched: 1204, Files: 1100, Dirs: 100, Symlinks: 4, Bytes: 52 << 20},
		MerkleRoot: strings.Repeat("ab", 32),
		Isolated:   true,
		LiveCheck:  true,
	}
}

func breachedInput() Input {
	in := input()
	in.Result = &manifest.Result{Checked: 1204, Matched: 1201, Files: 1100, Dirs: 100, Symlinks: 4, Bytes: 50 << 20, Problems: []manifest.Problem{
		{Path: "/srv/data/vaultwarden/db.sqlite3", Kind: manifest.ProblemContent, Reason: "checksum mismatch", Expected: strings.Repeat("1", 64), Actual: strings.Repeat("2", 64)},
		{Path: "/srv/data/vaultwarden/attachments/a1", Kind: manifest.ProblemMissing, Reason: "missing", Expected: "file", Actual: "absent"},
		{Path: "/srv/data/vaultwarden/rsa_key.pem", Kind: manifest.ProblemMode, Reason: "mode -rw-r--r--, expected -rw-------", Expected: "-rw-------", Actual: "-rw-r--r--"},
	}}
	return in
}

func TestBuildSurvived(t *testing.T) {
	r := Build(input())
	if r.Verdict != Survived || r.Exit() != 0 {
		t.Fatalf("verdict %s", r.Verdict)
	}
	if r.Headline != "The latest backup restores correctly." {
		t.Errorf("headline %q", r.Headline)
	}
	if !strings.Contains(r.Why, "All 1,204 entries in snapshot 3f9a1c2e") {
		t.Errorf("why %q", r.Why)
	}
	if r.Recovery.RTO.MeasuredMS != 2180 || !r.Recovery.RTO.Known {
		t.Errorf("RTO %+v", r.Recovery.RTO)
	}
	if got := ms(r.Recovery.RPO.MeasuredMS); got.Round(time.Second) != 6*time.Hour {
		t.Errorf("RPO %v", got)
	}
	if r.Recovery.RTO.Met != nil {
		t.Error("met set without a target")
	}
	if !strings.Contains(strings.Join(r.NextSteps, " "), "targets:") {
		t.Errorf("no hint to set targets: %v", r.NextSteps)
	}
}

func TestBuildBreached(t *testing.T) {
	r := Build(breachedInput())
	if r.Verdict != Breached || r.Exit() != 3 {
		t.Fatalf("verdict %s", r.Verdict)
	}
	want := "3 of 1,204 entries differ from what was backed up (1 changed content, 1 missing, 1 wrong permissions). First: /srv/data/vaultwarden/db.sqlite3, checksum mismatch."
	if r.Why != want {
		t.Errorf("why\n got %q\nwant %q", r.Why, want)
	}
	steps := strings.Join(r.NextSteps, "\n")
	for _, s := range []string{"check --read-data", "ls 3f9a1c2e", "keeps permissions", "sapper backup"} {
		if !strings.Contains(steps, s) {
			t.Errorf("next steps missing %q:\n%s", s, steps)
		}
	}
}

func TestBuildNoVerdict(t *testing.T) {
	cases := []struct {
		in       Input
		headline string
		step     string
	}{
		{Input{Fortress: "f", NoSnapshot: true, Err: errors.New("no snapshot"), ErrPhase: PhaseLocate}, "There is no backup to test yet.", "sapper backup"},
		{Input{Fortress: "f", SnapshotID: snapID, Err: errors.New("x"), ErrPhase: PhaseManifest}, "This backup cannot be checked from here.", "machine that took the backup"},
		{Input{Fortress: "f", SnapshotID: snapID, Err: errors.New("x"), ErrPhase: PhaseRestore}, "The latest backup could not be restored.", "restic -r"},
		{Input{Fortress: "f", SnapshotID: snapID, Err: errors.New("x"), ErrPhase: PhaseIsolation}, "The drill stopped before restoring anything.", "--temp-dir"},
	}
	for _, c := range cases {
		r := Build(c.in)
		if r.Verdict != NoVerdict || r.Exit() != 4 || r.Headline != c.headline || !strings.Contains(strings.Join(r.NextSteps, " "), c.step) {
			t.Errorf("%s: %+v", c.headline, r)
		}
		if r.Recovery.RTO.Known {
			t.Error("RTO marked known for a failed drill")
		}
	}
}

func TestTargets(t *testing.T) {
	in := input()
	in.TargetRTO, in.TargetRPO = time.Minute, 24*time.Hour
	r := Build(in)
	if r.Recovery.RTO.Met == nil || !*r.Recovery.RTO.Met || !*r.Recovery.RPO.Met || len(r.TargetsMissed()) != 0 {
		t.Fatalf("targets should be met: %+v", r.Recovery)
	}
	in.TargetRTO, in.TargetRPO = time.Second, time.Hour
	r = Build(in)
	if got := r.TargetsMissed(); len(got) != 2 {
		t.Fatalf("missed = %v", got)
	}
	if r.Verdict != Survived || !strings.Contains(r.Headline, "missed its RTO and RPO target") {
		t.Errorf("headline %q", r.Headline)
	}
	steps := strings.Join(r.NextSteps, "\n")
	if !strings.Contains(steps, "2.2s against an RTO target of 1.0s") || !strings.Contains(steps, "6h old against an RPO target of 1h") {
		t.Errorf("steps:\n%s", steps)
	}
}

func sealedChain(t *testing.T, n int, key ed25519.PrivateKey) Store {
	t.Helper()
	s := Store{Dir: filepath.Join(t.TempDir(), "reports")}
	for i := range n {
		in := input()
		in.Started = t0.Add(time.Duration(i) * time.Hour)
		if _, err := s.Append(Build(in), key); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func verify(t *testing.T, s Store, pub ed25519.PublicKey, head string) []Issue {
	t.Helper()
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	return VerifyChain(all, pub, head)
}

func TestChainVerifies(t *testing.T) {
	s := sealedChain(t, 3, nil)
	if issues := verify(t, s, nil, ""); len(issues) != 0 {
		t.Fatalf("clean chain: %+v", issues)
	}
	all, _ := s.List()
	if all[0].Report.Prev != Genesis || all[2].Report.Prev != all[1].Report.Hash || all[2].Seq != 3 {
		t.Fatal("chain not linked")
	}
	if issues := verify(t, s, nil, all[2].Report.Hash[:12]); len(issues) != 0 {
		t.Fatalf("head check: %+v", issues)
	}
	for _, e := range all {
		st, err := os.Stat(e.Path)
		if err != nil || st.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v %v", e.Path, st.Mode(), err)
		}
		if _, err := os.Stat(strings.TrimSuffix(e.Path, ".json") + ".md"); err != nil {
			t.Error(err)
		}
	}
}

func TestChainDetectsTampering(t *testing.T) {
	edit := func(path string, f func(m map[string]any)) {
		data, _ := os.ReadFile(path)
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		f(m)
		data, _ = json.Marshal(m)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name   string
		tamper func(s Store, all []Entry)
		want   string
	}{
		{"verdict flipped", func(_ Store, all []Entry) {
			edit(all[1].Path, func(m map[string]any) { m["verdict"] = "SURVIVED"; m["headline"] = "fine" })
		}, "contents changed"},
		{"rehashed after edit", func(_ Store, all []Entry) {
			r := all[1].Report
			r.Evidence.Matched = 1
			r.Hash, _ = r.ComputeHash()
			data, _ := json.Marshal(r)
			_ = os.WriteFile(all[1].Path, data, 0o600)
		}, "does not follow"},
		{"middle deleted", func(_ Store, all []Entry) { _ = os.Remove(all[1].Path) }, "missing or renumbered"},
		{"garbage", func(_ Store, all []Entry) { _ = os.WriteFile(all[0].Path, []byte("{"), 0o600) }, "cannot be read"},
		{"renamed", func(_ Store, all []Entry) {
			_ = os.Rename(all[2].Path, filepath.Join(filepath.Dir(all[2].Path), "000009.json"))
		}, "missing or renumbered"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := sealedChain(t, 3, nil)
			all, _ := s.List()
			tc.tamper(s, all)
			issues := verify(t, s, nil, "")
			var msgs []string
			for _, i := range issues {
				msgs = append(msgs, i.Problem)
			}
			if !strings.Contains(strings.Join(msgs, "\n"), tc.want) {
				t.Fatalf("issues %q do not mention %q", msgs, tc.want)
			}
		})
	}
	t.Run("newest deleted needs head", func(t *testing.T) {
		s := sealedChain(t, 3, nil)
		all, _ := s.List()
		head := all[2].Report.Hash[:16]
		_ = os.Remove(all[2].Path)
		if issues := verify(t, s, nil, ""); len(issues) != 0 {
			t.Fatalf("without a head the truncation is invisible, got %+v", issues)
		}
		if issues := verify(t, s, nil, head); len(issues) != 1 {
			t.Fatalf("head check: %+v", issues)
		}
	})
}

func TestAppendRefusesToChainPastUnreadableReport(t *testing.T) {
	s := sealedChain(t, 2, nil)
	all, _ := s.List()
	_ = os.WriteFile(all[1].Path, []byte("not json"), 0o600)
	if _, err := s.Append(Build(input()), nil); err == nil {
		t.Fatal("appended after a corrupt latest report")
	}
}

func TestSigning(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "drill.key")
	pub, pubPath, err := GenerateKey(keyPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(keyPath); st.Mode().Perm() != 0o600 {
		t.Errorf("private key mode %v", st.Mode().Perm())
	}
	if _, _, err := GenerateKey(keyPath, nil); err == nil {
		t.Fatal("overwrote an existing key")
	}
	priv, warn, err := LoadPrivateKey(keyPath)
	if err != nil || warn != "" {
		t.Fatal(err, warn)
	}
	loadedPub, err := LoadPublicKey(pubPath)
	if err != nil || !loadedPub.Equal(pub) {
		t.Fatal("public key round trip", err)
	}
	s := sealedChain(t, 2, priv)
	if issues := verify(t, s, pub, ""); len(issues) != 0 {
		t.Fatalf("signed chain: %+v", issues)
	}
	all, _ := s.List()
	raw, _ := os.ReadFile(all[0].Path)
	privPEM, _ := os.ReadFile(keyPath)
	if strings.Contains(string(raw), strings.TrimSpace(strings.Split(string(privPEM), "\n")[1])) {
		t.Fatal("private key material in report")
	}

	otherPub, _, _ := ed25519.GenerateKey(nil)
	if issues := verify(t, s, otherPub, ""); len(issues) != 2 || !strings.Contains(issues[0].Problem, "signed by key") {
		t.Fatalf("wrong key: %+v", issues)
	}
	// Re-signing a forged report with a new key passes only the embedded-key check;
	// pinning the public key catches it.
	forger, _, _ := ed25519.GenerateKey(nil)
	_ = forger
	_, fpriv, _ := ed25519.GenerateKey(nil)
	r := all[1].Report
	if err := r.Sign(fpriv); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(r)
	_ = os.WriteFile(all[1].Path, data, 0o600)
	if issues := verify(t, s, nil, ""); len(issues) != 0 {
		t.Fatalf("embedded key check: %+v", issues)
	}
	if issues := verify(t, s, pub, ""); len(issues) != 1 {
		t.Fatalf("pinned key check: %+v", issues)
	}
	r.Signature.Value = r.Signature.Value[:10] + "AAAA" + r.Signature.Value[14:]
	if err := r.CheckSignature(nil); err == nil {
		t.Fatal("corrupted signature accepted")
	}
	unsigned := sealedChain(t, 1, nil)
	if issues := verify(t, unsigned, pub, ""); len(issues) != 1 || !strings.Contains(issues[0].Problem, "not signed") {
		t.Fatalf("unsigned with pinned key: %+v", issues)
	}
}

func TestLoadPrivateKeyWarnsAndHidesContents(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k")
	if _, _, err := GenerateKey(p, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, warn, err := LoadPrivateKey(p); err != nil || !strings.Contains(warn, "chmod 600") {
		t.Fatalf("warn %q err %v", warn, err)
	}
	bad := filepath.Join(dir, "bad")
	_ = os.WriteFile(bad, []byte("SECRET-MATERIAL"), 0o600)
	if _, _, err := LoadPrivateKey(bad); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := LoadPrivateKey(p + ".pub"); err == nil {
		t.Fatal("public key accepted as private")
	}
}

func allRenders(t *testing.T, r *Report) map[string]string {
	t.Helper()
	out := map[string]string{"brief": Brief(r), "markdown": Markdown(r)}
	for _, w := range []int{0, 40, 60, 80, 200} {
		out["terminal-"+string(rune('0'+w/40))] = Terminal(r, View{Width: w, Loc: time.UTC, Path: "/home/someone/.local/state/sapper/fortresses/my-fortress-1a2b3c4d/reports/000007.json"})
	}
	return out
}

func TestTerminalFitsWidth(t *testing.T) {
	in := breachedInput()
	in.Result.Problems = append(in.Result.Problems, manifest.Problem{
		Path: "/srv/data/filebrowser/files/" + strings.Repeat("very-long-directory-name/", 8) + "report.pdf", Kind: manifest.ProblemSize, Reason: "size 1, expected 2",
		Expected: "2 bytes, sha256 " + strings.Repeat("a", 64), Actual: "1 bytes, sha256 " + strings.Repeat("b", 64)})
	for i := range 20 {
		in.Result.Problems = append(in.Result.Problems, manifest.Problem{Path: "/srv/data/memos/" + strings.Repeat("x", i), Kind: manifest.ProblemUnexpected, Reason: "unexpected in restore"})
	}
	for _, r := range []*Report{Build(input()), Build(in), Build(Input{Fortress: "f", NoSnapshot: true, Err: errors.New(strings.Repeat("restic said no ", 20)), ErrPhase: PhaseLocate})} {
		r.Seq, r.Hash, r.Prev = 7, strings.Repeat("c", 64), strings.Repeat("d", 64)
		for _, w := range []int{40, 60, 80, 0, 200} {
			out := Terminal(r, View{Width: w, Loc: time.UTC, Path: "/home/someone/.local/state/sapper/fortresses/my-fortress-1a2b3c4d/reports/000007.json"})
			limit := w
			if w == 0 || w > MaxWidth {
				limit = MaxWidth
			}
			limit = max(limit, art.FrameWidth)
			for _, l := range strings.Split(out, "\n") {
				if n := utf8.RuneCountInString(l); n > limit {
					t.Errorf("width %d: line of %d runes: %q", w, n, l)
				}
			}
		}
	}
}

func TestTerminalLayout(t *testing.T) {
	r := Build(breachedInput())
	r.Seq, r.Hash, r.Prev = 7, strings.Repeat("c", 64), strings.Repeat("d", 64)
	out := Terminal(r, View{Loc: time.UTC})
	verdict := strings.Index(out, "B R E A C H E D")
	if verdict < 0 || verdict > strings.Index(out, "What was tested") || strings.Index(out, "What was tested") > strings.Index(out, "Evidence") {
		t.Fatalf("verdict must come first:\n%s", out)
	}
	for _, want := range []string{"3f9a1c2e, taken 2026-11-04 03:30:00 UTC", snapID, "changed content: expected 111111111111…", "                 actual   222222222222…", "follows #6", "Restore (RTO) 2.2s to restore and verify; no target set"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("colour without Color")
	}
	if c := Terminal(r, View{Color: true}); !strings.Contains(c, "\x1b[38;2;") {
		t.Error("no colour with Color")
	}
}

func TestBriefIsOneLine(t *testing.T) {
	r := Build(breachedInput())
	r.Seq = 7
	b := Brief(r)
	if strings.Contains(b, "\n") || !strings.HasPrefix(b, "BREACHED  my-fortress  snapshot 3f9a1c2e  1,201/1,204 matched  3 problems") || !strings.HasSuffix(b, "report #7") {
		t.Fatalf("brief %q", b)
	}
}

// Untrusted file names must not reach the terminal as escape sequences or break out of Markdown code spans.
func TestHostileNamesAreNeutralised(t *testing.T) {
	in := breachedInput()
	in.Result.Problems[0].Path = "/srv/data/x\x1b]0;pwned\x07\x1b[2J\u202egnp.exe"
	in.Result.Problems[1].Path = "/srv/data/a` <script>alert(1)</script> `b\n# fake heading"
	r := Build(in)
	for name, out := range allRenders(t, r) {
		if strings.ContainsAny(out, "\x1b\x07\u202e") {
			t.Errorf("%s: raw control character in output", name)
		}
	}
	md := Markdown(r)
	if strings.Contains(md, "\n# fake heading") || strings.Contains(md, "<script>") && !strings.Contains(md, "``") {
		t.Errorf("markdown injection:\n%s", md)
	}
}

// The copy is part of the interface: keep it plain, calm and specific.
func TestCopyStyle(t *testing.T) {
	banned := []string{"!", "delve", "seamless", "robust", "crucial", "rest assured", "please note", "it is important", "unfortunately", "successfully", "🎉", "✅", "❌", "🚨", "simply", "just "}
	in := input()
	in.TargetRTO, in.TargetRPO = time.Second, time.Hour
	in.LiveDiff = []string{"/srv/data/memos/memos_prod.db"}
	reps := []*Report{Build(input()), Build(breachedInput()), Build(in),
		Build(Input{Fortress: "f", NoSnapshot: true, Err: errors.New("no snapshot"), ErrPhase: PhaseLocate})}
	for _, r := range reps {
		r.Seq, r.Hash, r.Prev = 1, strings.Repeat("c", 64), Genesis
		for name, out := range allRenders(t, r) {
			lower := strings.ToLower(out)
			for _, b := range banned {
				if strings.Contains(lower, b) {
					t.Errorf("%s %s: contains %q", r.Verdict, name, b)
				}
			}
		}
		for _, s := range append([]string{r.Headline, r.Why}, r.NextSteps...) {
			if !strings.HasSuffix(s, ".") {
				t.Errorf("sentence without a full stop: %q", s)
			}
			for _, sentence := range strings.Split(s, ". ") {
				if n := len(strings.Fields(sentence)); n > 32 {
					t.Errorf("sentence of %d words: %q", n, sentence)
				}
			}
		}
	}
}

func TestDur(t *testing.T) {
	tests := map[time.Duration]string{
		340 * time.Millisecond: "340ms", 4200 * time.Millisecond: "4.2s", 42 * time.Second: "42s",
		3*time.Minute + 12*time.Second: "3m 12s", 5 * time.Minute: "5m", 5*time.Hour + 3*time.Minute: "5h 3m",
		9*24*time.Hour + 4*time.Hour: "9d 4h", -time.Second: "0ms",
	}
	for in, want := range tests {
		if got := Dur(in); got != want {
			t.Errorf("Dur(%v) = %q, want %q", in, got, want)
		}
	}
	if Num(1234567) != "1,234,567" || Num(12) != "12" {
		t.Error("Num")
	}
}

func TestHashIsStableAcrossRoundTrip(t *testing.T) {
	r := Build(breachedInput())
	if err := r.Seal(nil); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(r)
	var back Report
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if h, _ := back.ComputeHash(); h != r.Hash {
		t.Fatal("hash changes after a JSON round trip")
	}
}

func TestFindingsAreOrderedBySeverity(t *testing.T) {
	in := breachedInput()
	p := in.Result.Problems
	in.Result.Problems = []manifest.Problem{p[2], p[1], p[0]}
	r := Build(in)
	if got := r.Evidence.Findings; got[0].Kind != manifest.ProblemContent || got[1].Kind != manifest.ProblemMissing || got[2].Kind != manifest.ProblemMode {
		t.Fatalf("order %+v", got)
	}
	if !strings.Contains(r.Why, "First: /srv/data/vaultwarden/db.sqlite3") {
		t.Errorf("why %q", r.Why)
	}
}

// A report edited on disk is rendered through Clean, so it cannot drive the terminal.
func TestTamperedReportFieldsAreCleaned(t *testing.T) {
	r := Build(input())
	r.Headline, r.Fortress, r.NextSteps = "fine\x1b[2J", "f\x1b]0;x\x07", []string{"run \x1b[31mthis"}
	r.Evidence.ByKind = map[string]int{"\x1b[1m": 1}
	for name, out := range allRenders(t, r) {
		if strings.ContainsAny(out, "\x1b\x07") {
			t.Errorf("%s: raw escape in output", name)
		}
	}
}
