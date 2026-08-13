package service

import (
	"testing"
)

func TestJQLBuilder(t *testing.T) {
	b := &JQLBuilder{}
	got := b.Project("WEBAPP").Status("In Progress").Assignee("me").Text("export").String()
	want := `project = WEBAPP AND status = "In Progress" AND assignee = currentUser() AND text ~ export ORDER BY updated DESC`
	if got != want {
		t.Fatalf("jql = %q, want %q", got, want)
	}
}

func TestJQLBuilderKey(t *testing.T) {
	got := (&JQLBuilder{}).Key("AB-1557").String()
	want := "key = AB-1557 ORDER BY updated DESC"
	if got != want {
		t.Fatalf("jql = %q, want %q", got, want)
	}
}

func TestJQLBuilderEmpty(t *testing.T) {
	if got := (&JQLBuilder{}).String(); got != "" {
		t.Fatalf("empty builder = %q", got)
	}
}

func TestQuoteJQL(t *testing.T) {
	if got := quoteJQL(`a"b`); got != `"a\"b"` {
		t.Fatalf("quote = %q", got)
	}
	if got := quoteJQL("AB-1"); got != "AB-1" {
		t.Fatalf("safe token quoted: %q", got)
	}
}

func TestParseFieldArgs(t *testing.T) {
	specs, err := ParseFieldArgs([]string{"Story Points=5", "labels=a,b"})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(specs) != 2 || specs[0].Key != "Story Points" || specs[0].Value != "5" {
		t.Fatalf("specs = %+v", specs)
	}
	if _, err := ParseFieldArgs([]string{"novalue"}); err == nil {
		t.Fatal("expected error for missing =")
	}
}
