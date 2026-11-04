package art

import (
	"math"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestCutsceneDeterministic(t *testing.T) {
	a, b := NewCutscene(EndSurvived, "x.", 42), NewCutscene(EndSurvived, "x.", 42)
	for _, i := range []int{0, 100, 300, 500, 700, a.Frames() - 1} {
		if a.Frame(i) != b.Frame(i) {
			t.Fatalf("frame %d differs for the same seed", i)
		}
	}
	if NewCutscene(EndSurvived, "x.", 43).Frame(50) == a.Frame(50) {
		t.Error("a different seed gave the same sky")
	}
	if s := float64(a.Frames()) / FPS; s < 25 || s > 40 {
		t.Errorf("cutscene lasts %.0fs", s)
	}
}

func TestCutsceneBounds(t *testing.T) {
	for _, e := range []Ending{EndNone, EndSurvived, EndBreached} {
		c := NewCutscene(e, "Your treasure survived the dragon 2 days ago. 13 of 13 drawings came back perfect.", 7)
		for i := 0; i < c.Frames(); i++ {
			rows := c.Text(i)
			if len(rows) != CutH {
				t.Fatalf("frame %d has %d rows", i, len(rows))
			}
			for y, r := range rows {
				if n := utf8.RuneCountInString(r); n != SceneW {
					t.Fatalf("ending %d frame %d row %d is %d columns", e, i, y, n)
				}
			}
			f := c.Frame(i)
			if strings.Count(f, "\r\n") != CutH-1 || !strings.HasPrefix(f, "\x1b[H") {
				t.Fatalf("frame %d is not a full-screen redraw", i)
			}
		}
	}
}

func TestCutsceneEndings(t *testing.T) {
	red := Accent.fg()
	for e, want := range map[Ending]string{EndSurvived: "SURVIVED", EndBreached: "BREACHED", EndNone: "NO DRILL YET"} {
		c := NewCutscene(e, "The final line.", 1)
		last := c.Frames() - 1
		if !strings.Contains(strings.Join(c.Text(last), "\n"), want) {
			t.Errorf("ending %d: last frame lacks %q", e, want)
		}
		if !strings.Contains(strings.Join(c.Text(last), "\n"), "The final line.") {
			t.Errorf("ending %d: last frame lacks the real last line", e)
		}
		usesRed := false
		for i := 0; i < c.Frames(); i += 5 {
			if strings.Contains(c.Frame(i), red) {
				usesRed = true
				break
			}
		}
		if usesRed != (e == EndBreached) {
			t.Errorf("ending %d: red accent used = %v", e, usesRed)
		}
	}
}

func TestCutsceneRandomAccess(t *testing.T) {
	a, b := NewCutscene(EndBreached, "x.", 5), NewCutscene(EndBreached, "x.", 5)
	_ = a.Frame(800)
	if a.Frame(560) != b.Frame(560) {
		t.Fatal("frame depends on which frames were drawn before it")
	}
}

func TestWingPoses(t *testing.T) {
	seen := map[float64]bool{}
	for p := 0.0; p < 1; p += .01 {
		seen[wingAngle(p)] = true
	}
	if len(seen) < 6 {
		t.Errorf("only %d wing poses", len(seen))
	}
}

func TestCutsceneFrameTime(t *testing.T) {
	c := NewCutscene(EndSurvived, "x.", 3)
	start := time.Now()
	n := 200
	for i := 300; i < 300+n; i++ {
		_ = c.Frame(i)
	}
	if per := time.Since(start) / time.Duration(n); per > time.Second/FPS/2 {
		t.Errorf("%v per frame, budget %v", per, time.Second/FPS)
	}
}

func BenchmarkCutsceneFrame(b *testing.B) {
	c := NewCutscene(EndSurvived, "x.", 3)
	for i := 0; i < b.N; i++ {
		_ = c.Frame(300 + i%500)
	}
}

// The warden stands two thirds of the gate opening, with headroom under the arch even
// with his hood up, and the gate geometry the constants describe is the one drawn.
func TestWardenFitsGate(t *testing.T) {
	ratio := wardenH * knightK / gateOpen
	if ratio < .62 || ratio > .71 {
		t.Fatalf("warden is %.2f of the gate opening, want about 2/3", ratio)
	}
	c := NewCutscene(EndSurvived, "x.", 1)
	for i := 0; i < int(tKnight*FPS); i++ { // at the gate, before he walks out
		p := c.wizardPose(float64(i) / FPS)
		_, hood := p.at(0, -wardenH)
		if groundY-hood > gateOpen*.75 {
			t.Fatalf("frame %d: hood %.1f px above the ground, gate opening %.1f", i, groundY-hood, gateOpen)
		}
	}
}

// Spatial continuity: every layer goes through one camera, so where a world point lands
// on screen moves smoothly from frame to frame, through the zoom in and out, and the
// warden and the dragon never jump.
func TestNoTeleports(t *testing.T) {
	for _, e := range []Ending{EndNone, EndSurvived, EndBreached} {
		c := NewCutscene(e, "x.", 1)
		var last [3][2]float64
		for i := 0; i < c.Frames(); i++ {
			tt := float64(i) / FPS
			f := &frame{t: tt, camX: 7 * smooth(0, cutSecs, tt)}
			c.camera(f)
			p := c.wizardPose(tt)
			dx, dy, _ := c.dragonPos(tt)
			var now [3][2]float64
			now[0][0], now[0][1] = f.tx(p.x-f.camX, p.y)
			now[1][0], now[1][1] = f.tx(dx-f.camX, dy)
			now[2][0], now[2][1] = f.tx(23+3-f.camX, groundY) // the gate
			if i > 0 {
				for k, lim := range []float64{2.5, 4.5, 2.5} {
					if tt < tStrike+.2 && k == 1 {
						continue // the dragon is not there yet
					}
					if d := math.Hypot(now[k][0]-last[k][0], now[k][1]-last[k][1]); d > lim {
						t.Fatalf("ending %d frame %d: object %d jumped %.1f px", e, i, k, d)
					}
				}
			}
			last = now
		}
	}
}
