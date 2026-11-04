package art

import (
	"math"
	"math/rand/v2"
	"strings"
	"unicode/utf8"
)

// The cutscene for `sapper tale`. Every frame is a pure function of its index and the
// seed, so frames can be rendered out of order, skipped to the end and tested. Ideas
// borrowed from sl, asciiquarium and the towel.blinkenlights.nl ASCIImation: a steady
// frame rate, layers that move at different speeds, and particles for fire, sparks,
// snow and smoke. Nothing is copied from them.

// Ending selects how the cutscene finishes, from the real last drill.
type Ending int

const (
	EndNone Ending = iota
	EndSurvived
	EndBreached
)

// Cutscene geometry: the scene, a rule, five narration lines and a hint line.
const (
	SceneW  = 78
	SceneH  = 22
	CutH    = 30
	FPS     = 25
	cutSecs = 34.0
	groundY = 19
)

// Cell is one character with its colour; the zero Cell is blank.
type Cell struct {
	R rune
	C RGB
}

// Cutscene holds the fixed parts of the animation.
type Cutscene struct {
	Ending Ending
	Final  string // the personalised last line, already worded from real data
	seed   uint64
	stars  []star
	flakes []flake
	dragon [3][]string
	castle []string
	chest  []string
}

type star struct {
	x, y         int
	rate, phase  float64
	big          bool
	twinkleScale float64
}

type flake struct {
	x0, y0, speed, drift float64
	near                 bool
}

// NewCutscene builds a cutscene. The same seed always gives the same frames.
func NewCutscene(e Ending, final string, seed uint64) *Cutscene {
	c := &Cutscene{Ending: e, Final: final, seed: seed}
	rng := rand.New(rand.NewPCG(seed, 0x5A9))
	for i := 0; i < 46; i++ {
		c.stars = append(c.stars, star{rng.IntN(SceneW), rng.IntN(12), 1 + rng.Float64()*3, rng.Float64() * 6, rng.IntN(5) == 0, .4 + rng.Float64()*.6})
	}
	for i := 0; i < 34; i++ {
		c.flakes = append(c.flakes, flake{rng.Float64() * SceneW, rng.Float64() * SceneH, .8 + rng.Float64()*1.2, rng.Float64()*2 - 1, i%3 == 0})
	}
	for i := range c.dragon {
		c.dragon[i] = sprite("s-dragon-" + string(rune('0'+i)))
	}
	c.castle = sprite("s-castle")
	c.chest = chestSprite
	return c
}

func sprite(name string) []string {
	l := TaleArt(name, false)
	for len(l) > 0 && strings.TrimSpace(l[0]) == "" {
		l = l[1:]
	}
	return l
}

// Frames is the total number of frames.
func (c *Cutscene) Frames() int { return int(cutSecs * FPS) }

type canvas [CutH][SceneW]Cell

func (g *canvas) set(x, y int, r rune, col RGB) {
	if x >= 0 && x < SceneW && y >= 0 && y < CutH {
		g[y][x] = Cell{r, col}
	}
}

func (g *canvas) text(x, y int, s string, col RGB) {
	for _, r := range s {
		g.set(x, y, r, col)
		x++
	}
}

// blit draws a sprite with spaces transparent, brightness scaled by k, clipped to rows < clip.
func (g *canvas) blit(lines []string, x, y int, k float64, clip int) {
	w := Width(lines)
	for dy, l := range lines {
		if y+dy >= clip {
			break
		}
		for dx, r := range []rune(l) {
			if r != ' ' {
				g.set(x+dx, y+dy, r, scale(cellColor(r, dx, w), k))
			}
		}
	}
}

func scale(c RGB, k float64) RGB {
	f := func(v uint8) uint8 { return uint8(math.Max(0, math.Min(255, float64(v)*k))) }
	return RGB{f(c.R), f(c.G), f(c.B)}
}

func smooth(a, b, t float64) float64 {
	x := math.Max(0, math.Min(1, (t-a)/(b-a)))
	return x * x * (3 - 2*x)
}

func hash(seed uint64, i int) float64 {
	x := seed ^ uint64(i)*0x9E3779B97F4A7C15
	x ^= x >> 31
	x *= 0xBF58476D1CE4E5B9
	x ^= x >> 29
	return float64(x%1_000_003) / 1_000_003
}

