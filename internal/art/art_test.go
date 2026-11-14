package art

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestShimmerDeltaPreservesPrompt(t *testing.T) {
	lines := []string{"AB     C"}
	frame := ShimmerDelta(lines, 1, -1, 0)
	if !strings.HasPrefix(frame, "\x1b7\x1b[1;1H") || !strings.HasSuffix(frame, "A\x1b[0m\x1b8") {
		t.Fatalf("expected a cursor-preserving update to the first glyph: %q", frame)
	}
	if strings.ContainsAny(frame, "\r\n") || strings.Count(frame, "H") != 1 {
		t.Fatalf("unexpected output outside the visible glyph: %q", frame)
	}
	if ShimmerDelta(lines, 8, .5, .5) != "" {
		t.Fatal("unchanged frame produced output")
	}
}

func TestVariantWidths(t *testing.T) {
	tests := []struct {
		size Size
		max  int
	}{{Full, 80}, {Compact, 80}, {OneLine, 60}}
	for _, tc := range tests {
		if w := Width(Lines(tc.size)); w > tc.max || w == 0 {
			t.Errorf("size %d width %d, want 1..%d", tc.size, w, tc.max)
		}
	}
}

func TestFit(t *testing.T) {
	tests := []struct {
		cols int
		max  Size
		want Size
	}{
		{0, Full, Full},
		{120, Full, Full},
		{80, Full, Full},
		{78, Full, Compact},
		{40, Full, OneLine},
		{120, Compact, Compact},
	}
	for _, tc := range tests {
		if got := Fit(tc.cols, tc.max); got != tc.want {
			t.Errorf("Fit(%d,%d) = %d, want %d", tc.cols, tc.max, got, tc.want)
		}
	}
}

func TestFitBoxHonorsHeight(t *testing.T) {
	fullH := len(Lines(Full))
	compactH := len(Lines(Compact))
	if fullH <= compactH {
		t.Fatalf("full art (%d) should be taller than compact (%d)", fullH, compactH)
	}
	tests := []struct {
		cols, rows int
		want       Size
	}{
		{0, 0, Full},
		{120, fullH + 2, Full},
		{120, fullH, Compact},
		{120, compactH, OneLine},
		{40, fullH + 2, OneLine},
	}
	for _, tc := range tests {
		if got := FitBox(tc.cols, tc.rows, Full); got != tc.want {
			t.Errorf("FitBox(%d,%d) = %d, want %d", tc.cols, tc.rows, got, tc.want)
		}
	}
	if got := Fit(80, Full); got != FitBox(80, 0, Full) {
		t.Errorf("Fit and FitBox disagree: %d vs %d", got, FitBox(80, 0, Full))
	}
}

func TestRenderGlowKeepsTheArt(t *testing.T) {
	a := stripANSI(RenderGlow(Lines(Compact), 0.2))
	b := stripANSI(RenderGlow(Lines(Compact), 0.8))
	if !strings.Contains(a, "by the sapper of the unbuilt") || !strings.Contains(b, "by the sapper of the unbuilt") {
		t.Fatal("glow frame lost the art")
	}
	if !strings.Contains(RenderGlow(Lines(Compact), 0.2), "\x1b[") {
		t.Fatal("glow frame is not coloured")
	}
}

func TestRenderNoColorIsPlain(t *testing.T) {
	out := Render(Lines(Compact), false)
	if strings.Contains(out, "\x1b") {
		t.Fatal("plain render contains escape codes")
	}
	if out != strings.Join(Lines(Compact), "\n")+"\n" {
		t.Fatal("plain render differs from source art")
	}
}

func TestRenderColorResets(t *testing.T) {
	for _, l := range strings.Split(strings.TrimRight(Render(Lines(Full), true), "\n"), "\n") {
		if strings.Contains(l, "\x1b[38;2") && !strings.HasSuffix(l, reset) {
			t.Fatalf("coloured line not reset: %q", l)
		}
	}
}

func TestIceEndpoints(t *testing.T) {
	tests := []struct {
		v    float64
		want RGB
	}{{-1, RGB{0, 8, 16}}, {0, RGB{0, 8, 16}}, {1, RGB{240, 252, 255}}, {2, RGB{240, 252, 255}}}
	for _, tc := range tests {
		if got := Ice(tc.v); got != tc.want {
			t.Errorf("Ice(%v) = %v, want %v", tc.v, got, tc.want)
		}
	}
}

func TestVerdict(t *testing.T) {
	tests := []struct {
		survived bool
		want     string
	}{{true, "S U R V I V E D"}, {false, "B R E A C H E D"}}
	for _, tc := range tests {
		out := Verdict{Survived: tc.survived, Title: "T", Lines: []string{"detail"}}.Render(false)
		if !strings.Contains(out, tc.want) || !strings.Contains(out, "detail") {
			t.Errorf("verdict output missing %q: %s", tc.want, out)
		}
	}
}

func TestAnimateEndsWithFinalArt(t *testing.T) {
	var buf bytes.Buffer
	skip := make(chan struct{})
	close(skip)
	Animate(&buf, Lines(Compact), 10, time.Millisecond, skip)
	out := buf.String()
	if !strings.HasSuffix(out, "\x1b[?25h") {
		t.Fatal("cursor not restored")
	}
	if !strings.Contains(stripANSI(out), "by the sapper of the unbuilt") {
		t.Fatal("final frame missing tagline")
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && !(s[i] >= 'A' && s[i] <= 'Z' || s[i] >= 'a' && s[i] <= 'z') {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
