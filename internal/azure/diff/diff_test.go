package diff

import (
	"strings"
	"testing"
)

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func TestUnifiedIdentical(t *testing.T) {
	if got := Unified(lines("a\nb\nc"), lines("a\nb\nc"), 3); got != "" {
		t.Fatalf("expected empty diff, got %q", got)
	}
}

func TestUnifiedEdit(t *testing.T) {
	got := Unified(lines("a\nb\nc"), lines("a\nB\nc"), 3)
	want := "@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n"
	if got != want {
		t.Fatalf("diff mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestUnifiedAddDelete(t *testing.T) {
	got := Unified(lines("x\ny"), lines("x\nnew\ny"), 3)
	want := "@@ -1,2 +1,3 @@\n x\n+new\n y\n"
	if got != want {
		t.Fatalf("insert mismatch:\n got: %q\nwant: %q", got, want)
	}
	got = Unified(nil, lines("only"), 3)
	want = "@@ -0,0 +1 @@\n+only\n"
	if got != want {
		t.Fatalf("add-file mismatch:\n got: %q\nwant: %q", got, want)
	}
}

func TestUnifiedSeparateHunks(t *testing.T) {
	a := lines("1\n2\n3\n4\n5\n6\n7\n8\n9")
	b := lines("1x\n2\n3\n4\n5\n6\n7\n8\n9x")
	got := Unified(a, b, 1)
	// Two changes far apart with ctx=1 must produce two hunks.
	if strings.Count(got, "@@ -") != 2 {
		t.Fatalf("expected 2 hunks, got:\n%s", got)
	}
}