// Knight sprites, drawn by hand: guard pose and two running strides. He faces right.
var (
	knightTop = []string{
		"   ▄███▄    ▄",
		"   █▀▀▀█▌   █",
		"   ▀█▄█▀    █",
		" ▄▄██████▄▄▄█▄▄",
		" ▐▓██████▌▐▓▓▓▌",
		"  ▓██████ ▐▓█▓▌",
	}
	knightLegs = [3][]string{
		{"   ▐█▌ ▐█▌ ▀▓▀", "  ▄██▌ ▐██▄"},
		{"   ▄█▀ ▀█▄ ▀▓▀", "  ▀▀     ▀▀"},
		{"    ▐███▌  ▀▓▀", "    ▄█▀█▄"},
	}
	chestSprite = []string{
		" ▄▄▄▄▄▄▄▄▄▄▄▄",
		"▐█▀▀█▀▀▀▀█▀▀█▌",
		"▐█▄▄█▄▐▌▄█▄▄█▌",
		"▐▓▓▓█▓▀▀▓█▓▓▓▌",
		"▐▓▓▓█▓▓▓▓█▓▓▓▌",
		" ▀▀▀▀▀▀▀▀▀▀▀▀",
	}
	moon = []string{" ▄██▀", "███", "███", " ▀██▄"}
)

// Narration is typed out beneath the scene, one block per chapter.
func (c *Cutscene) narration() []struct {
	at    float64
	lines []string
} {
	return []struct {
		at    float64
		lines []string
	}{
		{0, []string{"Imagine you have a treasure box full of your favourite drawings", "and photos."}},
		{4, []string{"sapper builds a strong castle to keep that box safe. Every night", "it makes a secret copy and hides it somewhere else."}},
		{9, []string{"Lots of people make copies and never check them. So every week", "sapper pretends a dragon smashed the castle, rebuilds everything", "from the secret copy and checks every drawing."}},
		{16, []string{"sapper is a knight who practises fighting dragons every week.", "It even spots a sneaky monster scribbling over all your drawings", "at once."}},
		{23, wrap(c.Final, 74)},
	}
}

func wrap(s string, n int) []string {
	var out []string
	line := ""
	for _, f := range strings.Fields(s) {
		if line != "" && utf8.RuneCountInString(line)+1+utf8.RuneCountInString(f) > n {
			out = append(out, line)
			line = f
			continue
		}
		if line != "" {
			line += " "
		}
		line += f
	}
	return append(out, line)
}

// FrameCells renders frame i into cells.
func (c *Cutscene) FrameCells(i int) *canvas {
	t := float64(i) / FPS
	g := &canvas{}
	c.sky(g, t)
	c.land(g, t)

	// chest glows in front of the gate from the first second
	glow := .55 + .45*math.Sin(t*3)
	cx, cy := 16, groundY-len(c.chest)+1
	g.blit(c.chest, cx, cy, (.6+.4*glow)*smooth(0, 1.5, t), CutH)
	if t < 4 { // a soft aura while the chest is alone on the snow
		for dx := -2; dx < 16; dx++ {
			g.set(cx+dx, cy-1, '░', scale(Ice(.5), glow*smooth(0, 1.5, t)))
		}
	}
	for k := 0; k < 6 && t < 9; k++ { // sparkles rising from the lid
		age := math.Mod(t*1.3+float64(k)/6, 1)
		x := cx + 2 + int(hash(c.seed, k+int(t*1.3)*7)*10)
		g.set(x, cy-1-int(age*3), []rune("·+*")[k%3], scale(Ice(1), 1-age))
	}

	// castle rises out of the ground at 4-7s
	rise := smooth(4, 7, t)
	if rise > 0 {
		h := len(c.castle)
		top := groundY + 1 - int(float64(h)*rise+.5)
		g.blit(c.castle, 1, top, .5+.5*rise, groundY+1)
		g.blit(c.chest, cx, cy, (.6+.4*glow)*smooth(0, 1.5, t), CutH) // the chest stays in front
	}

	dx, dy, flying := c.dragonAt(t)
	if flying {
		frame := []int{0, 1, 2, 1}[int(t*7)%4]
		g.blit(c.dragon[frame], int(dx), int(dy), 1, groundY+1)
	}
	c.fire(g, t, dx, dy)
	c.knight(g, t)
	c.ending(g, t)
	c.text(g, t)
	return g
}

