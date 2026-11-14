//go:build unix

package cli

import (
	"context"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/sapper-sh/sapper/internal/art"
)

// runConsole takes over the terminal after the boot reveal. The art stays on
// the top rows and is repainted in place; input and command output stay below it.
func (a *app) runConsole(ctx context.Context) error {
	in, ok := a.stdin.(*os.File)
	if !ok {
		return nil
	}
	out, ok := a.stdout.(*os.File)
	if !ok {
		return nil
	}
	cooked, err := term.MakeRaw(int(in.Fd()))
	if err != nil {
		return nil
	}
	defer term.Restore(int(in.Fd()), cooked)
	io.WriteString(out, "\x1b[?1049h\x1b[?25h")
	defer io.WriteString(out, "\x1b[0m\x1b[?25h\x1b[?1049l")

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)

	stopReader := make(chan struct{})
	defer close(stopReader)
	keys := make(chan []byte, 32)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 128)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				select {
				case keys <- append([]byte(nil), buf[:n]...):
				case <-stopReader: return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	view := consoleLayout(termSize(out))
	var edit lineEdit
	var log []string
	previousPhase := -1.0
	paint := func() {
		previousPhase = -1
		offset := view.offset
		view = consoleLayout(termSize(out))
		view.offset = min(offset,max(0,len(wrappedLog(log,view.cols))-(view.logEnd-view.logStart+1)))
		view.cursorBack = len(edit.buf)-edit.cursor
		view.blueprint = a.blueprintPath
		io.WriteString(out, renderConsole(view, log, edit.String()))
	}
	paint()
	start := time.Now()
	tick := time.NewTicker(40 * time.Millisecond)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-readDone:
			return nil
		case <-winch:
			paint()
		case now := <-tick.C:
			cols, rows := termSize(out)
			if cols != view.cols || rows != view.rows {
				continue
			}
			const cycle = 4 * time.Second
			phase := float64(now.Sub(start)%cycle) / float64(cycle)
			if delta := art.ShimmerDelta(view.art, cols, previousPhase, phase); delta != "" {
				io.WriteString(out, delta)
			}
			previousPhase = phase
		case chunk := <-keys:
		handleInput:
			beforeOffset := view.offset
			lines, quit := edit.feedBytes(chunk)
			if quit { return nil }
			var pending []byte
			view.cursorBack = len(edit.buf)-edit.cursor
			if edit.pages != 0 {
				height := view.logEnd-view.logStart+1
				view.offset = max(0,min(view.offset+edit.pages*height,max(0,len(wrappedLog(log,view.cols))-height)))
				edit.pages = 0
			}
			if len(lines) > 0 {
				for _, line := range lines {
					if strings.TrimSpace(line) == "" {
						continue
					}
					log = appendLog(log, "> "+line)
					view.offset = 0
					command := strings.Fields(line)[0]
					started := time.Now()
					log = appendLog(log, "Running "+command+"...")
					io.WriteString(out, renderConsoleBody(view, log, edit.String()))
					type result struct {
						text string
						err error
					}
					done := make(chan result, 1)
					taskCtx, cancel := context.WithCancel(ctx)
					go func(commandLine string) {
						text, err := a.execConsoleLine(taskCtx, commandLine)
						done <- result{text, err}
					}(line)
					var completed result
					closed := false
				working:
					for {
						select {
						case completed = <-done:
							break working
						case <-winch:
							paint()
						case input := <-keys:
							if bytes.Contains(input, []byte{3}) {
								cancel()
								closed = true
							} else {
								pending = append(pending,input...)
							}
						case <-readDone:
							cancel()
							completed = <-done
							closed = true
							break working
						case <-ctx.Done():
							cancel()
							completed = <-done
							closed = true
							break working
						}
					}
					cancel()
					if closed { return nil }
					text, runErr := completed.text, completed.err
					log = log[:len(log)-1]
					if text == "" && runErr != nil && !errors.Is(runErr, errConsoleExit) {
						text = runErr.Error()
					}
					if strings.TrimSpace(text) != "" {
						log = appendLog(log, text)
					}
					if !errors.Is(runErr, errConsoleExit) {
						outcome := "Completed"
						if runErr != nil {
							outcome = "Failed"
						}
						log = appendLog(log, fmt.Sprintf("%s %s (%s)",outcome,command,time.Since(started).Round(time.Millisecond)))
					}
					io.WriteString(out, renderConsoleBody(view, log, edit.String()))
					if errors.Is(runErr, errConsoleExit) || ctx.Err() != nil {
						return nil
					}
				}

			}
			if len(pending)>0 {
				chunk = pending
				goto handleInput
			}
			if view.offset != beforeOffset {
				io.WriteString(out, renderConsoleBody(view,log,edit.String()))
			} else {
				height:=view.logEnd-view.logStart+1
				above:=max(0,len(wrappedLog(log,view.cols))-height-view.offset)
				view.status=""
				if above>0 || view.offset>0 { view.status=fmt.Sprintf("PgUp/PgDn  %d above  %d below",above,view.offset) }
				io.WriteString(out, renderPrompt(view, edit.String()))
			}
		}
	}
}

func termSize(f *os.File) (int, int) {
	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil || w < 1 || h < 1 {
		return 80, 24
	}
	return w, h
}
