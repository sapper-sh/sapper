package cli

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/sapper-sh/sapper/internal/art"
)

// screen owns the terminal while the cutscene plays: alternate screen, hidden cursor,
// raw keyboard. restore undoes all of it exactly once, on any exit path.
type screen struct {
	w       io.Writer
	once    sync.Once
	undoRaw func()
}

func enterScreen(w io.Writer, in *os.File) *screen {
	s := &screen{w: w}
	if in != nil {
		if st, err := term.MakeRaw(int(in.Fd())); err == nil {
			s.undoRaw = func() { _ = term.Restore(int(in.Fd()), st) }
		}
	}
	io.WriteString(w, "\x1b[?1049h\x1b[?25l\x1b[2J")
	return s
}

func (s *screen) restore() {
	s.once.Do(func() {
		io.WriteString(s.w, "\x1b[0m\x1b[?25h\x1b[?1049l")
		if s.undoRaw != nil {
			s.undoRaw()
		}
	})
}

// playCutscene draws frames at a steady rate until the end, a key press or a signal.
// It returns false when it was interrupted by a signal.
func playCutscene(w io.Writer, cs *art.Cutscene, keys <-chan struct{}, sig <-chan os.Signal, tick time.Duration) bool {
	start := time.Now()
	n := cs.Frames()
	for {
		i := int(time.Since(start) / tick)
		if i >= n {
			i = n - 1
		}
		io.WriteString(w, cs.Frame(i))
		if i == n-1 {
			return true
		}
		next := start.Add(time.Duration(i+1) * tick)
		select {
		case <-keys:
			io.WriteString(w, cs.Frame(n-1))
			time.Sleep(600 * time.Millisecond)
			return true
		case <-sig:
			return false
		case <-time.After(time.Until(next)):
		}
	}
}

// runCutscene plays the cutscene full screen and always gives the terminal back.
func (a *app) runCutscene(cs *art.Cutscene) bool {
	var in *os.File
	if f, ok := a.stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		in = f
	}
	sc := enterScreen(a.stdout, in)
	defer sc.restore()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sig)
	keys := make(chan struct{})
	if in != nil {
		go func() {
			buf := make([]byte, 8)
			if n, err := in.Read(buf); err == nil && n > 0 {
				if buf[0] == 3 { // Ctrl-C arrives as a byte in raw mode
					sig <- os.Interrupt
					return
				}
				close(keys)
			}
		}()
	}
	ok := playCutscene(a.stdout, cs, keys, sig, time.Second/art.FPS)
	if ok {
		time.Sleep(1500 * time.Millisecond)
	}
	return ok
}
