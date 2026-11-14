package cli

import (
	"strings"
	"testing"

	"github.com/sapper-sh/sapper/internal/termx"
)

func TestBareCommandShowsBootArtNotHelp(t *testing.T) {
	r := run(t, "")
	if r.code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stdout, "S·A·P·P·E·R") {
		t.Fatalf("missing boot art:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "Available Commands:") || strings.Contains(r.stderr, "Available Commands:") {
		t.Fatalf("bare sapper printed cobra help:\n%s", r.stdout)
	}
	if strings.Contains(r.stdout, "\x1b[") {
		t.Fatal("non-interactive boot screen used escape codes")
	}
}

func TestHelpVersionAndSubcommands(t *testing.T) {
	help := run(t, "", "--help")
	if help.code != 0 || !strings.Contains(help.stdout, "Available Commands:") || !strings.Contains(help.stdout, " init ") {
		t.Fatalf("--help: %+v", help)
	}
	if strings.Contains(help.stdout, "S·A·P·P·E·R") {
		t.Fatalf("--help printed the boot art:\n%s", help.stdout)
	}

	sub := run(t, "", "help")
	if sub.code != 0 || !strings.Contains(sub.stdout, "Available Commands:") {
		t.Fatalf("help: %+v", sub)
	}

	ver := run(t, "", "--version")
	if ver.code != 0 || !strings.HasPrefix(ver.stdout, "sapper version ") {
		t.Fatalf("--version: %+v", ver)
	}

	initHelp := run(t, "", "init", "--help")
	if initHelp.code != 0 || !strings.Contains(initHelp.stdout, "fortress.blueprint") {
		t.Fatalf("init --help: %+v", initHelp)
	}
}

func TestPlainAndQuietRootShowHelpWithoutArt(t *testing.T) {
	for _, args := range [][]string{{"--plain"}, {"--quiet"}, {"-q"}} {
		r := run(t, "", args...)
		if r.code != 0 || !strings.Contains(r.stdout, "Available Commands:") {
			t.Fatalf("%v: %+v", args, r)
		}
		if strings.Contains(r.stdout, "S·A·P·P·E·R") || strings.Contains(r.stdout, "█") {
			t.Fatalf("%v printed boot art:\n%s", args, r.stdout)
		}
		if strings.Contains(r.stdout, "\x1b[") {
			t.Fatalf("%v printed escape codes", args)
		}
	}
}

func TestNonInteractiveColorFlagsStillShowArt(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
	}{
		{name: "no-color", args: []string{"--no-color"}},
		{name: "no-anim", args: []string{"--no-anim"}},
		{name: "both", args: []string{"--no-color", "--no-anim"}},
		{name: "NO_COLOR", env: "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("NO_COLOR", tc.env)
			}
			r := run(t, "", tc.args...)
			if r.code != 0 || !strings.Contains(r.stdout, "S·A·P·P·E·R") {
				t.Fatalf("%+v", r)
			}
			if strings.Contains(r.stdout, "Available Commands:") || strings.Contains(r.stdout, "\x1b[") {
				t.Fatalf("help or escapes:\n%s", r.stdout)
			}
		})
	}
}

func TestBootAnimated(t *testing.T) {
	colorTTY := termx.Style{Color: true, TTY: true, Width: 100, Height: 40}
	if !bootAnimated(colorTTY, 10, false) {
		t.Fatal("colour terminal should animate art that fits")
	}
	t.Setenv("SAPPER_NO_ANIM", "1")
	if bootAnimated(colorTTY, 10, false) {
		t.Fatal("SAPPER_NO_ANIM should skip animation")
	}
	t.Setenv("SAPPER_NO_ANIM", "")
	if bootAnimated(colorTTY, 10, true) {
		t.Fatal("--no-anim should skip animation")
	}
	if bootAnimated(termx.Style{TTY: true, Height: 40}, 10, false) {
		t.Fatal("colour disabled should skip animation")
	}
	if bootAnimated(termx.Style{Color: true, Height: 40}, 10, false) {
		t.Fatal("non-interactive output should skip animation")
	}
	if bootAnimated(colorTTY, 40, false) {
		t.Fatal("art as tall as the terminal should be printed still")
	}
	if !bootAnimated(termx.Style{Color: true, TTY: true}, 33, false) {
		t.Fatal("unknown height should still animate")
	}
}