func (c *Cutscene) sky(g *canvas, t float64) {
	for _, s := range c.stars {
		v := s.twinkleScale * (.35 + .65*math.Abs(math.Sin(t*s.rate+s.phase)))
		r := '·'
		if s.big && v > .7 {
			r = '+'
		}
		g.set(s.x, s.y, r, Ice(.4+.6*v))
	}
	g.blit(moon, 70, 1, 1, CutH)
	for _, f := range c.flakes { // two snow layers at different speeds
		sp := f.speed
		r, k := '·', .55
		if f.near {
			sp *= 2.2
			r, k = '*', .85
		}
		y := math.Mod(f.y0+sp*t, SceneH-2)
		x := math.Mod(f.x0+f.drift*t+1.2*math.Sin(t+f.y0)+SceneW*10, SceneW)
		if g[int(y)][int(x)].R == 0 {
			g.set(int(x), int(y), r, Ice(k))
		}
	}
}

func (c *Cutscene) land(g *canvas, t float64) {
	off := t * .9 // far hills drift slower than everything else
	for x := 0; x < SceneW; x++ {
		fx := float64(x) + off
		h := 2.5 + 1.6*math.Sin(fx*.13) + .9*math.Sin(fx*.37+1)
		for y := groundY - int(h); y <= groundY; y++ {
			r := '░'
			if y == groundY-int(h) {
				r = '▄'
			}
			g.set(x, y, r, Ice(.28))
		}
		g.set(x, groundY+1, '▀', Ice(.55))
		g.set(x, groundY+2, []rune("▓▒")[(x+int(t*4))%2], Ice(.35))
	}
}

// dragonAt is the dragon's top-left position.
func (c *Cutscene) dragonAt(t float64) (float64, float64, bool) {
	if t < 8 {
		return 0, 0, false
	}
	bob := math.Sin(t*3.5) * .8
	x := 80 - 36*smooth(8, 11.5, t) // in from the right
	y := 2 + bob
	if t >= 21 {
		x -= 4 * smooth(19, 21, t)
	}
	if t > 23 {
		switch c.Ending {
		case EndSurvived, EndBreached: // repelled, or done with its damage: it flies off
			a, b := 24.0, 28.0
			if c.Ending == EndBreached {
				a, b = 26.5, 30
			}
			p := smooth(a, b, t)
			x += 44 * p
			y -= 6 * p
		case EndNone: // circles the sky
			a := (t - 23) * .9
			x = 34 + 18*math.Cos(a)
			y = 2 + 2*math.Sin(a)
		}
	}
	return x, y, true
}

// fire is a stream of particles from the dragon's mouth; each particle's position is
// a closed-form function of its launch time, so no state is kept between frames.
func (c *Cutscene) fire(g *canvas, t, dx, dy float64) {
	type burst struct{ from, to, tx, ty float64 }
	bursts := []burst{{12, 15.5, 24, 11}, {19.5, 23, 59, 15}}
	if c.Ending == EndBreached {
		bursts = append(bursts, burst{23.5, 26, 38, 12})
	}
	mx, my := dx+1, dy+2
	for bi, b := range bursts {
		for k := 0; ; k++ {
			launch := b.from + float64(k/3)*.025
			if launch > b.to || launch > t {
				break
			}
			age := t - launch
			const life = .9
			if age > life {
				continue
			}
			j := hash(c.seed, bi*10000+k)
			vx := (b.tx - mx) / .7 * (.85 + .3*j)
			vy := (b.ty-my)/.7*(.8+.4*hash(c.seed, bi*10000+k+5000)) + 2*(j-.5)
			px, py := mx+vx*age, my+vy*age
			if bi == 1 && c.Ending == EndSurvived && t > 21.5 && px < b.tx+1 {
				px = b.tx + 1 + (b.tx+1-px)*.6 // the shield turns the fire back
			}
			f := age / life
			r := []rune("█▓▒░·")[int(f*4.99)]
			g.set(int(px), int(py), r, fireColor(f))
			if f < .5 {
				g.set(int(px)+1, int(py), r, fireColor(f+.15))
			}
		}
	}
}

func fireColor(f float64) RGB { return Ice(1 - .75*f) }

func (c *Cutscene) knight(g *canvas, t float64) {
	if t < 16 {
		return
	}
	run := smooth(16, 19, t)
	x := 18 + int(28*run)
	y := groundY - len(knightTop) - 1
	legs := knightLegs[0]
	if t < 19 {
		legs = knightLegs[1+int(t*8)%2]
	}
	g.blit(knightTop, x, y, 1, CutH)
	g.blit(legs, x, y+len(knightTop), 1, CutH)
	if t > 19.8 && t < 23.5 { // sparks where fire meets the shield
		sx, sy := float64(x+13), float64(y+4)
		for k := 0; k < 14; k++ {
			p := math.Mod(t*2.2+hash(c.seed, k), 1)
			a := hash(c.seed, k+99)*math.Pi - math.Pi/2
			r := 1 + 5*p
			g.set(int(sx+r*math.Cos(a)*1.8), int(sy+r*math.Sin(a)), []rune("*+·")[int(p*2.99)], Ice(1-.5*p))
		}
	}
}

