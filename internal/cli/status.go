package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/sapper-unbuilt/sapper/internal/art"
	"github.com/sapper-unbuilt/sapper/internal/ops"
	"github.com/sapper-unbuilt/sapper/internal/termx"
)

type statusView struct {
	Fortress   string            `json:"fortress"`
	Blueprint  string            `json:"blueprint"`
	Apps       map[string]int    `json:"apps"`
	Rendered   bool              `json:"rendered"`
	LastBackup *ops.BackupRecord `json:"last_backup,omitempty"`
	LastDrill  *drillSummary     `json:"last_drill,omitempty"`
}

type drillSummary struct {
	Verdict  string    `json:"verdict"`
	Snapshot string    `json:"snapshot"`
	Time     time.Time `json:"time"`
	Problems int       `json:"problems"`
}

func (a *app) statusCmd() *cobra.Command {
	var noAnim bool
	c := &cobra.Command{
		Use:   "status",
		Short: "Show the fortress, its last backup and last drill",
		Example: `  sapper status
  sapper status --json
  SAPPER_NO_ANIM=1 sapper status`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			bp, err := a.load()
			if err != nil {
				return err
			}
			v := statusView{Fortress: bp.Fortress, Blueprint: filepath.Join(bp.Dir, filepath.Base(a.blueprintPath)), Apps: map[string]int{}}
			for _, n := range bp.AppNames() {
				v.Apps[n] = bp.Apps[n].Port
			}
			if _, err := os.Stat(filepath.Join(bp.Dir, ".sapper", "docker-compose.yml")); err == nil {
				v.Rendered = true
			}
			if r, ok := ops.LastBackup(bp); ok {
				v.LastBackup = &r
			}
			if d, ok := ops.LastDrill(bp); ok {
				verdict := "SURVIVED"
				if !d.Survived() {
					verdict = "BREACHED"
				}
				v.LastDrill = &drillSummary{verdict, d.Snapshot, d.Time, len(d.Problems)}
			}
			if a.jsonOut {
				return a.printJSON(v)
			}
			a.banner(noAnim)
			a.printStatus(v)
			return nil
		},
	}
	c.Flags().BoolVar(&noAnim, "no-anim", false, "skip the boot animation")
	return c
}

// banner prints the boot art sized to the terminal, animated only on an interactive colour terminal.
func (a *app) banner(noAnim bool) {
	if a.plain || a.quiet {
		return
	}
	lines := art.Lines(art.Fit(a.style.Width, art.Compact))
	animate := a.style.Color && a.style.TTY && !noAnim && os.Getenv("SAPPER_NO_ANIM") == ""
	if !animate {
		fmt.Fprint(a.stdout, art.Render(lines, a.style.Color))
		return
	}
	skip := make(chan struct{})
	if f, ok := a.stdin.(*os.File); ok && termx.IsTTY(f) {
		go func() {
			buf := make([]byte, 1)
			if _, err := f.Read(buf); err == nil {
				close(skip)
			}
		}()
	}
	art.Animate(a.stdout, lines, 24, 30*time.Millisecond, skip)
}

func (a *app) printStatus(v statusView) {
	w := a.stdout
	fmt.Fprintf(w, "\nfortress   %s\nblueprint  %s\n", v.Fortress, v.Blueprint)
	for _, n := range sortedKeys(v.Apps) {
		fmt.Fprintf(w, "  app      %-12s 127.0.0.1:%d\n", n, v.Apps[n])
	}
	rendered := "no (run sapper build)"
	if v.Rendered {
		rendered = "yes"
	}
	fmt.Fprintf(w, "rendered   %s\n", rendered)
	if v.LastBackup != nil {
		fmt.Fprintf(w, "backup     %s  %s\n", short(v.LastBackup.Snapshot), v.LastBackup.Time.Format(time.RFC3339))
	} else {
		fmt.Fprintln(w, "backup     none")
	}
	if d := v.LastDrill; d != nil {
		verdict := d.Verdict
		if a.style.Color {
			c := art.Ice(1)
			if verdict == "BREACHED" {
				c = art.Accent
			}
			verdict = fmt.Sprintf("\x1b[38;2;%d;%d;%dm%s\x1b[0m", c.R, c.G, c.B, verdict)
		}
		fmt.Fprintf(w, "drill      %s  %s  %s\n", verdict, short(d.Snapshot), d.Time.Format(time.RFC3339))
	} else {
		fmt.Fprintln(w, "drill      never")
	}
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
