//go:build windows

package termx

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT turns on ANSI escape processing for the console behind f.
// It returns false on consoles that cannot do so, and output falls back to plain text.
func enableVT(f *os.File) bool {
	h := windows.Handle(f.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
