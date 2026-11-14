//go:build linux

package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestConsoleSessionOnPTY(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("SAPPER_NO_ANIM", "")
	bp := fortress(t)
	t.Chdir(filepath.Dir(bp))

	out, code := scriptPTY(t, 48, 100, nil, func(master *os.File, output func() string) {
		waitFor(t, output, consolePrompt, 8*time.Second)
		waitFor(t, output, consoleHint, time.Second)
		if _, err := master.Write([]byte("validate\rstatus\rreports\rbackup\rexit\r")); err != nil {
			t.Fatal(err)
		}
	})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, tail(stripEscapes(out)))
	}
	plain := stripEscapes(out)
	for _, want := range []string{consolePrompt, consoleHint, "valid (3 apps)", "my-fortress", "No drill reports yet", "SAPPER_RESTIC_PASSWORD"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q\n%s", want, tail(plain))
		}
	}
	assertConsoleRestored(t, out)
}

func TestConsoleCtrlCRestoresTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("SAPPER_NO_ANIM", "")
	out, code := scriptPTY(t, 32, 100, nil, func(master *os.File, output func() string) {
		waitFor(t, output, consolePrompt, 8*time.Second)
		if _, err := master.Write([]byte{3}); err != nil {
			t.Fatal(err)
		}
	})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, tail(stripEscapes(out)))
	}
	assertConsoleRestored(t, out)
}

func TestConsoleResizeRestoresTerminal(t *testing.T) {
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	t.Setenv("SAPPER_NO_ANIM", "")
	out, code := scriptPTY(t, 32, 100, nil, func(master *os.File, output func() string) {
		waitFor(t, output, consolePrompt, 8*time.Second)
		ws := &unix.Winsize{Row: 28, Col: 90}
		if err := unix.IoctlSetWinsize(int(master.Fd()), uint(unix.TIOCSWINSZ), ws); err != nil {
			t.Fatal(err)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)
		time.Sleep(250 * time.Millisecond)
		if _, err := master.Write([]byte("exit\r")); err != nil {
			t.Fatal(err)
		}
	})
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, tail(stripEscapes(out)))
	}
	assertConsoleRestored(t, out)
	if strings.Count(out, "\x1b[2J") < 2 {
		t.Fatal("resize did not redraw the fixed frame")
	}
}

func TestTTYModesThatMustNotOpenTheConsole(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  map[string]string
	}{
		{name: "no-anim", args: []string{"--no-anim"}},
		{name: "NO_COLOR", env: map[string]string{"NO_COLOR": "1"}},
		{name: "SAPPER_NO_ANIM", env: map[string]string{"SAPPER_NO_ANIM": "1"}},
		{name: "plain", args: []string{"--plain"}},
		{name: "quiet", args: []string{"-q"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TERM", "xterm-256color")
			t.Setenv("NO_COLOR", "")
			t.Setenv("SAPPER_NO_ANIM", "")
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			out, code := scriptPTY(t, 24, 80, tc.args, nil)
			if code != 0 {
				t.Fatalf("exit %d\n%s", code, tail(stripEscapes(out)))
			}
			if strings.Contains(out, "\x1b[?1049h") || strings.Contains(stripEscapes(out), consolePrompt) {
				t.Fatal("opened the interactive console")
			}
		})
	}
}

func assertConsoleRestored(t *testing.T, out string) {
	t.Helper()
	enter := strings.Index(out, "\x1b[?1049h")
	leave := strings.LastIndex(out, "\x1b[?1049l")
	if enter < 0 || leave < enter {
		t.Fatal("alternate screen was not entered and left")
	}
	if !strings.Contains(out[leave:], "\x1b[?25h") && !strings.Contains(out[:leave], "\x1b[?25h") {
		t.Fatal("cursor was not restored")
	}
	body := out[enter:leave]
	if strings.Contains(body, "\n") {
		t.Fatal("console wrote newlines after taking the screen")
	}
	if !strings.Contains(body, "\x1b[1;1H") {
		t.Fatal("art was not pinned to the top row")
	}
}

func scriptPTY(t *testing.T, rows, cols uint16, args []string, play func(master *os.File, output func() string)) (string, int) {
	t.Helper()
	master, slave := openTestPTY(t)
	defer master.Close()
	ws := &unix.Winsize{Row: rows, Col: cols}
	if err := unix.IoctlSetWinsize(int(slave.Fd()), uint(unix.TIOCSWINSZ), ws); err != nil {
		slave.Close()
		t.Fatal(err)
	}
	var buf memBuf
	copyDone := make(chan struct{})
	go func() {
		defer close(copyDone)
		_, _ = io.Copy(&buf, master)
	}()
	codeCh := make(chan int, 1)
	go func() {
		var errb bytes.Buffer
		codeCh <- Main(args, slave, slave, &errb)
	}()
	var closeSlave sync.Once
	finishSlave := func() { closeSlave.Do(func() { slave.Close() }) }
	defer finishSlave()
	if play != nil {
		play(master, buf.String)
	}
	var code int
	select {
	case code = <-codeCh:
	case <-time.After(12 * time.Second):
		t.Fatalf("sapper did not exit\n%s", tail(stripEscapes(buf.String())))
	}
	finishSlave()
	select {
	case <-copyDone:
	case <-time.After(time.Second):
	}
	return buf.String(), code
}

func openTestPTY(t *testing.T) (master, slave *os.File) {
	t.Helper()
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("ptmx: %v", err)
	}
	if err := unix.IoctlSetPointerInt(int(master.Fd()), uint(unix.TIOCSPTLCK), 0); err != nil {
		master.Close()
		t.Fatalf("unlock pty: %v", err)
	}
	n, err := unix.IoctlGetInt(int(master.Fd()), uint(unix.TIOCGPTN))
	if err != nil {
		master.Close()
		t.Fatalf("pty number: %v", err)
	}
	slave, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		t.Fatalf("slave: %v", err)
	}
	return master, slave
}

type memBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (m *memBuf) Write(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.b.Write(p)
}

func (m *memBuf) String() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.b.String()
}

func waitFor(t *testing.T, output func() string, needle string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if strings.Contains(output(), needle) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q\n%s", needle, tail(stripEscapes(output())))
}

func tail(s string) string {
	if len(s) > 1800 {
		return s[len(s)-1800:]
	}
	return s
}
