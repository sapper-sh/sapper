package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sapper-sh/sapper/internal/art"
	"github.com/sapper-sh/sapper/internal/termx"
)

func consoleApp(t *testing.T, bp string) *app {
	t.Helper()
	a := &app{stdin: strings.NewReader(""), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	a.rootCmd()
	a.blueprintPath = bp
	return a
}

func TestConsoleDispatchRunsRealCommands(t *testing.T) {
	bp := fortress(t)
	a := consoleApp(t, bp)
	ctx := context.Background()

	text, err := a.execConsoleLine(ctx, "validate")
	if err != nil || !strings.Contains(text, "valid (3 apps)") {
		t.Fatalf("validate: %v\n%s", err, text)
	}
	text, err = a.execConsoleLine(ctx, "status")
	if err != nil || !strings.Contains(text, "my-fortress") || !strings.Contains(text, "drill") {
		t.Fatalf("status: %v\n%s", err, text)
	}
	if strings.Contains(text, "S·A·P·P·E·R") {
		t.Fatal("status repeated the boot art inside the console")
	}
	text, err = a.execConsoleLine(ctx, "reports")
	if err != nil || !strings.Contains(text, "No drill reports yet") {
		t.Fatalf("reports: %v\n%s", err, text)
	}
	for _, line := range []string{"backup", "breach", "preflight"} {
		text, err = a.execConsoleLine(ctx, line)
		if err == nil || !strings.Contains(err.Error()+text, "SAPPER_RESTIC_PASSWORD") {
			t.Fatalf("%s: %v\n%s", line, err, text)
		}
	}

	help, err := a.execConsoleLine(ctx, "/help")
	if err != nil || !strings.Contains(help, "preflight") || !strings.Contains(help, consoleHint) || !strings.Contains(help, bp) {
		t.Fatalf("/help: %v\n%s", err, help)
	}
	help, err = a.execConsoleLine(ctx, "help validate")
	if err != nil || !strings.Contains(help, "Check the blueprint") {
		t.Fatalf("help validate: %v\n%s", err, help)
	}

	if _, err := a.execConsoleLine(ctx, "exit"); !errors.Is(err, errConsoleExit) {
		t.Fatalf("exit: %v", err)
	}
	if _, err := a.execConsoleLine(ctx, "quit"); !errors.Is(err, errConsoleExit) {
		t.Fatalf("quit: %v", err)
	}
}

func TestConsoleKeepsSessionFlags(t *testing.T) {
	bp := fortress(t)
	a := consoleApp(t, bp)
	ctx := context.Background()

	text, err := a.execConsoleLine(ctx, "status --json")
	if err != nil || !strings.Contains(text, `"fortress"`) {
		t.Fatalf("status --json: %v\n%s", err, text)
	}
	if a.jsonOut || a.blueprintPath != bp {
		t.Fatalf("session changed: json=%v path=%s", a.jsonOut, a.blueprintPath)
	}
	text, err = a.execConsoleLine(ctx, "status")
	if err != nil || !strings.Contains(text, "drill") || strings.Contains(text, `"fortress"`) {
		t.Fatalf("status after --json: %v\n%s", err, text)
	}

	missing := filepath.Join(t.TempDir(), "missing.blueprint")
	if _, err := a.execConsoleLine(ctx, "validate -f "+missing); err == nil {
		t.Fatal("missing blueprint validated")
	}
	if a.blueprintPath != bp {
		t.Fatalf("blueprint stuck on %s", a.blueprintPath)
	}

	if _, err := a.execConsoleLine(ctx, "status --bogus"); err == nil || !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("bogus flag: %v", err)
	}
	text, err = a.execConsoleLine(ctx, "status --help")
	if err != nil || !strings.Contains(text, "last backup") {
		t.Fatalf("status --help: %v\n%s", err, text)
	}
	text, err = a.execConsoleLine(ctx, "status")
	if err != nil || !strings.Contains(text, "drill") {
		t.Fatalf("status after help: %v\n%s", err, text)
	}
}

func TestConsoleRefusesCommandsOutsideItsSet(t *testing.T) {
	dir := t.TempDir()
	bp := filepath.Join(dir, "fortress.blueprint")
	a := consoleApp(t, bp)
	_, err := a.execConsoleLine(context.Background(), "init")
	if err == nil || !strings.Contains(err.Error(), consoleHint) {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(bp); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("init ran from the console")
	}
	if _, err := a.execConsoleLine(context.Background(), "detonate"); err == nil || !strings.Contains(err.Error(), consoleHint) {
		t.Fatal(err)
	}
}

