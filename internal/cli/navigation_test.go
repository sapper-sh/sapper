package cli

import (
	"strings"
	"testing"
)

func TestRecallEditAndDraft(t *testing.T) {
	var e lineEdit
	e.feedBytes([]byte("sttus\rdraft\x1b["))
	e.feedBytes([]byte("A\x1b[D\x1b[D\x1b[Da"))
	if e.String() != "status" { t.Fatalf("edited recall: %q",e.String()) }
	e.feedBytes([]byte("\x1b[B"))
	if e.String() != "draft" { t.Fatalf("lost draft: %q",e.String()) }
}

func TestPagedLogAndUnicodeBlueprint(t *testing.T) {
	v:=consoleLayout(40,8)
	log:=[]string{"first","second","third","fourth","fifth","sixth"}
	v.offset=2
	frame:=renderConsoleBody(v,log,"")
	if !strings.Contains(frame,"2 below") || strings.Contains(frame,"sixth") { t.Fatal("page includes newest row") }
	if !strings.Contains(sessionRule(40,"projet-\u00e9cole/\u754c"),"projet-\u00e9cole/\u754c") { t.Fatal("blueprint lost Unicode") }
}
