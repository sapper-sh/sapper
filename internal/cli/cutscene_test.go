package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sapper-sh/sapper/internal/art"
)

func TestScreenRestoreOnce(t *testing.T) {
	var b bytes.Buffer
	s := enterScreen(&b, nil)
	if !strings.HasPrefix(b.String(), "\x1b[?1049h\x1b[?25l") {
		t.Fatalf("enter: %q", b.String())
	}
	s.restore()
	s.restore()
	if strings.Count(b.String(), "\x1b[?25h\x1b[?1049l") != 1 {
		t.Fatalf("restore not exactly once: %q", b.String())
	}
}

func TestPlayCutsceneSkipAndSignal(t *testing.T) {
	cs := art.NewCutscene(art.EndSurvived, "Done.", 1)
	var b bytes.Buffer
	keys := make(chan struct{})
	close(keys)
	if !playCutscene(&b, cs, keys, nil, time.Millisecond) {
		t.Fatal("skip reported as interrupted")
	}
	if !strings.HasSuffix(b.String(), cs.Frame(cs.Frames()-1)) {
		t.Error("skip did not end on the final frame")
	}
	sig := make(chan os.Signal, 1)
	sig <- os.Interrupt
	b.Reset()
	if playCutscene(&b, cs, nil, sig, time.Hour) {
		t.Error("signal not reported")
	}
}

func TestCutsceneFallbacks(t *testing.T) {
	if cutsceneFits(77, 40) || cutsceneFits(120, 29) || !cutsceneFits(78, 30) {
		t.Error("size check wrong")
	}
	// Not a terminal: the static story, with no escape sequences or alternate screen.
	r := run(t, "", "tale")
	if strings.Contains(r.stdout, "\x1b[") || !strings.Contains(r.stdout, "I. The treasure box") {
		t.Errorf("non-TTY output changed:\n%s", r.stdout)
	}
}
