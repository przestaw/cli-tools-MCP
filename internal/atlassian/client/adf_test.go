// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTextToADFParagraphsAndBreaks(t *testing.T) {
	doc := TextToADF("line1\nline2\n\npara2")
	b, _ := json.Marshal(doc)
	s := string(b)
	if !strings.Contains(s, `"type":"doc"`) || !strings.Contains(s, `"version":1`) {
		t.Fatalf("not a doc: %s", s)
	}
	if !strings.Contains(s, `"hardBreak"`) {
		t.Fatalf("missing hardBreak: %s", s)
	}
	if strings.Count(s, `"type":"paragraph"`) != 2 {
		t.Fatalf("want 2 paragraphs: %s", s)
	}
	// Round trip through the renderer.
	if got := ADFToText(b); !strings.Contains(got, "line1\nline2") || !strings.Contains(got, "para2") {
		t.Fatalf("round trip = %q", got)
	}
}

func TestADFToTextRendering(t *testing.T) {
	adf := `{
	  "type":"doc","version":1,"content":[
	    {"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Title"}]},
	    {"type":"paragraph","content":[
	      {"type":"text","text":"see "},
	      {"type":"text","text":"docs","marks":[{"type":"link","attrs":{"href":"https://x"}}]},
	      {"type":"mention","attrs":{"text":"@Bob"}}
	    ]},
	    {"type":"bulletList","content":[
	      {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"alpha"}]}]},
	      {"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"beta"}]}]}
	    ]},
	    {"type":"codeBlock","attrs":{"language":"go"},"content":[{"type":"text","text":"x := 1"}]},
	    {"type":"table","content":[
	      {"type":"tableRow","content":[
	        {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"H1"}]}]},
	        {"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"H2"}]}]}
	      ]}
	    ]}
	  ]}`
	got := ADFToText(json.RawMessage(adf))
	for _, want := range []string{"## Title", "docs (https://x)", "@Bob", "- alpha", "- beta", "```go", "x := 1", "| H1 | H2 |"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

func TestADFToTextNonADFInputs(t *testing.T) {
	if got := ADFToText(json.RawMessage(`"plain string"`)); got != "plain string" {
		t.Fatalf("string passthrough = %q", got)
	}
	if got := ADFToText(nil); got != "" {
		t.Fatalf("nil = %q", got)
	}
	if got := ADFToText(json.RawMessage(`null`)); got != "" {
		t.Fatalf("null = %q", got)
	}
}

func TestTextToStorageEscapes(t *testing.T) {
	s := TextToStorage("a < b\nnext\n\npara & two")
	if s != "<p>a &lt; b<br/>next</p><p>para &amp; two</p>" {
		t.Fatalf("storage = %q", s)
	}
}

func TestMarkdownToADF(t *testing.T) {
	md := "### Context\n\nDone via `atl` and **bold** and [docs](https://x).\n\n" +
		"### Items\n\n- first\n- second `code`\n\n1. one\n2. two\n\n```go\nx := 1\n```"
	doc := MarkdownToADF(md)
	b, _ := json.Marshal(doc)
	s := string(b)
	if !strings.Contains(s, `"type":"doc"`) {
		t.Fatalf("not a doc: %s", s)
	}
	if got := strings.Count(s, `"type":"heading"`); got != 2 {
		t.Fatalf("want 2 headings, got %d: %s", got, s)
	}
	for _, want := range []string{`"type":"bulletList"`, `"type":"orderedList"`,
		`"type":"codeBlock"`, `"type":"strong"`, `"type":"code"`, `"type":"link"`, `"href":"https://x"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s in: %s", want, s)
		}
	}
	// Round-trip through the renderer: block structure survives back to text.
	got := ADFToText(b)
	for _, want := range []string{"### Context", "### Items", "- first", "- second",
		"1. one", "2. two", "```go", "x := 1", "`atl`", "docs (https://x)"} {
		if !strings.Contains(got, want) {
			t.Errorf("round-trip missing %q in:\n%s", want, got)
		}
	}
}

func TestMarkdownToADFPlainAndEmpty(t *testing.T) {
	// Plain prose with underscores (e.g. mcp__server__name) must not be mangled
	// into emphasis, and stays a single paragraph.
	doc := MarkdownToADF("just plain text with mcp__server__name token")
	b, _ := json.Marshal(doc)
	s := string(b)
	if strings.Count(s, `"type":"paragraph"`) != 1 {
		t.Fatalf("want 1 paragraph: %s", s)
	}
	if strings.Contains(s, `"type":"strong"`) || strings.Contains(s, `"type":"em"`) {
		t.Fatalf("underscores must not become emphasis: %s", s)
	}
	// Empty input renders back to empty text.
	eb, _ := json.Marshal(MarkdownToADF(""))
	if got := ADFToText(eb); got != "" {
		t.Fatalf("empty md = %q", got)
	}
}
