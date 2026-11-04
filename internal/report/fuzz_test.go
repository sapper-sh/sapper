package report

import (
	"strings"
	"testing"
	"unicode"
)

// FuzzClean checks that no input survives Clean or code() with a control character, so
// file names from a snapshot can never inject terminal escapes or new Markdown lines.
func FuzzClean(f *testing.F) {
	for _, s := range []string{"plain", "a\nb", "\x1b[2J", "\u202e", "``x``", "\xff\xfe", "` x `"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for _, out := range []string{Clean(s), CleanLines(s), code(s)} {
			for _, r := range out {
				if r != ' ' && !unicode.IsGraphic(r) {
					t.Fatalf("%q -> %q contains %U", s, out, r)
				}
			}
		}
		c := code(s)
		if c == "(none)" && s == "" {
			return
		}
		i := strings.IndexFunc(c, func(r rune) bool { return r != '`' })
		if i <= 0 || !strings.HasSuffix(c, c[:i]) || strings.Contains(c[i:len(c)-i], c[:i]) {
			t.Fatalf("code(%q) = %q is not fenced", s, c)
		}
	})
}