func (c *Cutscene) ending(g *canvas, t float64) {
	if t < 23 {
		return
	}
	switch c.Ending {
	case EndSurvived:
		up := smooth(26, 29, t)
		if up > 0 { // flag climbs the keep and shimmers
			px := 22
			top := groundY + 1 - len(c.castle) - 4
			for y := top; y < top+4; y++ {
				g.set(px, y, '│', Ice(.9))
			}
			fy := top + 3 - int(3*up)
			for k, r := range []rune("▀▀▀▀▄") {
				g.set(px+1+k, fy, r, Ice(.6+.4*math.Sin(float64(k)*.9-t*7)))
			}
		}
		c.banner(g, t, 28, "S U R V I V E D", false)
	case EndBreached:
		crack := smooth(25, 26.5, t)
		pts := [][2]int{{38, 11}, {39, 12}, {38, 13}, {39, 14}, {38, 15}, {39, 16}}
		for k := 0; k < int(crack*float64(len(pts))); k++ {
			g.set(pts[k][0], pts[k][1], []rune("╲╱")[k%2], Accent)
		}
		if t > 25.5 {
			for k := 0; k < 18; k++ { // smoke from the broken tower
				p := math.Mod((t-25.5)*.45+hash(c.seed, k+300), 1)
				x := 38 + int(p*9+2*math.Sin(t*1.5+float64(k)))
				g.set(x, 10-int(p*9), []rune("▒░·")[int(p*2.99)], scale(Ice(.5), 1-.6*p))
			}
		}
		c.banner(g, t, 29.5, "B R E A C H E D", true)
	default:
		c.banner(g, t, 27, "N O   D R I L L   Y E T", false)
	}
}

func (c *Cutscene) banner(g *canvas, t, at float64, s string, red bool) {
	if t < at {
		return
	}
	x := 61 - (utf8.RuneCountInString(s)+10)/2
	full := "▓▒░  " + s + "  ░▒▓"
	for k, r := range []rune(full) {
		col := Ice(.75 + .25*math.Sin(float64(k)*.5-t*5))
		if red && r != '▓' && r != '▒' && r != '░' {
			col = Accent
		}
		if float64(k) < (t-at)*40 {
			g.set(x+k, 6, r, col)
		}
	}
}

func (c *Cutscene) text(g *canvas, t float64) {
	g.text(1, SceneH+0, strings.Repeat("─", SceneW-2), Ice(.45))
	nar := c.narration()
	cur := nar[0]
	for _, n := range nar {
		if t >= n.at {
			cur = n
		}
	}
	budget := int((t - cur.at) * 45)
	for k, l := range cur.lines {
		r := []rune(l)
		if budget < len(r) {
			r = r[:max(budget, 0)]
		}
		budget -= len(r)
		g.text(2, SceneH+1+k, string(r), Ice(.92))
	}
	g.text(2, CutH-1, "any key skips", Ice(.4))
}

// Frame renders frame i as ANSI: home the cursor and redraw every row in one write.
func (c *Cutscene) Frame(i int) string {
	g := c.FrameCells(i)
	var b strings.Builder
	b.WriteString("\x1b[H")
	for y := 0; y < CutH; y++ {
		var last RGB
		open := false
		for x := 0; x < SceneW; x++ {
			cl := g[y][x]
			if cl.R == 0 || cl.R == ' ' {
				b.WriteByte(' ')
				continue
			}
			if !open || cl.C != last {
				b.WriteString(cl.C.fg())
				last, open = cl.C, true
			}
			b.WriteRune(cl.R)
		}
		b.WriteString(reset + "\x1b[K")
		if y < CutH-1 {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}

// Text returns frame i as plain text, one string per row, for tests.
func (c *Cutscene) Text(i int) []string {
	g := c.FrameCells(i)
	out := make([]string, CutH)
	for y := range g {
		var b strings.Builder
		for _, cl := range g[y] {
			if cl.R == 0 {
				b.WriteByte(' ')
			} else {
				b.WriteRune(cl.R)
			}
		}
		out[y] = b.String()
	}
	return out
}
