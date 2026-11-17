// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package httpx

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/net/html"
)

// skipTags hold no readable content.
var skipTags = map[string]bool{"script": true, "style": true, "head": true, "noscript": true, "svg": true, "template": true}

// breakTags end a line of text.
var breakTags = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "pre": true, "table": true, "ul": true, "ol": true, "section": true, "article": true, "blockquote": true}

// separateTags are controls and links that browsers lay out apart even when no whitespace sits between them.
var separateTags = map[string]bool{"a": true, "button": true, "label": true, "option": true, "summary": true}

// LooksLikeHTML reports whether body starts like an HTML document or fragment.
func LooksLikeHTML(body []byte) bool {
	head := strings.ToLower(string(bytes.TrimSpace(body[:min(len(body), 256)])))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") || strings.HasPrefix(head, "<head") || strings.HasPrefix(head, "<body")
}

// textWriter collects page text: runs of whitespace collapse to one space, except inside <pre>.
type textWriter struct {
	b     strings.Builder
	space bool // whitespace is pending between two words
}

func (w *textWriter) atLineStart() bool {
	text := w.b.String()
	return text == "" || strings.HasSuffix(text, "\n")
}

func (w *textWriter) breakLine() {
	if !w.atLineStart() {
		w.b.WriteByte('\n')
	}
	w.space = false
}

func (w *textWriter) text(s string, pre bool) {
	if pre {
		w.b.WriteString(strings.ReplaceAll(s, "\r\n", "\n"))
		return
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		w.space = w.space || s != ""
		return
	}
	w.space = w.space || strings.TrimLeftFunc(s, unicode.IsSpace) != s
	for _, word := range words {
		if w.space && !w.atLineStart() {
			w.b.WriteByte(' ')
		}
		w.b.WriteString(word)
		w.space = true
	}
	w.space = strings.TrimRightFunc(s, unicode.IsSpace) != s
}

// HTMLToText keeps what a reader of the page sees: the visible text with line breaks at block elements, without
// scripts, styles, markup and attributes (so no hidden inputs, passwords or link targets). <pre> keeps its layout.
// It fails when the page cannot be parsed (the parser limits nesting depth); the markup is then never passed on.
func HTMLToText(body []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("HTML page not converted to text: %w", err)
	}
	var w textWriter
	var walk func(n *html.Node, pre bool)
	walk = func(n *html.Node, pre bool) {
		if n.Type == html.TextNode {
			w.text(n.Data, pre)
			return
		}
		if n.Type == html.ElementNode {
			if skipTags[n.Data] {
				return
			}
			pre = pre || n.Data == "pre"
			if breakTags[n.Data] {
				w.breakLine()
			}
			if prev := n.PrevSibling; separateTags[n.Data] && prev != nil && prev.Type == html.ElementNode && separateTags[prev.Data] {
				w.space = true // adjacent links or buttons stay separate words
			}
			if (n.Data == "td" || n.Data == "th") && !w.atLineStart() {
				w.b.WriteString(" | ") // keeps the cells of a row apart
				w.space = false
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, pre)
		}
		if n.Type == html.ElementNode && breakTags[n.Data] {
			w.breakLine()
		}
	}
	walk(doc, false)
	return strings.TrimRight(strings.TrimLeft(w.b.String(), "\n"), " \t\n"), nil
}
