// Package termx decides how output should look on the current terminal.
package termx

import (
	"os"

	"golang.org/x/term"
)

// IsTTY reports whether f is a terminal.
func IsTTY(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// Width returns the terminal width of f, or 0 when unknown.
func Width(f *os.File) int {
	w, _ := sizeOf(f)
	return w
}

func sizeOf(f *os.File) (width, height int) {
	w, h, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0, 0
	}
	return w, h
}

// Style is the resolved output style.
type Style struct {
	Color  bool // ANSI colour allowed
	TTY    bool // output is an interactive terminal
	Width  int  // columns, 0 when unknown
	Height int  // rows, 0 when unknown
}

// Detect resolves the style for f. noColor covers --no-color and --plain.
func Detect(f *os.File, noColor bool) Style {
	s := Style{TTY: IsTTY(f)}
	if s.TTY {
		s.Width, s.Height = sizeOf(f)
	}
	s.Color = s.TTY && !noColor && colorAllowedByEnv(os.Getenv) && enableVT(f)
	return s
}

// colorAllowedByEnv applies the NO_COLOR convention (https://no-color.org) and TERM=dumb.
func colorAllowedByEnv(getenv func(string) string) bool {
	return getenv("NO_COLOR") == "" && getenv("TERM") != "dumb"
}
