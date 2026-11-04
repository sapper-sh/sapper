package termx

import (
	"os"
	"testing"
)

func TestColorAllowedByEnv(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want bool
	}{
		{"default", nil, true},
		{"NO_COLOR set", map[string]string{"NO_COLOR": "1"}, false},
		{"NO_COLOR empty means unset", map[string]string{"NO_COLOR": ""}, true},
		{"dumb terminal", map[string]string{"TERM": "dumb"}, false},
		{"xterm", map[string]string{"TERM": "xterm-256color"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := colorAllowedByEnv(func(k string) string { return tc.env[k] }); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDetectNonTTYIsPlain(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if s := Detect(f, false); s.Color || s.TTY || s.Width != 0 {
		t.Fatalf("file output detected as %+v", s)
	}
}
