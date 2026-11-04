// Package art holds the sapper boot art, the ice palette and terminal output helpers.
package art

import (
	_ "embed"
	"fmt"
	"strings"
	"unicode/utf8"
)

//go:embed assets/full.txt
var fullTxt string

//go:embed assets/compact.txt
var compactTxt string

//go:embed assets/oneline.txt
var onelineTxt string

// Size selects an art variant.
type Size int

const (
	OneLine Size = iota
	Compact
	Full
)

// Lines returns the art lines for size.
func Lines(s Size) []string {
	switch s {
	case Full:
		return split(fullTxt)
	case Compact:
		return split(compactTxt)
	default:
		return split(onelineTxt)
	}
}

// Fit returns the largest variant no wider than cols. cols <= 0 means unknown and selects Full.
func Fit(cols int, max Size) Size {
	for s := max; s > OneLine; s-- {
		if cols <= 0 || Width(Lines(s)) <= cols {
			return s
		}
	}
	return OneLine
}

// Width returns the widest line in runes.
func Width(lines []string) int {
	w := 0
	for _, l := range lines {
		if n := utf8.RuneCountInString(l); n > w {
			w = n
		}
	}
	return w
}

// Render returns the art as text, coloured with the ice palette when color is true.
func Render(lines []string, color bool) string {
	var b strings.Builder
	w := Width(lines)
	for y, l := range lines {
		if color {
			b.WriteString(colorLine(l, y, len(lines), w))
		} else {
			b.WriteString(l)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func split(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// RGB is a 24-bit colour.
type RGB struct{ R, G, B uint8 }

func (c RGB) fg() string { return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B) }

const reset = "\x1b[0m"

// Ice palette stops, dark to hot.
var iceStops = []struct {
	at float64
	c  RGB
}{
	{0, RGB{0, 8, 16}}, {.3, RGB{0, 45, 80}}, {.6, RGB{0, 140, 200}}, {.85, RGB{120, 225, 255}}, {1, RGB{240, 252, 255}},
}

// Accent is the single warm colour, used only for failure.
var Accent = RGB{235, 20, 40}

// Ice maps v in [0,1] onto the ice palette.
func Ice(v float64) RGB {
	if v <= 0 {
		return iceStops[0].c
	}
	for i := 1; i < len(iceStops); i++ {
		a, b := iceStops[i-1], iceStops[i]
		if v <= b.at {
			t := (v - a.at) / (b.at - a.at)
			return RGB{lerp(a.c.R, b.c.R, t), lerp(a.c.G, b.c.G, t), lerp(a.c.B, b.c.B, t)}
		}
	}
	return iceStops[len(iceStops)-1].c
}

func lerp(a, b uint8, t float64) uint8 { return uint8(float64(a) + (float64(b)-float64(a))*t) }

var density = map[rune]float64{'█': 1, '▀': .92, '▄': .92, '▌': .9, '▐': .9, '▓': .62, '▒': .42, '░': .24}

// cellColor returns the colour for rune r at column x of a line in a block of width w.
func cellColor(r rune, x, w int) RGB {
	v, ok := density[r]
	if !ok {
		v = .8
	}
	if w > 0 {
		d := float64(x-w/2) / float64(w/2)
		if d < 0 {
			d = -d
		}
		v *= 1 - .35*d
	}
	return Ice(v)
}

func colorLine(l string, y, h, w int) string {
	var b strings.Builder
	var last RGB
	open := false
	x := 0
	for _, r := range l {
		if r == ' ' {
			b.WriteRune(r)
			x++
			continue
		}
		c := cellColor(r, x, w)
		if !open || c != last {
			b.WriteString(c.fg())
			last, open = c, true
		}
		b.WriteRune(r)
		x++
	}
	if open {
		b.WriteString(reset)
	}
	return b.String()
}
