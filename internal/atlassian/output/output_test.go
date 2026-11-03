// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"workctl/internal/atlassian/client"
)

func TestTableSelectsAndOrdersColumns(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{W: &buf, Fields: []string{"status", "key"}}
	err := p.Table([]string{"Key", "Summary", "Status"}, [][]string{{"A-1", "s", "Open"}})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "STATUS") || !strings.Contains(out, "KEY") || strings.Contains(out, "SUMMARY") {
		t.Fatalf("columns wrong:\n%s", out)
	}
}

func TestCellTruncationAndExpand(t *testing.T) {
	long := strings.Repeat("x", 120)
	p := &Printer{}
	if got := p.cell(long); len([]rune(got)) != 80 || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated = %q", got)
	}
	p.Expand = true
	if got := p.cell(long); got != long {
		t.Fatalf("expand should keep full value")
	}
	if got := (&Printer{}).cell("a\nb\tc"); got != "a b c" {
		t.Fatalf("newlines = %q", got)
	}
}

func TestStorageToText(t *testing.T) {
	p := &Printer{}
	in := `<h1>Title</h1><p>one &amp; two</p><ul><li>a</li><li>b</li></ul>` +
		`<ac:structured-macro ac:name="toc"></ac:structured-macro>`
	got := p.StorageToText(in)
	for _, want := range []string{"Title", "one & two", "- a", "- b", "[macro:toc]"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestIssueTableRenders(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{W: &buf}
	issues := []client.Issue{{
		Key: "AB-1",
		Fields: client.IssueFields{
			Summary:   "Do thing",
			Status:    &client.Named{Name: "Open"},
			IssueType: &client.Named{Name: "Task"},
			Updated:   "2026-06-12T10:00:00.000+0200",
		},
	}}
	if err := p.IssueTable(issues); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"AB-1", "Do thing", "Open", "Task", "2026-06-12"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestIssueDetailShowsCustomFieldsWithNames(t *testing.T) {
	var buf bytes.Buffer
	p := &Printer{W: &buf}
	is := &client.Issue{
		Key: "AB-2",
		Fields: client.IssueFields{
			Summary: "S",
			Custom: map[string]json.RawMessage{
				"customfield_10010": json.RawMessage(`{"value":"Critical"}`),
				"customfield_10099": json.RawMessage(`null`),
			},
		},
		Names: map[string]string{"customfield_10010": "Severity"},
	}
	if err := p.Issue(is, ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "Severity") || !strings.Contains(out, "Critical") {
		t.Fatalf("custom field not rendered:\n%s", out)
	}
	if strings.Contains(out, "customfield_10099") {
		t.Fatalf("null custom field should be hidden:\n%s", out)
	}
}

func TestFormatAnyShapes(t *testing.T) {
	if got := formatAny(map[string]any{"displayName": "Bob"}); got != "Bob" {
		t.Fatalf("user = %q", got)
	}
	if got := formatAny([]any{map[string]any{"value": "A"}, map[string]any{"value": "B"}}); got != "A, B" {
		t.Fatalf("options = %q", got)
	}
	if got := formatAny(float64(42)); got != "42" {
		t.Fatalf("int = %q", got)
	}
}
