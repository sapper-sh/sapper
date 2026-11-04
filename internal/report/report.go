// Package report builds, renders, chains and signs breach drill evidence reports.
//
// A report says what was tested, what was found and what to do about it, in that
// order of importance: verdict first, then the evidence behind it. Every report
// carries the hash of the one before it, so editing or removing an old report breaks
// the chain, and can be signed with an ed25519 key held outside the state directory.
package report

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/sapper-unbuilt/sapper/internal/manifest"
)

// Schema is the report format version.
const Schema = 1

// Verdicts.
const (
	Survived  = "SURVIVED"
	Breached  = "BREACHED"
	NoVerdict = "NO VERDICT"
)

// Genesis is the Prev value of the first report in a chain.
const Genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// MaxFindings bounds how many findings and changed live paths a report stores. The
// totals are always kept, so a huge breach still reports its true size.
const MaxFindings = 1000

// Phases of a drill, in order.
const (
	PhaseLocate    = "find snapshot"
	PhaseManifest  = "load manifest"
	PhaseIsolation = "isolation"
	PhaseRestore   = "restore"
	PhaseVerify    = "verify"
	PhaseLiveCheck = "live check"
)

// Report is the evidence a breach drill leaves behind.
type Report struct {
	Schema   int    `json:"schema"`
	Seq      int    `json:"seq"`
	Fortress string `json:"fortress"`
	Verdict  string `json:"verdict"`
	Headline string `json:"headline"`
	Why      string `json:"why"`

	Snapshot Snapshot  `json:"snapshot"`
	Started  time.Time `json:"started"`
	TookMS   int64     `json:"took_ms"`
	Volumes  []Volume  `json:"volumes"`
	Repo     string    `json:"repository"`

	Evidence  Evidence  `json:"evidence"`
	Isolation Isolation `json:"isolation"`
	Phases    []Phase   `json:"phases"`
	Recovery  Recovery  `json:"recovery"`
	NextSteps []string  `json:"next_steps"`
	Error     string    `json:"error,omitempty"`

	Prev      string     `json:"prev"`
	Hash      string     `json:"hash"`
	Signature *Signature `json:"signature,omitempty"`
}

// Snapshot identifies the restic snapshot that was drilled.
type Snapshot struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time,omitzero"`
}

// Short returns the first 8 characters of the snapshot ID, as restic prints it.
func (s Snapshot) Short() string { return shortHex(s.ID, 8) }

// Volume is one backed-up host directory.
type Volume struct {
	App   string `json:"app"`
	Mount string `json:"mount"`
	Path  string `json:"path"`
}

// Evidence is what verification found.
type Evidence struct {
	Entries       int                `json:"entries"`
	Matched       int                `json:"matched"`
	Files         int                `json:"files"`
	Dirs          int                `json:"dirs"`
	Symlinks      int                `json:"symlinks"`
	Bytes         int64              `json:"bytes"`
	MerkleRoot    string             `json:"merkle_root,omitempty"`
	Findings      []manifest.Problem `json:"findings"`
	FindingsTotal int                `json:"findings_total"`
	ByKind        map[string]int     `json:"by_kind,omitempty"`
}

// Isolation records the checks that keep a drill away from live data.
type Isolation struct {
	RestoreOutsideLive bool     `json:"restore_outside_live"`
	LiveChecked        bool     `json:"live_checked"`
	LiveUntouched      bool     `json:"live_untouched"`
	LiveChanged        []string `json:"live_changed,omitempty"`
	LiveChangedTotal   int      `json:"live_changed_total,omitempty"`
	KeptCopy           string   `json:"kept_copy,omitempty"`
}

// Phase is how long one step of the drill took.
type Phase struct {
	Name string `json:"name"`
	MS   int64  `json:"ms"`
}

// Objective is a measured recovery figure and its optional target.
type Objective struct {
	MeasuredMS int64 `json:"measured_ms"`
	TargetMS   int64 `json:"target_ms,omitempty"`
	Met        *bool `json:"met,omitempty"`
	Known      bool  `json:"known"`
}

