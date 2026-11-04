package art

import (
	"fmt"
	"strings"
)

// Verdict frames a drill result line in the boot-art style.
type Verdict struct {
	Survived bool
	Title    string   // e.g. "BREACH DRILL // my-fortress"
	Lines    []string // detail lines
	// Word replaces SURVIVED/BREACHED when set, e.g. "N O   V E R D I C T".
	Word string
	// Neutral paints the word in ice instead of the failure accent.
	Neutral bool
}

// FrameWidth is the width of the verdict frame in columns.
const FrameWidth = 64

// Render returns the framed verdict. color enables ANSI colour.
func (v Verdict) Render(color bool) string {
	word, glyph := "S U R V I V E D", "▓▒░"
	c := Ice(1)
	if !v.Survived {
		word, c = "B R E A C H E D", Accent
	}
	if v.Word != "" {
		word = v.Word
	}
	if v.Neutral {
		c = Ice(.85)
	}
	edge := "░▒▓" + strings.Repeat("▀", FrameWidth-6) + "▓▒░"
	head := center(glyph+"  "+word+"  "+reverse(glyph), FrameWidth)
	paint := func(s string, col RGB) string {
		if !color {
			return s
		}
		return col.fg() + s + reset
	}
	var b strings.Builder
	fmt.Fprintln(&b, paint(edge, Ice(.6)))
	fmt.Fprintln(&b, paint(center(v.Title, FrameWidth), Ice(.85)))
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, paint(head, c))
	fmt.Fprintln(&b)
	for _, l := range v.Lines {
		fmt.Fprintln(&b, "  "+l)
	}
	fmt.Fprintln(&b, paint(edge, Ice(.6)))
	return b.String()
}

func center(s string, w int) string {
	n := len([]rune(s))
	if n >= w {
		return s
	}
	return strings.Repeat(" ", (w-n)/2) + s
}

func reverse(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}