func TestConsoleInputAndSplitting(t *testing.T) {
	var edit lineEdit
	for _, r := range "status" {
		edit.feed(r)
	}
	line, ok, quit, _ := edit.feed('\r')
	if !ok || quit || line != "status" || edit.String() != "" {
		t.Fatalf("enter: %q ok=%v quit=%v buf=%q", line, ok, quit, edit.String())
	}
	edit.feed('a')
	edit.feed('b')
	edit.feed(127)
	line, ok, _, _ = edit.feed('\n')
	if !ok || line != "a" {
		t.Fatalf("backspace: %q", line)
	}
	if _, _, quit, _ = edit.feed(3); !quit {
		t.Fatal("ctrl-c")
	}
	if _, _, quit, _ = edit.feed(4); !quit {
		t.Fatal("ctrl-d")
	}
	edit.feed('x')
	if _, _, quit, _ = edit.feed(4); quit {
		t.Fatal("ctrl-d should not quit while the line has text")
	}

	edit = lineEdit{}
	lines, quit := edit.feedBytes([]byte("status\rquit\r"))
	if quit || len(lines) != 2 || lines[0] != "status" || lines[1] != "quit" {
		t.Fatalf("feedBytes: %q quit=%v", lines, quit)
	}
	edit = lineEdit{}
	if _, quit = edit.feedBytes([]byte("ab\x1b[D")); quit || edit.String() != "ab" {
		t.Fatalf("arrow key leaked into the line: %q", edit.String())
	}

	got, err := splitConsoleArgs(`validate -f "my file.blueprint"`)
	if err != nil || len(got) != 3 || got[2] != "my file.blueprint" {
		t.Fatalf("split: %q %v", got, err)
	}
	if _, err := splitConsoleArgs(`"unterminated`); err == nil {
		t.Fatal("expected a quote error")
	}
	if text, err := consoleApp(t, "x").execConsoleLine(context.Background(), "   "); err != nil || text != "" {
		t.Fatalf("blank line: %q %v", text, err)
	}
}

// Cover the previous failure at 28-31 rows as well as transitions between
// banner sizes. Activity chrome must never downgrade the original wordmark.
func TestConsoleLayoutPreservesBannerAcrossResizes(t *testing.T) {
	for _, cols := range []int{72, 80, 100, 120} {
		for rows := 8; rows <= 64; rows++ {
			view := consoleLayout(cols, rows)
			want := art.Lines(art.FitBox(cols, max(1, rows-3-10)+1, art.Full))
			if strings.Join(view.art, "\n") != strings.Join(want, "\n") {
				t.Fatalf("%dx%d lost the original banner", cols, rows)
			}
			if view.logStart != len(view.art)+2 {
				t.Fatalf("%dx%d has a gap below the session rule", cols, rows)
			}
		}
	}
	view := consoleLayout(120, 30)
	if len(view.art) != len(art.Lines(art.Compact)) {
		t.Fatal("expected the original compact wordmark at 120x30")
	}
}

func TestConsoleLayoutAndRedrawDoNotScroll(t *testing.T) {
	tall := consoleLayout(100, 64)
	if !strings.Contains(strings.Join(tall.art, "\n"), "S·A·P·P·E·R") {
		t.Fatal("tall terminal should keep the full boot art")
	}
	if tall.promptRow != 63 || tall.hintRow != 64 || tall.logEnd-tall.logStart+1 < 10 {
		t.Fatalf("layout: %+v", tall)
	}
	short := consoleLayout(100, 24)
	art := strings.Join(short.art, "\n")
	if strings.Contains(art, "S·A·P·P·E·R") || !strings.Contains(art, "by the sapper of the unbuilt") {
		t.Fatal("short terminal should use the compact art")
	}
	tiny := consoleLayout(40, 8)
	if len(tiny.art) != 1 || tiny.promptRow != 7 || tiny.hintRow != 8 {
		t.Fatalf("tiny layout: %+v lines=%d", tiny, len(tiny.art))
	}


	frame := renderConsole(short, []string{"blueprint  fortress.blueprint"}, "status")
	if strings.Contains(frame, "\n") {
		t.Fatal("full frame contains a newline and can scroll the terminal")
	}
	if !strings.Contains(frame, consolePrompt+"status") || !strings.Contains(frame, "Enter to run status") || !strings.Contains(frame, "\x1b[1;1H") {
		t.Fatal("frame is missing the prompt, hint, or pinned art")
	}
	if prompt := renderPrompt(short, "status"); !strings.Contains(prompt, "\x1b[23;1H") || !strings.HasSuffix(prompt, "sapper> status\x1b[0m") || strings.Contains(prompt, "\x1b[2J") {
		t.Fatalf("typing repainted outside the input row: %q", prompt)
	}
}

func TestConsoleReadyStaysOff(t *testing.T) {
	a := &app{style: termx.Style{Color: true, TTY: true, Width: 80, Height: 40}}
	if a.consoleReady(false) {
		t.Fatal("stdin is not a terminal")
	}
	a.plain = true
	if a.consoleReady(false) {
		t.Fatal("plain")
	}
	a.plain, a.quiet = false, true
	if a.consoleReady(false) {
		t.Fatal("quiet")
	}
	a.quiet, a.jsonOut = false, true
	if a.consoleReady(false) {
		t.Fatal("json")
	}
	a.jsonOut = false
	if a.consoleReady(true) {
		t.Fatal("--no-anim")
	}
	t.Setenv("SAPPER_NO_ANIM", "1")
	if a.consoleReady(false) {
		t.Fatal("SAPPER_NO_ANIM")
	}
	t.Setenv("SAPPER_NO_ANIM", "")
	a.style.Color = false
	if a.consoleReady(false) {
		t.Fatal("no colour")
	}
}

func TestConsoleTabOnlyEditsAllowedCommand(t *testing.T) {
	for prefix, want := range map[string]string{"sta": "status", "b": "b", "ini": "ini", "backup x": "backup x", "/h": "/help"} {
		var edit lineEdit
		lines, quit := edit.feedBytes([]byte(prefix + "\t"))
		if quit || len(lines) != 0 || edit.String() != want {
			t.Fatalf("Tab after %q: draft %q, submitted %v", prefix, edit.String(), lines)
		}
	}
	if got := commandHint("b"); got != "backup  breach" {
		t.Fatalf("ambiguous prefix: %q", got)
	}
}
