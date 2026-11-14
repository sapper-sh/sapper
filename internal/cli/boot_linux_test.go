//go:build linux

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestBareCommandOnTTY(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("SAPPER_NO_ANIM", "")

	cases := []struct {
		name     string
		args     []string
		env      map[string]string
		rows     uint16
		art      string
		animate  bool
		help     bool
		noEscape bool
	}{
		{name: "animated", rows: 48, art: "S·A·P·P·E·R", animate: true},
		{name: "short terminal", rows: 24, art: "by the sapper of the unbuilt", animate: true},
		{name: "no-anim", args: []string{"--no-anim"}, rows: 48, art: "S·A·P·P·E·R"},
		{name: "SAPPER_NO_ANIM", rows: 48, art: "S·A·P·P·E·R", env: map[string]string{"SAPPER_NO_ANIM": "1"}},
		{name: "no-color", args: []string{"--no-color"}, rows: 48, art: "S·A·P·P·E·R", noEscape: true},
		{name: "NO_COLOR", rows: 48, art: "S·A·P·P·E·R", noEscape: true, env: map[string]string{"NO_COLOR": "1"}},
		{name: "plain", args: []string{"--plain"}, rows: 48, help: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			out, code := runPTY(t, tc.rows, 100, tc.args)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, out)
			}
			if tc.help {
				if !strings.Contains(out, "Available Commands:") {
					t.Fatalf("missing help:\n%s", out)
				}
				if strings.Contains(stripEscapes(out), "S·A·P·P·E·R") {
					t.Fatalf("plain mode printed boot art:\n%s", out)
				}
				return
			}
			if strings.Contains(out, "Available Commands:") {
				t.Fatalf("printed cobra help:\n%s", out)
			}
			if !strings.Contains(stripEscapes(out), tc.art) {
				t.Fatalf("missing %q\n%s", tc.art, stripEscapes(out))
			}
			if animated := strings.Contains(out, "\x1b[?25l"); animated != tc.animate {
				t.Fatalf("animation=%v, want %v", animated, tc.animate)
			}
			if tc.noEscape && strings.Contains(out, "\x1b[") {
				t.Fatal("escape codes in plain output")
			}
		})
	}
}

func runPTY(t *testing.T, rows, cols uint16, args []string) (string, int) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("ptmx: %v", err)
	}
	defer master.Close()
	// TIOCSPTLCK takes a pointer to an int. 0 unlocks the slave.
	if err := unix.IoctlSetPointerInt(int(master.Fd()), uint(unix.TIOCSPTLCK), 0); err != nil {
		t.Fatalf("unlock pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), uint(unix.TIOCGPTN))
	if err != nil {
		t.Fatalf("pty number: %v", err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("slave: %v", err)
	}
	ws := &unix.Winsize{Row: rows, Col: cols}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), uint(unix.TIOCSWINSZ), ws); err != nil {
		slave.Close()
		t.Fatalf("winsize: %v", err)
	}

	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = io.Copy(&buf, master)
	}()

	var errb bytes.Buffer
	code := Main(args, strings.NewReader(""), slave, &errb)
	slave.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out reading the terminal")
	}
	if errb.Len() != 0 {
		t.Fatalf("stderr: %s", errb.String())
	}
	return buf.String(), code
}

func stripEscapes(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		for i < len(s) && !(s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
			i++
		}
	}
	return b.String()
}
