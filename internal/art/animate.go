package art

import (
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"time"
)

const noise = "█▓▒░▀▄#@$%&01<>/\\|=+"

// Animate plays a decrypt reveal of lines on a terminal and always ends by drawing the
// final art. It returns early when skip is closed or receives.
func Animate(w io.Writer, lines []string, frames int, delay time.Duration, skip <-chan struct{}) {
	if frames < 2 {
		frames = 2
	}
	rng := rand.New(rand.NewPCG(7, 11))
	grid := make([][]rune, len(lines))
	reveal := make([][]float64, len(lines))
	for y, l := range lines {
		grid[y] = []rune(l)
		reveal[y] = make([]float64, len(grid[y]))
		for x := range grid[y] {
			reveal[y][x] = float64(y)/float64(len(lines))*.75 + rng.Float64()*.25
		}
	}
	width := Width(lines)
	noiseRunes := []rune(noise)
	fmt.Fprint(w, "\x1b[?25l")
	fmt.Fprint(w, strings.Repeat("\n", len(lines)))
	fmt.Fprintf(w, "\x1b[%dA", len(lines))
	defer fmt.Fprint(w, "\x1b[?25h")
	draw := func(t float64, final bool) {
		var b strings.Builder
		for y, row := range grid {
			for x, r := range row {
				switch {
				case r == ' ':
					b.WriteRune(' ')
				case final || t >= reveal[y][x]+.08:
					b.WriteString(cellColor(r, x, width).fg())
					b.WriteRune(r)
				case t >= reveal[y][x]-.15:
					b.WriteString(Ice(.6).fg())
					b.WriteRune(noiseRunes[rng.IntN(len(noiseRunes))])
				default:
					b.WriteRune(' ')
				}
			}
			b.WriteString(reset + "\x1b[K\n")
		}
		fmt.Fprintf(&b, "\x1b[%dA", len(lines))
		io.WriteString(w, b.String())
	}
	for i := 0; i < frames; i++ {
		select {
		case <-skip:
			i = frames
			continue
		default:
		}
		draw(float64(i)/float64(frames-1)*1.1, false)
		time.Sleep(delay)
	}
	draw(1, true)
	fmt.Fprintf(w, "\x1b[%dB", len(lines))
}
