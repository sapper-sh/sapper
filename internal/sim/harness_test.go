package sim

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/sapper-unbuilt/sapper/internal/blueprint"
	"github.com/sapper-unbuilt/sapper/internal/ops"
	"github.com/sapper-unbuilt/sapper/internal/paths"
	"github.com/sapper-unbuilt/sapper/internal/restic"
)

// Result of one scenario.
const (
	Caught  = "CAUGHT"
	Partial = "PARTIAL"
	Missed  = "MISSED"
	// Gap is a miss sapper knows about and documents; it does not fail the campaign.
	Gap = "GAP"
)

// outcome is one scoreboard row.
type outcome struct {
	ID, Name, Expected, Actual, Result string
}

var (
	mu      sync.Mutex
	results []outcome
)

func record(t *testing.T, o outcome) {
	t.Helper()
	mu.Lock()
	results = append(results, o)
	mu.Unlock()
	t.Logf("%s %s: %s", o.Result, o.Name, o.Actual)
	if o.Result == Missed && os.Getenv("SAPPER_SIM_ALLOW_MISS") == "" {
		t.Errorf("missed: expected %s, got %s", o.Expected, o.Actual)
	}
}

func TestMain(m *testing.M) {
	code := m.Run()
	if out := os.Getenv("SAPPER_SIM_OUT"); out != "" && len(results) > 0 {
		sort.Slice(results, func(i, j int) bool { return results[i].ID < results[j].ID })
		var b strings.Builder
		for _, o := range results {
			fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\n", o.ID, o.Result, o.Name, o.Expected, o.Actual)
		}
		if err := os.WriteFile(out, []byte(b.String()), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}
	os.Exit(code)
}

// env is one fortress with a real local restic repository.
type env struct {
	t    *testing.T
	bp   *blueprint.Blueprint
	c    *restic.Client
	repo string
	ctx  context.Context
}

const password = "sim-password"

func newEnv(t *testing.T) *env {
	t.Helper()
	if testing.Short() {
		t.Skip("simulation campaign skipped in -short mode")
	}
	if _, err := exec.LookPath("restic"); err != nil {
		t.Skip("restic not installed")
	}
	t.Setenv(paths.EnvStateDir, t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, blueprint.FileName), []byte(blueprint.Example), 0o600); err != nil {
		t.Fatal(err)
	}
	bp, err := blueprint.Load(filepath.Join(dir, blueprint.FileName))
	if err != nil {
		t.Fatal(err)
	}
	bp.Backup.Repository = filepath.Join(t.TempDir(), "repo")
	e := &env{t: t, bp: bp, repo: bp.RepoLocation(), ctx: context.Background()}
	e.c = e.client(password)
	for i, p := range bp.VolumePaths() {
		for j := 0; j < 10; j++ {
			e.write(filepath.Join(p, "docs", fmt.Sprintf("note-%d.txt", j)), strings.Repeat(fmt.Sprintf("volume %d note %d. ", i, j), 40))
		}
		e.write(filepath.Join(p, "app.db"), strings.Repeat("SQLite format 3\x00 rows ", 200))
	}
	return e
}

func (e *env) client(pw string) *restic.Client {
	c, err := restic.New(e.repo, pw)
	if err != nil {
		e.t.Fatal(err)
	}
	return c
}

func (e *env) write(p, s string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) backup() ops.BackupRecord {
	e.t.Helper()
	rec, err := ops.Backup(e.ctx, e.bp, e.c, ops.BackupOptions{})
	if err != nil {
		e.t.Fatalf("backup: %v", err)
	}
	return rec
}

func (e *env) breach(opt ops.DrillOptions) (ops.DrillReport, error) {
	return ops.Breach(e.ctx, e.bp, e.c, opt)
}

// verdict names what a drill concluded.
func verdict(rep ops.DrillReport) string {
	if rep.Report == nil {
		return "no report"
	}
	return rep.Report.Verdict
}

// repoFiles lists files under a repository subdirectory (data, index, snapshots, keys, locks).
func (e *env) repoFiles(sub string) []string {
	var out []string
	_ = filepath.WalkDir(filepath.Join(e.repo, sub), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func writable(t *testing.T, p string) {
	t.Helper()
	if err := os.Chmod(p, 0o600); err != nil {
		t.Fatal(err)
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 160 {
		s = s[:160] + "..."
	}
	return s
}
