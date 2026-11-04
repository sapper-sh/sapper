package cli

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sapper-unbuilt/sapper/internal/art"
	"github.com/sapper-unbuilt/sapper/internal/ops"
	"github.com/sapper-unbuilt/sapper/internal/termx"
)

func (a *app) taleCmd() *cobra.Command {
	var noAnim bool
	c := &cobra.Command{
		Use:    "tale",
		Short:  "Tell what sapper does as a story for children",
		Long:   "Tell what sapper does as a bedtime story, with pictures. If a blueprint and\na drill report exist, the story ends with what happened to your own treasure.",
		Hidden: true,
		Example: `  sapper tale
  sapper tale --plain
  sapper tale --no-anim`,
		Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if a.jsonOut {
				return usagef("tale is a story for humans and has no --json output")
			}
			a.tell(noAnim, time.Now())
			return nil
		},
	}
	c.Flags().BoolVar(&noAnim, "no-anim", false, "print the whole story at once")
	return c
}

// tell prints the story. Pictures are ASCII under --plain or a non-UTF-8 locale.
func (a *app) tell(noAnim bool, now time.Time) {
	w := a.stdout
	ascii := a.plain || !utf8Locale(os.Getenv)
	animate := a.style.Color && a.style.TTY && !noAnim && !a.plain && os.Getenv("SAPPER_NO_ANIM") == ""
	if animate && !ascii && a.fitsCutscene() {
		ending, panel := a.taleEnding(now)
		e := map[string]art.Ending{"survived": art.EndSurvived, "breached": art.EndBreached}[panel]
		a.runCutscene(art.NewCutscene(e, ending, uint64(now.UnixNano())))
		fmt.Fprintln(w, "\n  The Tale of sapper, the Backup Knight")
		for _, l := range wrapWords(ending, 74) {
			fmt.Fprintln(w, "  "+l)
		}
		fmt.Fprintln(w, "\n  The end.")
		return
	}
	skip := make(chan struct{})
	if animate {
		if f, ok := a.stdin.(*os.File); ok && termx.IsTTY(f) {
			go func() {
				buf := make([]byte, 1)
				if _, err := f.Read(buf); err == nil {
					close(skip)
				}
			}()
		}
	}
	rule := "  " + strings.Repeat("-", 40)
	if !ascii {
		rule = "  " + strings.Repeat("─", 40)
	}
	fmt.Fprintln(w, "\n  The Tale of sapper, the Backup Knight")
	fmt.Fprintln(w, rule)
	for i, ch := range art.Tale {
		lines := art.TaleArt(ch.Art, ascii)
		fmt.Fprintf(w, "\n  %s\n\n", ch.Title)
		if animate {
			art.Animate(w, lines, 18, 25*time.Millisecond, skip)
		} else {
			fmt.Fprint(w, art.Render(lines, a.style.Color))
		}
		fmt.Fprintln(w)
		for _, l := range ch.Text {
			if l == "" {
				fmt.Fprintln(w)
				continue
			}
			fmt.Fprintln(w, "  "+l)
		}
		if animate && i < len(art.Tale)-1 {
			select {
			case <-skip:
			case <-time.After(1500 * time.Millisecond):
			}
		}
	}
	ending, panel := a.taleEnding(now)
	fmt.Fprintln(w)
	fmt.Fprintln(w, rule)
	if panel != "" {
		lines := art.TaleArt(panel, ascii)
		fmt.Fprintln(w)
		if animate {
			art.Animate(w, lines, 18, 25*time.Millisecond, skip)
		} else {
			fmt.Fprint(w, art.Render(lines, a.style.Color))
		}
		fmt.Fprintln(w)
	}
	for _, l := range wrapWords(ending, 74) {
		fmt.Fprintln(w, "  "+l)
	}
	fmt.Fprintln(w, "\n  The end.")
}

// taleEnding is the last line and the picture above it, built only from the blueprint
// and the last drill in state. The picture is empty when no drill has run.
func (a *app) taleEnding(now time.Time) (string, string) {
	bp, err := a.load()
	if err != nil {
		return "No castle here yet. Run sapper init to draw the plans for one.", ""
	}
	d, ok := ops.LastDrill(bp)
	if !ok {
		return "No dragon has visited yet. Run sapper breach to send one.", ""
	}
	if d.Survived() {
		return fmt.Sprintf("Your treasure survived the dragon %s. %d of %d %s came back perfect.",
			ago(now.Sub(d.Time)), d.Checked, d.Checked, talePlural(d.Checked, "drawing", "drawings")), "survived"
	}
	n := len(d.Problems)
	return fmt.Sprintf("The dragon smudged %d %s. Run sapper reports show to see which.", n, talePlural(n, "drawing", "drawings")), "breached"
}

func talePlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func ago(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "a moment ago"
	case d < time.Hour:
		m := int(d.Minutes())
		return fmt.Sprintf("%d %s ago", m, talePlural(m, "minute", "minutes"))
	case d < 24*time.Hour:
		h := int(d.Hours())
		return fmt.Sprintf("%d %s ago", h, talePlural(h, "hour", "hours"))
	}
	n := int(d.Hours()) / 24
	return fmt.Sprintf("%d %s ago", n, talePlural(n, "day", "days"))
}

// utf8Locale reports whether the locale can show Unicode art. An unset locale counts as UTF-8.
func utf8Locale(getenv func(string) string) bool {
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(k); v != "" {
			v = strings.ToLower(v)
			return strings.Contains(v, "utf-8") || strings.Contains(v, "utf8")
		}
	}
	return true
}

// wrapWords breaks s into lines of at most n runes at spaces.
func wrapWords(s string, n int) []string {
	var out []string
	line := ""
	for _, f := range strings.Fields(s) {
		if line != "" && len([]rune(line))+1+len([]rune(f)) > n {
			out = append(out, line)
			line = f
			continue
		}
		if line != "" {
			line += " "
		}
		line += f
	}
	return append(out, line)
}

// fitsCutscene reports whether stdout is a terminal of at least the cutscene's size.
func (a *app) fitsCutscene() bool {
	f, ok := a.stdout.(*os.File)
	if !ok {
		return false
	}
	w, h, err := term.GetSize(int(f.Fd()))
	return err == nil && cutsceneFits(w, h)
}

func cutsceneFits(w, h int) bool { return w >= art.SceneW && h >= art.CutH }
