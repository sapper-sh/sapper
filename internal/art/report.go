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
}

const frameWidth = 64

// Render returns the framed verdict. color enables ANSI colour.
func (v Verdict) Render(color bool) string {
	word, glyph := "S U R V I V E D", "▓▒░"
	c := Ice(1)
	if !v.Survived {
		word, c = "B R E A C H E D", Accent
	}
	edge := "░▒▓" + strings.Repeat("▀", frameWidth-6) + "▓▒░"
	head := center(glyph+"  "+word+"  "+reverse(glyph), frameWidth)
	paint := func(s string, col RGB) string {
		if !color {
			return s
		}
		return col.fg() + s + reset
	}
	var b strings.Builder
	fmt.Fprintln(&b, paint(edge, Ice(.6)))
	fmt.Fprintln(&b, paint(center(v.Title, frameWidth), Ice(.85)))
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
