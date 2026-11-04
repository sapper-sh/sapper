package paths

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestStateBase(t *testing.T) {
	home := func() (string, error) { return filepath.FromSlash("/home/u"), nil }
	noHome := func() (string, error) { return "", errors.New("no home") }
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	tests := []struct {
		name, goos string
		env        map[string]string
		home       func() (string, error)
		want       string
		wantErr    bool
	}{
		{"linux default", "linux", nil, home, "/home/u/.local/state/sapper", false},
		{"linux xdg", "linux", map[string]string{"XDG_STATE_HOME": abs("/x/state")}, home, filepath.Join(abs("/x/state"), "sapper"), false},
		{"linux relative xdg ignored", "linux", map[string]string{"XDG_STATE_HOME": "rel"}, home, "/home/u/.local/state/sapper", false},
		{"freebsd uses xdg", "freebsd", nil, home, "/home/u/.local/state/sapper", false},
		{"darwin", "darwin", nil, home, "/home/u/Library/Application Support/sapper", false},
		{"windows", "windows", map[string]string{"LOCALAPPDATA": `C:\Users\u\AppData\Local`}, home, `C:\Users\u\AppData\Local` + string(filepath.Separator) + "sapper", false},
		{"windows unset", "windows", nil, home, "", true},
		{"no home", "linux", nil, noHome, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := stateBase(tc.goos, env(tc.env), tc.home)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			want := tc.want
			if tc.goos != "windows" {
				want = filepath.FromSlash(want)
			}
			if got != want {
				t.Errorf("got %q, want %q", got, want)
			}
		})
	}
}

func abs(p string) string {
	a, _ := filepath.Abs(filepath.FromSlash(p))
	return a
}

func TestFortressStateDistinct(t *testing.T) {
	t.Setenv(EnvStateDir, t.TempDir())
	a, err := FortressState("f", filepath.FromSlash("/a"))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := FortressState("f", filepath.FromSlash("/b"))
	if a == b {
		t.Fatal("different blueprint dirs share a state dir")
	}
}
