package art

import (
	"math"
	"math/rand/v2"
	"strings"
	"unicode/utf8"
)

// The cutscene for `sapper tale`.
//
// The scene is a 78x44 pixel framebuffer shown with the half-block technique used by
// notcurses' 2x1 blitter and chafa: each cell is "▀" with the top pixel as foreground
// and the bottom pixel as background, so a 78x22 cell area carries 78x44 truecolour
// pixels. Lighting follows Brogue's model: every material has a pigment, light sources
// add colour with a radial falloff, and the pigment is multiplied by the light. The
// dragon's breath is a heat field in the spirit of the Doom PSX fire (Fabien Sanglard,
// "How DOOM fire was done"): heat is injected, carried along a wind vector with random
// jitter, diffused and cooled, then mapped through a palette, here cold plasma.
// Animation uses anticipation, a held hit, recoil and follow-through on the wings,
// tail, cape and flag. Every frame depends only on its index and the seed.

// Ending selects how the cutscene finishes, from the real last drill.
type Ending int

const (
	EndNone Ending = iota
	EndSurvived
	EndBreached
)

// Geometry: the scene, then a rule, narration and a hint line.
const (
	SceneW  = 78
	SceneH  = 22
	CutH    = 30
	FPS     = 25
	cutSecs = 38.0
	pxH     = SceneH * 2
	groundY = 38.0
)

// Cell is one character with its colour; the zero Cell is blank.
type Cell struct {
	R  rune
	C  RGB
	BG RGB
	HB bool // BG is set
}

type vec struct{ r, g, b float64 }

