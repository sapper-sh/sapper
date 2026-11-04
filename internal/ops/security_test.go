package ops

import (
	"context"
	"strings"
	"testing"
)

func TestBreachRejectsTraversalSnapshotID(t *testing.T) {
	bp := newFortress(t)
	fr := &fakeRestic{snapDir: t.TempDir(), snapID: "../../../../tmp/evil"}
	_, err := Breach(context.Background(), bp, fr, DrillOptions{})
	if err == nil || !strings.Contains(err.Error(), "invalid snapshot id") {
		t.Fatalf("err = %v, want invalid snapshot id", err)
	}
}

func TestValidID(t *testing.T) {
	for id, want := range map[string]bool{
		"f00dfeed": true, strings.Repeat("a", 64): true,
		"": false, "abc": false, "../x/../y": false, "F00DFEED": false, strings.Repeat("a", 65): false,
	} {
		if validID(id) != want {
			t.Errorf("validID(%q) = %v", id, !want)
		}
	}
}