// Recovery holds the measured restore time (RTO) and backup age (RPO).
type Recovery struct {
	RTO Objective `json:"rto"`
	RPO Objective `json:"rpo"`
}

// Signature is an ed25519 signature over the report hash.
type Signature struct {
	Alg       string `json:"alg"`
	KeyID     string `json:"key_id"`
	PublicKey string `json:"public_key"`
	Value     string `json:"value"`
}

// Input is everything a drill measured. Build turns it into a Report.
type Input struct {
	Fortress   string
	Repo       string // already redacted
	Volumes    []Volume
	SnapshotID string
	SnapshotAt string // as restic printed it, RFC 3339
	Started    time.Time
	Took       time.Duration
	Phases     []Phase
	Result     *manifest.Result // nil when the drill stopped before verifying
	MerkleRoot string
	Isolated   bool
	LiveCheck  bool
	LiveDiff   []string
	KeptCopy   string
	TargetRTO  time.Duration
	TargetRPO  time.Duration
	Err        error  // why the drill stopped early, if it did
	ErrPhase   string // phase that failed
	NoSnapshot bool   // the repository has no sapper snapshot yet
}

// Build assembles a report from what a drill measured.
func Build(in Input) *Report {
	r := &Report{
		Schema:   Schema,
		Fortress: in.Fortress,
		Repo:     in.Repo,
		Volumes:  in.Volumes,
		Snapshot: Snapshot{ID: in.SnapshotID},
		Started:  in.Started.UTC().Truncate(time.Millisecond),
		TookMS:   in.Took.Milliseconds(),
		Phases:   in.Phases,
		Evidence: Evidence{MerkleRoot: in.MerkleRoot, Findings: []manifest.Problem{}},
		Isolation: Isolation{
			RestoreOutsideLive: in.Isolated,
			LiveChecked:        in.LiveCheck,
			LiveUntouched:      in.LiveCheck && len(in.LiveDiff) == 0,
			LiveChangedTotal:   len(in.LiveDiff),
			KeptCopy:           in.KeptCopy,
		},
	}
	if r.Phases == nil {
		r.Phases = []Phase{}
	}
	if t, err := time.Parse(time.RFC3339Nano, in.SnapshotAt); err == nil {
		r.Snapshot.Time = t.UTC()
	}
	if len(in.LiveDiff) > 0 {
		r.Isolation.LiveChanged = capList(in.LiveDiff, MaxFindings)
	}
	if res := in.Result; res != nil {
		e := &r.Evidence
		e.Entries, e.Matched, e.Files, e.Dirs, e.Symlinks, e.Bytes = res.Checked, res.Matched, res.Files, res.Dirs, res.Symlinks, res.Bytes
		e.FindingsTotal = len(res.Problems)
		if len(res.Problems) > 0 {
			e.ByKind = map[string]int{}
			for _, p := range res.Problems {
				e.ByKind[kindOf(p)]++
			}
			// Most serious first, so the headline and the first lines name what matters.
			sorted := append([]manifest.Problem(nil), res.Problems...)
			sort.SliceStable(sorted, func(i, j int) bool { return kindRank(sorted[i]) < kindRank(sorted[j]) })
			e.Findings = sorted[:min(len(sorted), MaxFindings)]
		}
	}
	r.Recovery.RTO = Objective{MeasuredMS: restoreTime(in.Phases).Milliseconds(), Known: in.Err == nil}
	if !r.Snapshot.Time.IsZero() {
		age := max(in.Started.Sub(r.Snapshot.Time), 0)
		r.Recovery.RPO = Objective{MeasuredMS: age.Milliseconds(), Known: true}
	}
	setTarget(&r.Recovery.RTO, in.TargetRTO)
	setTarget(&r.Recovery.RPO, in.TargetRPO)

	switch {
	case in.Err != nil || in.Result == nil:
		r.Verdict = NoVerdict
		if in.Err != nil {
			r.Error = CleanLines(in.Err.Error())
		} else {
			r.Error = "verification did not run"
		}
	case len(in.Result.Problems) > 0:
		r.Verdict = Breached
	default:
		r.Verdict = Survived
	}
	r.Headline, r.Why = headline(r, in)
	r.NextSteps = nextSteps(r, in)
	return r
}