func (a vec) add(b vec) vec     { return vec{a.r + b.r, a.g + b.g, a.b + b.b} }
func (a vec) mul(k float64) vec { return vec{a.r * k, a.g * k, a.b * k} }
func (a vec) times(b vec) vec   { return vec{a.r * b.r, a.g * b.g, a.b * b.b} }
func (a vec) lerp(b vec, t float64) vec {
	return vec{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}
func (a vec) rgb() RGB {
	c := func(v float64) uint8 { return uint8(math.Max(0, math.Min(255, v*255))) }
	return RGB{c(a.r), c(a.g), c(a.b)}
}
func hex(r, g, b int) vec { return vec{float64(r) / 255, float64(g) / 255, float64(b) / 255} }

// Materials. Pigments are lit by the light map; emissive materials glow by themselves.
type mat uint8

const (
	mSky mat = iota
	mMount
	mGround
	mWall
	mDragon
	mWarden
	mCloth
	mEmit // emissive, colour stored in emit
)

// Cutscene holds the fixed parts of the animation and the fire simulation.
type Cutscene struct {
	Ending Ending
	Final  string
	seed   uint64
	stars  [][3]float64
	flakes [][4]float64
	emblem []string

	// fire simulation state, stepped forward one frame at a time
	heat, tmp [pxH][SceneW]float64
	simAt     int
}

// NewCutscene builds a cutscene. The same seed always gives the same frames.
func NewCutscene(e Ending, final string, seed uint64) *Cutscene {
	c := &Cutscene{Ending: e, Final: final, seed: seed, simAt: -1}
	rng := rand.New(rand.NewPCG(seed, 0x5A9))
	for i := 0; i < 40; i++ {
		c.stars = append(c.stars, [3]float64{rng.Float64() * SceneW, rng.Float64() * 18, rng.Float64() * 6})
	}
	for i := 0; i < 45; i++ {
		c.flakes = append(c.flakes, [4]float64{rng.Float64() * SceneW, rng.Float64() * pxH, .5 + rng.Float64(), float64(i % 3)})
	}
	for _, l := range Lines(Compact) {
		c.emblem = append(c.emblem, strings.Replace(l, "by the sapper of the unbuilt", "BY THE SAPPER OF THE UNBUILT", 1))
	}
	return c
}

// Frames is the total number of frames.
func (c *Cutscene) Frames() int { return int(cutSecs * FPS) }

// frame is one rendered scene: pixels, their materials and the text overlay.
type frame struct {
	t     float64
	pig   [pxH][SceneW]vec
	mt    [pxH][SceneW]mat
	emit  [pxH][SceneW]vec
	light [pxH][SceneW]vec
	camX  float64

	// zoom for the close-up: a world point w is drawn at o + (w - p) * z
	z, px, py, ox, oy float64
}

func (f *frame) tx(x, y float64) (float64, float64)  { return f.ox + (x-f.px)*f.z, f.oy + (y-f.py)*f.z }
func (f *frame) inv(x, y float64) (float64, float64) { return f.px + (x-f.ox)/f.z, f.py + (y-f.oy)/f.z }

// put sets the world pixel (x, y), as a z-by-z block when zoomed in.
func (f *frame) put(x, y int, m mat, col vec) {
	sx, sy := f.tx(float64(x), float64(y))
	n := int(math.Ceil(f.z))
	for dy := 0; dy < n; dy++ {
		for dx := 0; dx < n; dx++ {
			f.raw(int(sx)+dx, int(sy)+dy, m, col)
		}
	}
}

// raw sets a screen pixel, ignoring the zoom.
func (f *frame) raw(x, y int, m mat, col vec) {
	if x >= 0 && x < SceneW && y >= 0 && y < pxH {
		f.mt[y][x] = m
		if m == mEmit {
			f.emit[y][x] = col
			f.pig[y][x] = vec{}
		} else {
			f.pig[y][x] = col
			f.emit[y][x] = vec{}
		}
	}
}

// poly fills a polygon given in world pixels; shade returns the pigment at a pixel.
func (f *frame) poly(world [][2]float64, m mat, shade func(x, y int) vec) {
	pts := make([][2]float64, len(world))
	for i, p := range world {
		pts[i][0], pts[i][1] = f.tx(p[0], p[1])
	}
	minX, maxX, minY, maxY := 1e9, -1e9, 1e9, -1e9
	for _, p := range pts {
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	for y := int(math.Max(0, math.Floor(minY))); y <= int(math.Min(pxH-1, maxY)); y++ {
		for x := int(math.Max(0, math.Floor(minX))); x <= int(math.Min(SceneW-1, maxX)); x++ {
			if inside(pts, float64(x)+.5, float64(y)+.5) {
				wx, wy := f.inv(float64(x), float64(y))
				f.raw(x, y, m, shade(int(wx), int(wy)))
			}
		}
	}
}

func inside(pts [][2]float64, x, y float64) bool {
	in := false
	for i, j := 0, len(pts)-1; i < len(pts); j, i = i, i+1 {
		a, b := pts[i], pts[j]
		if (a[1] > y) != (b[1] > y) && x < (b[0]-a[0])*(y-a[1])/(b[1]-a[1])+a[0] {
			in = !in
		}
	}
	return in
}

func (f *frame) disc(cx, cy, r float64, m mat, col vec) {
	cx, cy = f.tx(cx, cy)
	r *= f.z
	for y := int(cy - r - 1); y <= int(cy+r+1); y++ {
		for x := int(cx - r - 1); x <= int(cx+r+1); x++ {
			dx, dy := float64(x)+.5-cx, float64(y)+.5-cy
			if dx*dx+dy*dy <= r*r {
				f.raw(x, y, m, col)
			}
		}
	}
}

func (f *frame) line(x0, y0, x1, y1, w float64, m mat, col vec) {
	n := int(math.Max(math.Abs(x1-x0), math.Abs(y1-y0))*2) + 1
	for i := 0; i <= n; i++ {
		t := float64(i) / float64(n)
		f.disc(x0+(x1-x0)*t, y0+(y1-y0)*t, w/2, m, col)
	}
}

func flat(c vec) func(int, int) vec { return func(int, int) vec { return c } }

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

// noise is smooth value noise in [0,1].
func (c *Cutscene) noise(x, y float64) float64 {
	xi, yi := math.Floor(x), math.Floor(y)
	fx, fy := x-xi, y-yi
	fx, fy = fx*fx*(3-2*fx), fy*fy*(3-2*fy)
	h := func(i, j float64) float64 { return hash(c.seed, int(i)*7919+int(j)*104729) }
	a := h(xi, yi) + (h(xi+1, yi)-h(xi, yi))*fx
	b := h(xi, yi+1) + (h(xi+1, yi+1)-h(xi, yi+1))*fx
	return a + (b-a)*fy
}

func (c *Cutscene) fbm(x, y float64) float64 {
	return .55*c.noise(x, y) + .3*c.noise(x*2.1, y*2.1) + .15*c.noise(x*4.3, y*4.3)
}

// ---------------------------------------------------------------- timeline

const (
	tRise     = 1.5  // citadel rises 1.5-6
	tRunes    = 6.5  // runes ignite
	tSilence  = 10.5 // snow stops, the world holds its breath
	tStrike   = 12.6 // lightning, the dragon arrives
	tFire1    = 16.2 // breath on the citadel
	tKnight   = 19.0 // the sentinel marches out
	tFire2    = 22.0 // breath on the shield
	tResolve  = 25.0 // the ending begins
	tBanner   = 29.5
	tEmblem   = 31.5
	fireLen   = 2.6
	impactLag = .35
)

// Lightning flashes, each a time and a strength.
var flashes = [][2]float64{{4.2, .35}, {tStrike, 1}, {13.4, .5}}

func (c *Cutscene) flash(t float64) float64 {
	v := 0.0
	for _, f := range flashes {
		if d := t - f[0]; d >= 0 && d < .5 {
			v = math.Max(v, f[1]*math.Exp(-d*9)*(.7+.3*math.Cos(d*60)))
		}
	}
	for _, at := range c.impacts() {
		if d := t - at; d >= 0 && d < .3 {
			v = math.Max(v, .55*math.Exp(-d*14))
		}
	}
	return v
}

func (c *Cutscene) impacts() []float64 {
	out := []float64{tFire1 + impactLag, tFire2 + impactLag}
	if c.Ending == EndBreached {
		out = append(out, tResolve+.9+.8)
	}
	return out
}

// shake is a small decaying camera jolt after each impact.
func (c *Cutscene) shake(t float64) (int, int) {
	for k, at := range c.impacts() {
		if d := t - at; d >= 0 && d < .35 {
			a := 1.4 * (1 - d/.35)
			return int(math.Round(a * (hash(c.seed, k*97+int(d*FPS)) - .5) * 2)), int(math.Round(a * (hash(c.seed, k*89+int(d*FPS)+7) - .5)))
		}
	}
	return 0, 0
}

// ---------------------------------------------------------------- frame

// Close-up for the clash: the warden is small beside his gate, so the camera pushes in
// on him while the breath hits his shield, then pulls back out.
const (
	closeZoom = 2.0
	knightK   = .36 // the warden's scale: about 7 px tall against an 11 px gate
)

func (c *Cutscene) camera(f *frame) {
	k := smooth(tFire2-1.8, tFire2-1, f.t) * (1 - smooth(tFire2+fireLen+.2, tFire2+fireLen+1.2, f.t))
	f.z = 1 + (closeZoom-1)*k
	kx, ky := c.knightAt(f.t)
	f.px, f.py = kx-f.camX+1, ky-3
	f.ox, f.oy = f.px+(SceneW*.5-f.px)*k, f.py+(pxH*.7-f.py)*k
}

func (c *Cutscene) knightAt(t float64) (float64, float64) {
	march := smooth(tKnight, tKnight+2.6, t)
	return 26 + 20*march, groundY
}

func (c *Cutscene) wizardVisible(t float64) bool { return t >= .8 }

// FrameCells renders frame i into cells.
func (c *Cutscene) FrameCells(i int) *[CutH][SceneW]Cell {
	t := float64(i) / FPS
	f := &frame{t: t, camX: 7 * smooth(0, cutSecs, t)}
	c.camera(f)
	c.stepFire(i)
	c.sky(f)
	c.mountains(f)
	c.ground(f)
	c.citadel(f)
	dx, dy, ok := c.dragonPos(t)
	if ok {
		c.dragon(f, dx-f.camX, dy)
	}
	c.knight(f)
	c.inhale(f)
	c.frost(f)
	c.endingScene(f)
	c.snow(f)
	c.lights(f, dx-f.camX, dy, ok)
	out := c.compose(f)
	c.overlay(f, out)
	return out
}

func (c *Cutscene) sky(f *frame) {
	fl := c.flash(f.t)
	top, hor := hex(5, 8, 16), hex(30, 46, 66)
	for y := 0; y < pxH; y++ {
		g := top.lerp(hor, math.Pow(float64(y)/groundY, 1.6))
		for x := 0; x < SceneW; x++ {
			// storm clouds drifting on the wind, lit from inside by lightning
			n := c.fbm((float64(x)+f.camX*.15+f.t*1.2)*.07, float64(y)*.16)
			cl := smooth(.45, .75, n) * (1 - float64(y)/groundY)
			col := g.lerp(hex(42, 56, 76), cl*.85).add(hex(120, 170, 230).mul(fl * (.35 + cl)))
			f.raw(x, y, mSky, col)
			f.emit[y][x] = col
		}
	}
	for _, s := range c.stars {
		x, y := int(s[0]-f.camX*.05), int(s[1])
		if x >= 0 && x < SceneW && c.fbm((s[0]+f.t*1.2)*.07, s[1]*.16) < .5 {
			v := .25 + .35*math.Abs(math.Sin(f.t*1.3+s[2]))
			f.emit[y][x] = f.emit[y][x].add(hex(180, 220, 255).mul(v))
		}
	}
	// lightning bolt, jagged, for a few frames after each strong flash
	for k, fla := range flashes {
		if d := f.t - fla[0]; d >= 0 && d < .16 && fla[1] > .4 {
			x, y := 52+12*hash(c.seed, k)-f.camX*.15, 0.0
			for y < 26 {
				nx := x + (hash(c.seed, k*31+int(y))-.5)*5
				f.line(x, y, nx, y+3, 1, mEmit, hex(230, 245, 255))
				x, y = nx, y+3
			}
		}
	}
}

func (c *Cutscene) mountains(f *frame) {
	for x := 0; x < SceneW; x++ {
		wx := float64(x) + f.camX*.35
		h := 9 + 5*c.fbm(wx*.05, 3) + 4*math.Abs(math.Sin(wx*.09))
		for y := int(groundY - h); y < int(groundY); y++ {
			d := (float64(y) - (groundY - h)) / h
			col := hex(18, 26, 38).lerp(hex(8, 12, 20), d)
			if d < .1 {
				col = hex(44, 58, 78).mul(.8 + .4*c.noise(wx*.4, 9)) // snow on the ridges
			}
			f.raw(x, y, mMount, col)
		}
	}
}

func (c *Cutscene) ground(f *frame) {
	for y := int(groundY); y < pxH; y++ {
		for x := 0; x < SceneW; x++ {
			n := c.noise((float64(x)+f.camX)*.3, float64(y)*.8)
			f.raw(x, y, mGround, hex(40, 54, 72).lerp(hex(24, 32, 46), float64(y-int(groundY))/6).mul(.85+.3*n))
		}
	}
}

// citadel draws the fortress: dark stone, sharp spires, runes that glow ice blue.
func (c *Cutscene) citadel(f *frame) {
	rise := smooth(tRise, 6, f.t)
	if rise <= 0 {
		return
	}
	ox := 3 - f.camX
	oy := (1 - rise) * 34
	stone := func(x0, x1 float64) func(int, int) vec {
		return func(x, y int) vec {
			d := (float64(x) - x0) / (x1 - x0)
			base := hex(30, 40, 56).lerp(hex(10, 14, 22), d) // moon from the left: depth shading
			if (y+int(oy))%3 == 0 && (x+(y/3)*2)%5 == 0 {
				base = base.mul(.75) // masonry joints
			}
			return base
		}
	}
	clip := func(pts [][2]float64) [][2]float64 {
		out := make([][2]float64, len(pts))
		for i, p := range pts {
			out[i] = [2]float64{p[0] + ox, math.Min(groundY, p[1]+oy)}
		}
		return out
	}
	tower := func(x0, x1, top, spire float64) {
		f.poly(clip([][2]float64{{x0, top}, {x1, top}, {x1, groundY}, {x0, groundY}}), mWall, stone(x0+ox, x1+ox))
		for mx := x0; mx < x1-.5; mx += 2 { // crenellations
			f.poly(clip([][2]float64{{mx, top - 1.5}, {mx + 1, top - 1.5}, {mx + 1, top}, {mx, top}}), mWall, stone(x0+ox, x1+ox))
		}
		mid := (x0 + x1) / 2
		f.poly(clip([][2]float64{{x0 + .5, top - 1.5}, {mid, top - 1.5 - spire}, {x1 - .5, top - 1.5}}), mWall, stone(x0+ox, x1+ox))
		f.poly(clip([][2]float64{{mid - .3, top - 1.5 - spire*.6}, {mid, top - 1.5 - spire}, {mid + .6, top - 1.5 - spire*.6}}), mWall, flat(hex(60, 78, 100)))
	}
	f.poly(clip([][2]float64{{6, 25}, {40, 25}, {40, groundY}, {6, groundY}}), mWall, stone(6+ox, 40+ox))
	for mx := 6.0; mx < 40; mx += 2.5 {
		f.poly(clip([][2]float64{{mx, 23.5}, {mx + 1.2, 23.5}, {mx + 1.2, 25}, {mx, 25}}), mWall, stone(6+ox, 40+ox))
	}
	tower(2, 9, 17, 9)
	tower(36, 43, 18, 8)
	if !(c.Ending == EndBreached && f.t > tResolve+2.2) {
		tower(17, 29, 13, 10)
	} else { // the keep's spire has fallen; a jagged stump remains
		f.poly(clip([][2]float64{{17, 13}, {29, 13}, {29, groundY}, {17, groundY}}), mWall, stone(17+ox, 29+ox))
		f.poly(clip([][2]float64{{17.5, 11.5}, {20, 8}, {22, 10.5}, {24.5, 7.5}, {28.5, 11.5}}), mWall, stone(17+ox, 29+ox))
	}
	// gate with the treasure glowing inside
	f.poly(clip([][2]float64{{20, 30}, {26, 30}, {26, groundY}, {20, groundY}}), mWall, flat(hex(3, 5, 9)))
	f.disc(23+ox, 30+oy, 3, mWall, hex(3, 5, 9))
	glow := .6 + .4*math.Sin(f.t*2.2)
	f.poly(clip([][2]float64{{21, 34.5}, {25, 34.5}, {25, 37.5}, {21, 37.5}}), mEmit, flat(hex(90, 190, 255).mul(glow)))
	f.poly(clip([][2]float64{{21.5, 35.2}, {24.5, 35.2}, {24.5, 35.8}, {21.5, 35.8}}), mEmit, flat(hex(230, 250, 255).mul(glow)))
	// window slits flicker; runes on the keep ignite one by one and pulse
	for k, w := range [][2]float64{{5, 21}, {39, 22}, {22, 15}, {25, 15}} {
		fl := .5 + .5*c.noise(f.t*3, float64(k)*10)
		f.poly(clip([][2]float64{{w[0], w[1]}, {w[0] + 1, w[1]}, {w[0] + 1, w[1] + 2.5}, {w[0], w[1] + 2.5}}), mEmit, flat(hex(80, 170, 240).mul(.4+.6*fl*rise)))
	}
	runes := [][][2]float64{
		{{9, 28}, {11, 28}, {10, 31}}, {{13, 31}, {14, 27}, {15, 31}}, {{31, 27}, {33, 29}, {31, 31}}, {{35, 31}, {36, 27}, {37, 29}},
	}
	for k, r := range runes {
		on := smooth(tRunes+float64(k)*.7, tRunes+float64(k)*.7+.4, f.t)
		if on <= 0 {
			continue
		}
		v := on * (.55 + .45*math.Sin(f.t*2+float64(k)))
		col := hex(110, 210, 255).mul(v)
		f.line(r[0][0]+ox, r[0][1]+oy, r[1][0]+ox, r[1][1]+oy, .8, mEmit, col)
		f.line(r[1][0]+ox, r[1][1]+oy, r[2][0]+ox, r[2][1]+oy, .8, mEmit, col)
	}
}

// ---------------------------------------------------------------- dragon

// wing angle keyframes over one slow beat: anticipation lift, held top, a fast heavy
// downstroke, overshoot (follow-through) and a slow recovery.
var wingKeys = [][2]float64{{0, .9}, {.18, 1.25}, {.38, 1.55}, {.52, 1.5}, {.64, -.55}, {.72, -.8}, {.86, -.2}, {1, .9}}

const wingBeat = 1.9 // seconds, slow and heavy

func wingAngle(p float64) float64 {
	p = math.Floor(p*8) / 8 // 8 held poses, like hand-drawn frames
	for k := 1; k < len(wingKeys); k++ {
		a, b := wingKeys[k-1], wingKeys[k]
		if p <= b[0] {
			u := (p - a[0]) / (b[0] - a[0])
			return a[1] + (b[1]-a[1])*u
		}
	}
	return wingKeys[0][1]
}

// WingFrames is the number of distinct wing poses.
const WingFrames = 8

func (c *Cutscene) dragonPos(t float64) (float64, float64, bool) {
	if t < tStrike-.4 {
		return 0, 0, false
	}
	p := math.Mod(t/wingBeat, 1)
	bob := 1.2 * math.Sin(2*math.Pi*(p-.6)) // lift on the downstroke
	x := 108 - 51*smooth(tStrike-.4, tStrike+3.4, t)
	y := 15 + bob
	x += 2 * math.Sin(t*.7)
	x += 21 * smooth(tKnight+.8, tKnight+2.8, t) // wheels to face the warden
	if t > tResolve {
		switch c.Ending {
		case EndSurvived:
			q := smooth(tResolve+.6, tResolve+4.5, t)
			x += 2*math.Sin(q*math.Pi)*-3 + 60*q*q // recoils, then flees
			y -= 12 * q
		case EndBreached:
			q := smooth(tResolve+3, tResolve+6.5, t)
			x += 58 * q * q
			y -= 10 * q
		default:
			a := (t - tResolve) * .7
			x = 50 + 26*math.Cos(a)
			y = 11 + 4*math.Sin(a)
		}
	}
	return x, y, true
}

func (c *Cutscene) breathing(t float64) bool {
	for _, b := range c.bursts() {
		if t >= b.from-.4 && t < b.from+fireLen {
			return true
		}
	}
	return false
}

// dragon draws the beast facing left with its body centre at (x, y). Coordinates are
// in dragon units, scaled by dragonScale.
const dragonScale = 1.15

func (c *Cutscene) dragon(f *frame, x, y float64) {
	t := f.t
	k := dragonScale
	p := math.Mod(t/wingBeat, 1)
	th := wingAngle(p)
	jaw := 0.0
	if c.breathing(t) {
		jaw = 1
	}
	body := hex(3, 4, 8)
	W := func(q [2]float64) (float64, float64) { return x + q[0]*k, y + q[1]*k }
	P := func(pts ...[2]float64) [][2]float64 {
		out := make([][2]float64, len(pts))
		for i, q := range pts {
			out[i][0], out[i][1] = W(q)
		}
		return out
	}
	L := func(a, b [2]float64, w float64, col vec) {
		ax, ay := W(a)
		bx, by := W(b)
		f.line(ax, ay, bx, by, w*k, mDragon, col)
	}
	wing := func(sx, sy, th float64, dark float64) {
		ex, ey := sx+9*math.Cos(th), sy-9*math.Sin(th)
		hx, hy := ex+9*math.Cos(th-.35), ey-9*math.Sin(th-.35)
		pts := [][2]float64{{sx, sy}, {ex, ey}, {hx, hy}}
		for j := 1; j <= 3; j++ { // finger tips with jagged, torn membrane between them
			a := th - .35 - float64(j)*.5
			l := 13 - float64(j)*1.8
			fx, fy := ex+l*math.Cos(a), ey-l*math.Sin(a)
			last := pts[len(pts)-1]
			mx, my := (last[0]+fx)/2, (last[1]+fy)/2
			pts = append(pts, [2]float64{mx + (ex-mx)*.3, my + (ey-my)*.3}, [2]float64{fx, fy})
		}
		pts = append(pts, [2]float64{sx + 8, sy + 2})
		f.poly(P(pts...), mDragon, flat(body.mul(dark)))
		L([2]float64{sx, sy}, [2]float64{ex, ey}, 1.1, body)
		L([2]float64{ex, ey}, [2]float64{hx, hy}, .8, body)
		f.poly(P([2]float64{ex - .6, ey}, [2]float64{ex - 1.8, ey - 2.2}, [2]float64{ex + .6, ey - .4}), mDragon, flat(body))
	}
	wing(3, -3.5, th+.15, .7) // far wing, in shadow
	// tail: segments with a sway that lags along its length (follow-through)
	tp := [2]float64{11, .5}
	for j := 0; j < 12; j++ {
		a := .2 + .45*math.Sin(t*1.8-float64(j)*.45)
		np := [2]float64{tp[0] + 2*math.Cos(a), tp[1] + 2*math.Sin(a)}
		L(tp, np, 2.6-float64(j)*.18, body)
		tp = np
	}
	f.poly(P([2]float64{tp[0] - 1, tp[1] - 1.5}, [2]float64{tp[0] + 3.5, tp[1]}, [2]float64{tp[0] - 1, tp[1] + 1.5}), mDragon, flat(body))
	// torso and spines along the back
	f.poly(P([2]float64{-6, -2}, [2]float64{-1, -4.5}, [2]float64{7, -3.5}, [2]float64{12, -.5}, [2]float64{8, 3}, [2]float64{0, 4}, [2]float64{-6, 2.5}), mDragon, flat(body))
	for j := 0; j < 5; j++ {
		sx := -3 + float64(j)*3
		f.poly(P([2]float64{sx, -4}, [2]float64{sx + .8, -6.3}, [2]float64{sx + 1.6, -3.8}), mDragon, flat(body))
	}
	// legs with claws, tucked in flight
	L([2]float64{6, 2}, [2]float64{7.5, 6}, 1.8, body)
	L([2]float64{-3, 2.5}, [2]float64{-4.5, 6}, 1.6, body)
	f.poly(P([2]float64{6, 6}, [2]float64{9, 6.5}, [2]float64{6.5, 7.2}), mDragon, flat(body))
	f.poly(P([2]float64{-5.5, 5.5}, [2]float64{-2.5, 6.4}, [2]float64{-5, 7}), mDragon, flat(body))
	// neck and an angular head with swept-back horns
	n := [2]float64{-6, -1}
	for j := 0; j < 6; j++ {
		mx, my := neckStep(n[0], n[1], j, t)
		L(n, [2]float64{mx, my}, 3.2-float64(j)*.25, body)
		n = [2]float64{mx, my}
	}
	H := func(pts ...[2]float64) [][2]float64 {
		for i := range pts {
			pts[i] = [2]float64{n[0] + pts[i][0], n[1] + pts[i][1]}
		}
		return P(pts...)
	}
	f.poly(H([2]float64{1.5, -2}, [2]float64{-3, -2.2}, [2]float64{-7.5, -.6}, [2]float64{-6.5, .3}, [2]float64{-2, .6}, [2]float64{1.5, 1.2}), mDragon, flat(body))
	f.poly(H([2]float64{0, .4}, [2]float64{-6.5, .5 + jaw*2.6}, [2]float64{-5.5, 1.2 + jaw*2.6}, [2]float64{.8, 1.8}), mDragon, flat(body)) // jaw
	f.poly(H([2]float64{0, -1.8}, [2]float64{5.5, -5.5}, [2]float64{1.6, -1.2}), mDragon, flat(body))                                       // horn
	f.poly(H([2]float64{-1.5, -2}, [2]float64{2.5, -4.6}, [2]float64{-.2, -1.6}), mDragon, flat(body))
	wing(-1, -3.5, th, 1) // near wing over the body
	// the eye burns ice blue
	ex, ey := W([2]float64{n[0] - 3.2, n[1] - 1})
	f.put(int(ex), int(ey), mEmit, hex(220, 250, 255))
	f.put(int(ex)+1, int(ey), mEmit, hex(90, 190, 255))
}

func (c *Cutscene) eye(t float64) (float64, float64) {
	x, y, _ := c.dragonPos(t)
	n := [2]float64{-6, -1}
	for j := 0; j < 6; j++ {
		n[0], n[1] = neckStep(n[0], n[1], j, t)
	}
	return x + (n[0]-3.2)*dragonScale, y + (n[1]-1)*dragonScale
}

func neckStep(x, y float64, k int, t float64) (float64, float64) {
	a := 2.6 + .12*float64(k) + .1*math.Sin(t*1.2)
	return x + 1.6*math.Cos(a), y - 1.6*math.Sin(a)
}

// mouth is where the breath leaves the jaw, in world pixels.
func (c *Cutscene) mouth(t float64) (float64, float64) {
	x, y, _ := c.dragonPos(t)
	n := [2]float64{-6, -1}
	for j := 0; j < 6; j++ {
		n[0], n[1] = neckStep(n[0], n[1], j, t)
	}
	return x + (n[0]-7)*dragonScale, y + (n[1]+.9)*dragonScale
}

// ---------------------------------------------------------------- fire

type burst struct{ from, tx, ty, lift float64 }

func (c *Cutscene) bursts() []burst {
	b := []burst{{tFire1, 25, 16, 0}, {tFire2, 47, 31, 0}}
	if c.Ending == EndBreached {
		b = append(b, burst{tResolve + .9, 34, 22, -.04}) // through the broken ward onto the keep
	}
	return b
}

// stepFire advances the heat field to frame i. Frames are normally drawn in order; an
// earlier frame restarts the simulation from zero, so results never depend on history.
func (c *Cutscene) stepFire(i int) {
	if i < c.simAt {
		c.heat, c.simAt = [pxH][SceneW]float64{}, -1
	}
	for c.simAt < i {
		c.simAt++
		c.fireTick(c.simAt)
	}
}

func (c *Cutscene) fireTick(i int) {
	t := float64(i) / FPS
	cam := 7 * smooth(0, cutSecs, t)
	vx, vy := 0.0, -.25
	for _, b := range c.bursts() {
		if t < b.from || t > b.from+fireLen {
			continue
		}
		mx, my := c.mouth(t)
		mx -= cam
		dx, dy := b.tx-cam-mx, b.ty-my
		d := math.Hypot(dx, dy)
		dx, dy = dx/d, dy/d
		sp := 2.1
		if b.from == tFire2 {
			sp = 3.2 // the breath at the ward is a hard, fast stream
		}
		vx, vy = dx*sp, dy*sp-.15
		vy += b.lift * (t - b.from) * 25 // the breath droops and rises along an arc
		k := 0
		for s := 0.0; s < 3; s += .5 { // inject a short, hot, slightly fanned jet
			for w := -1.0; w <= 1; w++ {
				x := int(mx + dx*s - dy*w*s*.55)
				y := int(my + dy*s + dx*w*s*.55)
				if x >= 0 && x < SceneW && y >= 0 && y < pxH {
					c.heat[y][x] = math.Min(1.2, c.heat[y][x]+.9*(.7+.3*hash(c.seed, i*131+k)))
				}
				k++
			}
		}
	}
	wardOn := c.wardOn(t)
	wp := c.wizardPose(t)
	wcx, wcy := wp.x-cam, wp.y
	gap := c.Ending == EndBreached && t > tResolve+.9
	// carry heat along the wind with jitter (semi-Lagrangian), diffuse and cool
	for y := 0; y < pxH; y++ {
		for x := 0; x < SceneW; x++ {
			j := hash(c.seed, i*7919+y*101+x)
			ux, uy := vx, vy
			if wardOn > 0 { // the ward: no heat inside, and flow near it slides around it
				dx, dy := float64(x)-wcx, (float64(y)-wcy)*1.15
				d := math.Hypot(dx, dy)
				R := wardR * wardOn
				ang := math.Atan2(-dy, dx)
				open := gap && ang > .55 && ang < 1.15
				if d < R && float64(y) <= wcy && !open {
					c.tmp[y][x] = 0
					continue
				}
				if d < R+5 && !open {
					nx, ny := dx/d, dy/d
					if dot := ux*nx + uy*ny; dot < 0 {
						ux, uy = ux-nx*dot*1.6, uy-ny*dot*1.6
					}
				}
			}
			sx := float64(x) - ux + (j-.5)*1.6
			sy := float64(y) - uy + (hash(c.seed, i*31+y*7+x*13)-.5)*1.6
			c.tmp[y][x] = c.sample(sx, sy) * (.885 + .06*j)
		}
	}
	for y := 0; y < pxH; y++ {
		for x := 0; x < SceneW; x++ {
			s := c.tmp[y][x] * 4
			n := 4.0
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				if xx, yy := x+d[0], y+d[1]; xx >= 0 && xx < SceneW && yy >= 0 && yy < pxH {
					s += c.tmp[yy][xx]
					n++
				}
			}
			c.heat[y][x] = s / n
			if c.heat[y][x] < .02 {
				c.heat[y][x] = 0
			}
		}
	}
}

func (c *Cutscene) sample(x, y float64) float64 {
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	g := func(x, y int) float64 {
		if x < 0 || x >= SceneW || y < 0 || y >= pxH {
			return 0
		}
		return c.heat[y][x]
	}
	a := g(x0, y0) + (g(x0+1, y0)-g(x0, y0))*fx
	b := g(x0, y0+1) + (g(x0+1, y0+1)-g(x0, y0+1))*fx
	return a + (b-a)*fy
}

// plasma maps heat to cold fire: deep blue, cyan, ice white.
func plasma(h float64) vec {
	switch {
	case h < .25:
		return hex(10, 30, 110).mul(h / .25)
	case h < .5:
		return hex(10, 30, 110).lerp(hex(40, 150, 255), (h-.25)/.25)
	case h < .8:
		return hex(40, 150, 255).lerp(hex(190, 235, 255), (h-.5)/.3)
	}
	return hex(190, 235, 255).lerp(hex(255, 255, 255), math.Min(1, (h-.8)/.3))
}

// ---------------------------------------------------------------- knight

// The warden: sapper itself, a tall hooded figure. His silhouette is one dominant
// triangle (hood point to robe hem) with the staff as a strong vertical; the ice
// crystal and his eyes are the brightest values on him, so they are the focal point.
// He is drawn in his own units (21 tall at the hood tip) and scaled by knightK so the
// gate is about 1.5 times his height.

type pose struct {
	x, y   float64 // feet, world pixels before zoom
	staffA float64 // staff angle from vertical, positive leans toward the dragon
	lift   float64 // hand height above rest, in his units
	slam   float64 // 0..1, the staff driven into the snow
	march  float64
}

const (
	tSlam   = tFire2 - .45 // he answers the dragon's inhale
	tThrust = tResolve + .3
)

func (c *Cutscene) wizardPose(t float64) pose {
	x, y := c.knightAt(t)
	p := pose{x: x, y: y, march: smooth(tKnight, tKnight+2.6, t)}
	// raising the citadel: the staff lifts high while the walls rise, then pulses with
	// each rune that ignites
	p.lift = 5 * smooth(tRise-.6, tRise, t) * (1 - smooth(6, 7, t))
	for k := 0; k < 4; k++ {
		at := tRunes + float64(k)*.7
		if d := t - at; d > -.15 && d < .35 {
			p.lift = math.Max(p.lift, 2*math.Sin((d+.15)/.5*math.Pi))
		}
	}
	// the slam: anticipation lift, a fast drop, a held hit
	if d := t - (tSlam - .45); d > 0 && d < .45 {
		p.lift = 4 * math.Sin(d/.45*math.Pi/2)
	}
	if d := t - tSlam; d >= 0 {
		p.slam = smooth(0, .08, d) * (1 - smooth(tResolve, tResolve+.3, t))
		p.lift = 4 * (1 - smooth(0, .08, d))
	}
	if c.Ending == EndSurvived { // the thrust that drives the dragon off
		th := smooth(tThrust-.25, tThrust, t) * (1 - smooth(tThrust+1.4, tThrust+2.2, t))
		p.staffA = .9 * th
		p.lift = math.Max(p.lift, 3*th)
	}
	return p
}

// crystal returns the staff tip in world pixels before zoom.
func (c *Cutscene) crystal(t, camX float64) (float64, float64) {
	p := c.wizardPose(t)
	k := knightK
	hx, hy := 3.2, -11-p.lift+p.slam*2.5
	return p.x - camX + (hx+12*math.Sin(p.staffA))*k, p.y + (hy-12*math.Cos(p.staffA))*k
}

func (c *Cutscene) wardOn(t float64) float64 {
	return smooth(tSlam+.05, tSlam+.45, t) * (1 - smooth(tResolve+4.5, tResolve+5.5, t))
}

const wardR = 9.0

func (c *Cutscene) knight(f *frame) {
	t := f.t
	if !c.wizardVisible(t) {
		return
	}
	p := c.wizardPose(t)
	k := knightK
	x0, y0 := p.x-f.camX, p.y
	K := func(dx, dy float64) (float64, float64) { return x0 + dx*k, y0 + dy*k }
	L := func(ax, ay, bx, by, w float64, m mat, col vec) {
		x1, y1 := K(ax, ay)
		x2, y2 := K(bx, by)
		f.line(x1, y1, x2, y2, math.Max(.5, w*k), m, col)
	}
	P := func(m mat, col vec, pts ...[2]float64) {
		for i := range pts {
			pts[i][0], pts[i][1] = K(pts[i][0], pts[i][1])
		}
		f.poly(pts, m, flat(col))
	}
	walking := p.march > 0 && p.march < 1
	sway := .7*math.Sin(t*2.1) + 1.5*math.Sin(t*7)*b2f(walking) // hem follow-through
	cloak := hex(16, 22, 36)
	robe := hex(44, 56, 80)
	// cloak behind him, a long sharp triangle trailing in the wind
	P(mCloth, cloak, [2]float64{-2.4, -16}, [2]float64{1, -16}, [2]float64{-4.5 - sway, 0}, [2]float64{-8 - sway*1.5, -.5})
	// robe: one tall triangle from the shoulders to a wide hem that sways
	P(mWarden, robe, [2]float64{-2.2, -15.5}, [2]float64{2.2, -15.5}, [2]float64{4.2 + sway*.4, -.2}, [2]float64{1, .2}, [2]float64{-4.4 + sway*.6, -.1})
	// hood: a pointed cowl, its tip bent back, a black opening with two burning eyes
	P(mWarden, robe, [2]float64{-2.8, -15}, [2]float64{2.8, -15}, [2]float64{2.4, -19}, [2]float64{-.2, -22.5 - .3*math.Sin(t*2)}, [2]float64{-2.6, -19})
	P(mWarden, hex(2, 3, 6), [2]float64{-.6, -18.8}, [2]float64{2.6, -18.6}, [2]float64{2.2, -16}, [2]float64{.2, -15.8})
	eye := hex(170, 240, 255).mul(.75 + .25*math.Sin(t*2.7))
	ex, ey := K(1.4, -17.6)
	f.put(int(ex), int(ey), mEmit, eye)
	// arm and staff: the line of action runs from the hem through the arm to the crystal
	hx, hy := 3.2, -11-p.lift+p.slam*2.5
	L(.8, -14, hx, hy, 1.6, mWarden, robe)
	tx, ty := hx+12*math.Sin(p.staffA), hy-12*math.Cos(p.staffA)
	bx, by := hx-9*math.Sin(p.staffA), hy+9*math.Cos(p.staffA)
	if by > 0 {
		by = 0
	}
	L(bx, by, tx, ty, .9, mWarden, hex(60, 70, 86))
	glow := .7 + .3*math.Sin(t*3) + .6*c.wardOn(t)*math.Exp(-math.Max(0, t-tSlam)*3)
	cx, cy := K(tx, ty)
	f.disc(cx, cy-.3, .9, mEmit, hex(170, 235, 255).mul(glow))
	f.put(int(cx), int(cy-1), mEmit, hex(240, 252, 255))

	// rune circle on the snow: flares at the slam, then holds
	if on := smooth(tSlam, tSlam+.12, t); on > 0 && c.wardOn(t) > 0 {
		flare := 1 + 2*math.Exp(-(t-tSlam)*5)
		for a := 0.0; a < 2*math.Pi; a += .05 {
			rx, ry := f.tx(x0+10*math.Cos(a), y0+1.2+1.6*math.Sin(a))
			f.raw(int(rx), int(ry), mEmit, hex(80, 180, 255).mul(.35*flare*c.wardOn(t))) // a hairline, even zoomed
		}
		for j := 0; j < 10; j++ { // glyph ticks around the ring
			a := float64(j)/10*2*math.Pi + t*.3
			rx, ry := f.tx(x0+8*math.Cos(a), y0+1.2+1.1*math.Sin(a))
			f.raw(int(rx), int(ry), mEmit, hex(170, 235, 255).mul(.5*flare*c.wardOn(t)))
		}
	}
}

// inhale pulls motes of cold light into the jaw before each breath: the dragon's tell.
func (c *Cutscene) inhale(f *frame) {
	for bi, b := range c.bursts() {
		d := f.t - (b.from - .7)
		if d < 0 || d > .7 {
			continue
		}
		mx, my := c.mouth(f.t)
		mx -= f.camX
		for j := 0; j < 36; j++ {
			u := math.Mod(hash(c.seed, bi*100+j)+d*1.6, 1)
			a := hash(c.seed, bi*100+j+50) * 2 * math.Pi
			r := 9 * (1 - u)
			f.put(int(mx+r*math.Cos(a)), int(my+r*math.Sin(a)*.7), mEmit, hex(90, 190, 255).mul(.3+.6*u))
		}
	}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// ward draws the ice dome as additive light: a bright rim, a faint hexagonal lattice,
// ripples where the breath strikes and, when the drill fails, a fracture.
func (c *Cutscene) ward(f *frame, out *[pxH][SceneW]vec) {
	on := c.wardOn(f.t)
	if on <= 0 {
		return
	}
	p := c.wizardPose(f.t)
	cx, cy := p.x-f.camX, p.y
	gap := c.Ending == EndBreached && f.t > tResolve+.9
	for y := 0; y < pxH; y++ {
		for x := 0; x < SceneW; x++ {
			wx, wy := f.inv(float64(x)+.5, float64(y)+.5)
			if wy > cy {
				continue
			}
			dx, dy := wx-cx, wy-cy
			r := math.Hypot(dx, dy*1.15) / (wardR * on)
			if r > 1.08 {
				continue
			}
			ang := math.Atan2(-dy, dx)
			if gap && ang > .55 && ang < 1.15 {
				continue // the ward has broken here
			}
			v := 0.0
			if r > .9 {
				v = .55 * (1 - math.Abs(r-1)/.18)
			}
			// hexagonal lattice
			hx, hy := wx*.55, wy*.55/0.866
			if math.Abs(math.Mod(hx+math.Floor(hy)*.5+100, 1)-.5) > .42 || math.Abs(math.Mod(hy+100, 1)-.5) > .44 {
				v += .1
			}
			// impact ripples: brighten where heat touches the ward
			v += 1.4 * c.heatAt(wx, wy) * (1 - r*.3)
			out[y][x] = out[y][x].add(hex(110, 200, 255).mul(v * on))
		}
	}
}

func (c *Cutscene) heatAt(x, y float64) float64 {
	m := 0.0
	for _, d := range [][2]float64{{0, 0}, {2, 0}, {-2, 0}, {0, 2}, {0, -2}} {
		m = math.Max(m, c.sample(x+d[0], y+d[1]))
	}
	return m
}

// frost is the warden's answer in the survived ending: a cone of ice light from the
// crystal toward the dragon.
func (c *Cutscene) frost(f *frame) {
	if c.Ending != EndSurvived {
		return
	}
	a := smooth(tThrust, tThrust+.15, f.t) * (1 - smooth(tThrust+1, tThrust+1.6, f.t))
	if a <= 0 {
		return
	}
	sx, sy := c.crystal(f.t, f.camX)
	tx, ty := c.mouth(f.t)
	tx -= f.camX
	for j := 0; j < 240; j++ {
		u := math.Mod(hash(c.seed, j)+f.t*1.8, 1)
		spread := (hash(c.seed, j+900) - .5) * 16 * u
		px := sx + (tx-sx)*u - (ty-sy)/40*spread
		py := sy + (ty-sy)*u + (tx-sx)/40*spread
		qx, qy := f.tx(px, py)
		f.raw(int(qx), int(qy), mEmit, hex(170, 230, 255).mul(.7*a*(1-.6*u)))
	}
}

// ---------------------------------------------------------------- endings

func (c *Cutscene) endingScene(f *frame) {
	t := f.t
	ox := 3 - f.camX
	switch c.Ending {
	case EndSurvived:
		up := smooth(tResolve+2, tResolve+4.5, t)
		if up <= 0 {
			return
		}
		px, top := ox+5.5, -1+7*(1-up) // the banner climbs the west tower's spire
		f.line(px, -1, px, 7, .6, mWarden, hex(140, 160, 190))
		// cloth: each column follows a travelling wave, amplitude growing toward the
		// free edge, with a little sag and slope shading
		for i := 0; i < 14; i++ {
			u := float64(i) / 13
			w := 1.6 * u * math.Sin(float64(i)*.55-t*5.5)
			slope := math.Cos(float64(i)*.55 - t*5.5)
			for j := 0; j < 6; j++ {
				col := hex(150, 220, 255).mul(.55 + .35*slope + .15*(1-float64(j)/6))
				if j == 2 || j == 3 {
					if i > 3 && i < 10 {
						col = hex(20, 40, 70) // the emblem stripe
					}
				}
				f.put(int(px+1+float64(i)), int(top+float64(j)+w+.4*u*u*2), mCloth, col)
			}
		}
	case EndBreached:
		g := smooth(tResolve+1.7, tResolve+2.6, t)
		pts := [][2]float64{{23, 8}, {22, 10}, {24, 12}, {22.5, 15}, {24.5, 19}, {23, 23}, {25, 27}}
		n := int(g * float64(len(pts)-1))
		for k := 0; k < n; k++ {
			f.line(ox+pts[k][0], pts[k][1], ox+pts[k+1][0], pts[k+1][1], .7, mEmit, vec{.92, .08, .16}.mul(.75+.25*math.Sin(t*8)))
		}
		if t > tResolve+2.2 { // smoke billows from the broken keep: drifting noise
			for y := 0; y < 14; y++ {
				for x := 14; x < 40; x++ {
					wx := float64(x) - ox
					d := c.fbm(wx*.18+t*.25, float64(y)*.2+t*.9)
					fall := math.Max(0, 1-math.Abs(float64(x)-ox-23-float64(13-y)*.6)/(3+float64(13-y)*.5))
					a := smooth(.35, .8, d) * fall * smooth(tResolve+2.2, tResolve+3.5, t)
					if a > .05 && f.mt[y][x] == mSky {
						f.emit[y][x] = f.emit[y][x].lerp(hex(46, 52, 62), a)
					}
				}
			}
		}
	}
}

func (c *Cutscene) snow(f *frame) {
	k := 1 - smooth(tSilence-.5, tSilence+.5, f.t) + smooth(tStrike, tStrike+2, f.t)
	if k > 1 {
		k = 1
	}
	for i, s := range c.flakes {
		if hash(c.seed, i) > k {
			continue
		}
		speed := s[2] * (4 + 4*s[3])
		y := math.Mod(s[1]+speed*f.t, pxH)
		x := math.Mod(s[0]-f.t*(2+2*s[3])-f.camX*(.3+.3*s[3])+SceneW*20, SceneW)
		xi, yi := int(x), int(y)
		if m := f.mt[yi][xi]; m == mSky || m == mMount || m == mGround {
			f.raw(xi, yi, mEmit, f.emit[yi][xi].add(f.pig[yi][xi]).lerp(hex(190, 210, 235), .08+.12*s[3]))
		}
	}
}

// ---------------------------------------------------------------- light and compose

func (c *Cutscene) lights(f *frame, dx, dy float64, dragon bool) {
	type src struct {
		x, y, r float64
		col     vec
	}
	var ls []src
	// the fire lights its surroundings: one light at the heat centroid
	sum, cx, cy := 0.0, 0.0, 0.0
	for y := 0; y < pxH; y++ {
		for x := 0; x < SceneW; x++ {
			if h := c.heat[y][x]; h > 0 {
				sum += h
				cx += h * float64(x)
				cy += h * float64(y)
			}
		}
	}
	if sum > .5 {
		ls = append(ls, src{cx / sum, cy / sum, 26, hex(90, 170, 255).mul(math.Min(2.2, sum/40))})
	}
	if dragon {
		ex, ey := c.eye(f.t)
		ls = append(ls, src{ex - f.camX, ey, 6, hex(90, 180, 255).mul(.6)})
	}
	if c.Ending == EndBreached && f.t > tResolve+1.7 {
		ls = append(ls, src{26 - f.camX, 16, 9, vec{.6, .05, .1}.mul(.6)})
	}
	if c.wizardVisible(f.t) {
		cx, cy := c.crystal(f.t, f.camX)
		ls = append(ls, src{cx, cy, 7, hex(110, 200, 255).mul(.5 + .9*c.wardOn(f.t))})
	}
	for i := range ls { // sources are in world pixels; light the zoomed screen
		ls[i].x, ls[i].y = f.tx(ls[i].x, ls[i].y)
		ls[i].r *= f.z
	}
	moon := hex(60, 80, 110)
	fl := c.flash(f.t)
	for y := 0; y < pxH; y++ {
		for x := 0; x < SceneW; x++ {
			l := moon.add(hex(160, 200, 255).mul(fl))
			for _, s := range ls {
				d2 := (float64(x)-s.x)*(float64(x)-s.x) + (float64(y)-s.y)*(float64(y)-s.y)
				if fo := 1 - math.Sqrt(d2)/s.r; fo > 0 { // Brogue-style radial fade
					l = l.add(s.col.mul(fo * fo))
				}
			}
			f.light[y][x] = l
		}
	}
}

func (c *Cutscene) compose(f *frame) *[CutH][SceneW]Cell {
	sx, sy := c.shake(f.t)
	fl := c.flash(f.t)
	var px [pxH][SceneW]vec
	for y := 0; y < pxH; y++ {
		for x := 0; x < SceneW; x++ {
			X, Y := x+sx, y+sy
			X, Y = max(0, min(SceneW-1, X)), max(0, min(pxH-1, Y))
			m := f.mt[Y][X]
			col := f.emit[Y][X]
			if m != mEmit && m != mSky {
				col = f.pig[Y][X].times(f.light[Y][X]).mul(2.6)
				// rim light: creatures get a cold edge wherever they meet open sky, so the
				// silhouette reads even against a dark storm; walls only on top
				open := func(dx, dy int) bool {
					xx, yy := X+dx, Y+dy
					if xx < 0 || xx >= SceneW || yy < 0 || yy >= pxH {
						return false
					}
					m := f.mt[yy][xx]
					return m == mSky || m == mMount
				}
				switch {
				case m == mDragon || m == mWarden:
					if open(0, -1) {
						col = col.add(hex(110, 190, 240).mul(.5 + .5*fl).add(f.light[Y][X].mul(.4)))
					} else if open(-1, 0) || open(1, 0) {
						col = col.add(hex(40, 70, 110).mul(.25 + .4*fl).add(f.light[Y][X].mul(.12)))
					}
				case m == mWall && open(0, -1):
					col = col.add(f.light[Y][X].mul(.5))
				}
			}
			wx, wy := f.inv(float64(X), float64(Y))
			if h := c.sample(wx, wy); h > .02 {
				col = col.add(plasma(h)) // additive: plasma glows over whatever it engulfs
			}
			px[y][x] = col
		}
	}
	c.ward(f, &px)
	out := &[CutH][SceneW]Cell{}
	for r := 0; r < SceneH; r++ {
		for x := 0; x < SceneW; x++ {
			out[r][x] = Cell{R: '▀', C: px[2*r][x].rgb(), BG: px[2*r+1][x].rgb(), HB: true}
		}
	}
	return out
}

// overlay writes text cells: the emblem, the verdict banner and the narration.
func (c *Cutscene) overlay(f *frame, out *[CutH][SceneW]Cell) {
	t := f.t
	glass := func(x, y int) RGB {
		b := out[y][x].BG
		return RGB{b.R / 3, b.G / 3, b.B / 3}
	}
	if t >= tEmblem { // the SAPPER emblem decrypts over the fortress
		w := Width(c.emblem)
		x0 := (SceneW - w) / 2
		noise := []rune(noiseChars)
		for y, l := range c.emblem {
			for x, r := range []rune(l) {
				if r == ' ' {
					continue
				}
				rev := float64(y)/float64(len(c.emblem))*.6 + hash(c.seed, y*131+x)*.4
				p := (t - tEmblem) / 2.2
				cx, cy := x0+x, 1+y
				switch {
				case p >= rev+.1:
					out[cy][cx] = Cell{R: r, C: cellColor(r, x, w), BG: glass(cx, cy), HB: true}
				case p >= rev-.15:
					out[cy][cx] = Cell{R: noise[int(hash(c.seed, y*977+x*13+int(t*FPS))*float64(len(noise)))], C: Ice(.6), BG: glass(cx, cy), HB: true}
				}
			}
		}
	}
	if t >= tBanner {
		var s string
		red := false
		switch c.Ending {
		case EndSurvived:
			s = "S U R V I V E D"
		case EndBreached:
			s, red = "B R E A C H E D", true
		default:
			s = "N O   D R I L L   Y E T"
		}
		full := []rune("▓▒░  " + s + "  ░▒▓")
		x0 := (SceneW - len(full)) / 2
		for k, r := range full {
			if float64(k) > (t-tBanner)*50 {
				break
			}
			sheen := math.Max(0, 1-math.Abs(float64(k)-math.Mod((t-tBanner)*22, float64(len(full))+16))/3)
			col := vec{1, 1, 1}.lerp(hex(150, 225, 255), .25).add(vec{1, 1, 1}.mul(sheen)).rgb()
			if red && r != '▓' && r != '▒' && r != '░' {
				col = Accent
			}
			out[12][x0+k] = Cell{R: r, C: col, BG: RGB{2, 4, 9}, HB: true}
		}
	}
	rule := Ice(.4)
	for x := 1; x < SceneW-1; x++ {
		out[SceneH][x] = Cell{R: '─', C: rule}
	}
	nar := c.narration()
	cur := nar[0]
	for _, n := range nar {
		if t >= n.at {
			cur = n
		}
	}
	budget := int((t - cur.at) * 32)
	for k, l := range cur.lines {
		r := []rune(l)
		if budget < len(r) {
			r = r[:max(budget, 0)]
		}
		budget -= len(r)
		for x, ch := range r {
			out[SceneH+2+k][2+x] = Cell{R: ch, C: Ice(.95)}
		}
	}
	for x, ch := range "any key skips" {
		out[CutH-1][2+x] = Cell{R: ch, C: Ice(.35)}
	}
}

const noiseChars = "█▓▒░▀▄#@$%&01<>/\\|=+"

type chapter struct {
	at    float64
	lines []string
}

// narration is terse and grave, but a child can follow it.
func (c *Cutscene) narration() []chapter {
	return []chapter{
		{0, []string{"You keep a box of treasures. Drawings. Photos. Things you cannot lose."}},
		{1.5, []string{"sapper, the warden, raises a fortress around it. Each night it hides", "a copy far away."}},
		{tSilence, []string{"But a copy no one tests is a promise no one keeps."}},
		{tStrike + .6, []string{"So each week, the warden summons the dragon."}},
		{tFire1, []string{"The fortress burns. sapper rebuilds it from the copy", "and checks every drawing, one by one."}},
		{tKnight + .5, []string{"The warden raises a ward. It knows the sound of a monster", "scribbling over everything at once."}},
		{tResolve + 1, wrap(c.Final, 74)},
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

func (c RGB) bg() string {
	return "\x1b[48;2;" + itoa(int(c.R)) + ";" + itoa(int(c.G)) + ";" + itoa(int(c.B)) + "m"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [3]byte
	i := 3
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Frame renders frame i as ANSI: home the cursor and redraw every row in one write,
// emitting a colour only when it changes.
func (c *Cutscene) Frame(i int) string {
	g := c.FrameCells(i)
	var b strings.Builder
	b.Grow(64 * 1024)
	b.WriteString("\x1b[H")
	for y := 0; y < CutH; y++ {
		var fg, bg RGB
		haveFG, haveBG := false, false
		for x := 0; x < SceneW; x++ {
			cl := g[y][x]
			if cl.HB {
				if !haveBG || cl.BG != bg {
					b.WriteString(cl.BG.bg())
					bg, haveBG = cl.BG, true
				}
			} else if haveBG {
				b.WriteString("\x1b[49m")
				haveBG = false
			}
			if cl.R == 0 || cl.R == ' ' {
				b.WriteByte(' ')
				continue
			}
			if !haveFG || cl.C != fg {
				b.WriteString(cl.C.fg())
				fg, haveFG = cl.C, true
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
