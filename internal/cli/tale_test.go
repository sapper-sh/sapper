package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sapper-unbuilt/sapper/internal/art"
	"github.com/sapper-unbuilt/sapper/internal/blueprint"
	"github.com/sapper-unbuilt/sapper/internal/manifest"
	"github.com/sapper-unbuilt/sapper/internal/ops"
)

func writeDrill(t *testing.T, bpPath string, d ops.DrillReport) {
	t.Helper()
	bp, err := blueprint.Load(bpPath)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := ops.StateDir(bp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(d)
	if err := os.WriteFile(filepath.Join(dir, "last-drill.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestTaleWidthAndASCII(t *testing.T) {
	names := []string{"survived", "breached"}
	for _, ch := range art.Tale {
		names = append(names, ch.Art)
	}
	for _, name := range names {
		for _, ascii := range []bool{false, true} {
			lines := art.TaleArt(name, ascii)
			if len(lines) == 0 {
				t.Fatalf("missing art %s ascii=%v", name, ascii)
			}
			if w := art.Width(lines); w > 78 {
				t.Errorf("%s ascii=%v is %d columns", name, ascii, w)
			}
		}
	}
	for _, args := range [][]string{{"tale"}, {"tale", "--plain"}, {"tale", "--no-anim"}, {"tale", "--no-color"}} {
		r := run(t, "", args...)
		if r.code != 0 {
			t.Fatalf("%v: %+v", args, r)
		}
		for _, l := range strings.Split(r.stdout, "\n") {
			if n := utf8.RuneCountInString(l); n > 78 {
				t.Errorf("%v: line of %d columns: %q", args, n, l)
			}
		}
		lower := strings.ToLower(r.stdout)
		for _, b := range []string{"!", "delve", "seamless", "robust", "crucial", "simply", "just ", "unfortunately", "successfully"} {
			if strings.Contains(lower, b) {
				t.Errorf("%v: contains %q", args, b)
			}
		}
		if args[len(args)-1] == "--plain" {
			for i := 0; i < len(r.stdout); i++ {
				if c := r.stdout[i]; c >= 0x80 || (c < 0x20 && c != '\n') {
					t.Fatalf("plain output has non-ASCII byte %#x", c)
				}
			}
		}
	}
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_CTYPE", "")
	t.Setenv("LANG", "C")
	if r := run(t, "", "tale"); strings.ContainsAny(r.stdout, "▓█─") {
		t.Error("non-UTF-8 locale got Unicode art")
	}
}

func TestTaleJSONRefused(t *testing.T) {
	if r := run(t, "", "tale", "--json"); r.code != ExitUsage || !strings.Contains(r.stderr, "humans") {
		t.Fatalf("%+v", r)
	}
}

func TestTaleHidden(t *testing.T) {
	if r := run(t, "", "--help"); strings.Contains(r.stdout, "tale") {
		t.Error("tale listed in main help")
	}
	if r := run(t, "", "tale", "--help"); !strings.Contains(r.stdout, "bedtime story") {
		t.Errorf("tale --help: %+v", r)
	}
}

func TestTaleEnding(t *testing.T) {
	bp := fortress(t)
	now := time.Now()
	ending := func() string {
		var out bytes.Buffer
		a := &app{stdout: &out, blueprintPath: bp, plain: true}
		got, _ := a.taleEnding(now)
		return got
	}
	if got := ending(); got != "No dragon has visited yet. Run sapper breach to send one." {
		t.Errorf("none: %q", got)
	}
	writeDrill(t, bp, ops.DrillReport{Fortress: "f", Time: now.Add(-49 * time.Hour), Checked: 13})
	if got := ending(); got != "Your treasure survived the dragon 2 days ago. 13 of 13 drawings came back perfect." {
		t.Errorf("survived: %q", got)
	}
	r := run(t, "", "tale", "-f", bp)
	for _, l := range strings.Split(r.stdout, "\n") {
		if n := utf8.RuneCountInString(l); n > 78 {
			t.Errorf("survived line of %d columns: %q", n, l)
		}
	}
	if !strings.Contains(r.stdout, "13 of 13 drawings") || !strings.Contains(r.stdout, "▓") {
		t.Errorf("survived tale:\n%s", r.stdout)
	}
	writeDrill(t, bp, ops.DrillReport{Fortress: "f", Time: now, Checked: 13, Problems: []manifest.Problem{{}}})
	if got := ending(); got != "The dragon smudged 1 drawing. Run sapper reports show to see which." {
		t.Errorf("breached: %q", got)
	}
	r = run(t, "", "tale", "--plain", "-f", bp)
	if !strings.Contains(r.stdout, "The dragon smudged 1 drawing.") {
		t.Errorf("tale output missing ending:\n%s", r.stdout)
	}
}