func setTarget(o *Objective, target time.Duration) {
	if target <= 0 {
		return
	}
	o.TargetMS = target.Milliseconds()
	if o.Known {
		met := o.MeasuredMS <= o.TargetMS
		o.Met = &met
	}
}

// restoreTime is the measured recovery time: restoring plus verifying, because a restore
// is not usable until it is known to be correct.
func restoreTime(ph []Phase) time.Duration {
	var ms int64
	for _, p := range ph {
		if p.Name == PhaseRestore || p.Name == PhaseVerify {
			ms += p.MS
		}
	}
	return time.Duration(ms) * time.Millisecond
}

func kindOf(p manifest.Problem) string {
	if p.Kind != "" {
		return p.Kind
	}
	return "other"
}

func capList(s []string, n int) []string {
	if len(s) > n {
		s = s[:n]
	}
	return append([]string(nil), s...)
}

// Exit tells callers which exit code the verdict maps to: 0 survived, 3 breached, 4 no verdict.
func (r *Report) Exit() int {
	switch r.Verdict {
	case Survived:
		return 0
	case Breached:
		return 3
	}
	return 4
}

// TargetsMissed lists the recovery objectives the drill did not meet.
func (r *Report) TargetsMissed() []string {
	var out []string
	if m := r.Recovery.RTO.Met; m != nil && !*m {
		out = append(out, "RTO")
	}
	if m := r.Recovery.RPO.Met; m != nil && !*m {
		out = append(out, "RPO")
	}
	return out
}

// hashPrefix separates report hashes from any other sha256 use.
const hashPrefix = "sapper-report-v1\n"

// ComputeHash returns the hex sha256 of the report with Hash and Signature cleared.
// encoding/json writes struct fields in declaration order and sorts map keys, so the
// encoding is deterministic for a given report.
func (r *Report) ComputeHash() (string, error) {
	c := *r
	c.Hash, c.Signature = "", nil
	data, err := json.Marshal(&c)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte(hashPrefix), data...))
	return hex.EncodeToString(sum[:]), nil
}

// Seal sets Prev and Seq from the previous report (nil for the first) and computes Hash.
func (r *Report) Seal(prev *Report) error {
	r.Prev, r.Seq = Genesis, 1
	if prev != nil {
		if prev.Hash == "" {
			return errors.New("previous report has no hash")
		}
		r.Prev, r.Seq = prev.Hash, prev.Seq+1
	}
	h, err := r.ComputeHash()
	if err != nil {
		return err
	}
	r.Hash = h
	return nil
}

func shortHex(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// KindOrder is the order findings are grouped in, most serious first.
var KindOrder = []string{
	manifest.ProblemUnsafe, manifest.ProblemContent, manifest.ProblemSize, manifest.ProblemMissing,
	manifest.ProblemUnexpected, manifest.ProblemType, manifest.ProblemSymlink, manifest.ProblemMode, "other",
}

func kindRank(p manifest.Problem) int {
	for i, k := range KindOrder {
		if k == kindOf(p) {
			return i
		}
	}
	return len(KindOrder)
}

// kindsPresent returns the finding kinds in r in KindOrder.
func kindsPresent(r *Report) []string {
	var out []string
	for _, k := range KindOrder {
		if r.Evidence.ByKind[k] > 0 {
			out = append(out, k)
		}
	}
	// Unknown kinds from a newer sapper still show up, after the known ones.
	var extra []string
	for k := range r.Evidence.ByKind {
		known := false
		for _, o := range KindOrder {
			known = known || o == k
		}
		if !known {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	return append(out, extra...)
}
