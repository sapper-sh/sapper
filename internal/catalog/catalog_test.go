package catalog

import (
	"strings"
	"testing"
)

func TestCatalogueEntries(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("empty catalogue")
	}
	for _, n := range names {
		a, ok := Lookup(n)
		if !ok || a.Name != n {
			t.Fatalf("Lookup(%q) = %+v, %v", n, a, ok)
		}
		if !strings.Contains(a.Image, ":") || strings.HasSuffix(a.Image, ":latest") {
			t.Errorf("%s: image %q must be pinned to a tag", n, a.Image)
		}
		if a.Port < 1 || len(a.Mounts) == 0 {
			t.Errorf("%s: needs a port and at least one mount", n)
		}
		seen := map[string]bool{}
		for _, m := range a.Mounts {
			if seen[m.Name] || !strings.HasPrefix(m.ContainerPath, "/") {
				t.Errorf("%s: bad or duplicate mount %+v", n, m)
			}
			seen[m.Name] = true
		}
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup of unknown app succeeded")
	}
}
