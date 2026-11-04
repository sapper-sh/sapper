package art

import (
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
	for e, want := range map[Ending]string{EndSurvived: "S U R V I V E D", EndBreached: "B R E A C H E D", EndNone: "N O   D R I L L"} {
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
