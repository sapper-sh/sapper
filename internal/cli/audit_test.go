package cli

import (
	"reflect"
	"testing"
)

func TestJoinedQuotedArguments(t *testing.T) {
	got, err := splitConsoleArgs("validate --blueprint=\"my file\" a'b'c \"\"")
	want := []string{"validate", "--blueprint=my file", "abc", ""}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("args=%q err=%v", got, err)
	}
}

func TestConsoleRejectsPasswordStdinBeforeRead(t *testing.T) {
	a := &app{inConsole: true}
	if _, err := (&passwordFlags{stdin: true}).resolve(a, nil); err == nil {
		t.Fatal("console accepted conflicting stdin readers")
	}
}
