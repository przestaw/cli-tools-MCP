// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package httpx

import (
	"bytes"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// skipTags hold no readable content.
var skipTags = map[string]bool{"script": true, "style": true, "head": true, "noscript": true, "svg": true, "template": true}

// breakTags end a line of text.
var breakTags = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true, "h4": true,
	"h5": true, "h6": true, "pre": true, "table": true, "ul": true, "ol": true, "section": true, "article": true, "blockquote": true}

var (
	spaces     = regexp.MustCompile(`[ \t\f\r]+`)
	blankLines = regexp.MustCompile(`\n[ \n]*\n`)
)

// LooksLikeHTML reports whether body starts like an HTML document or fragment.
func LooksLikeHTML(body []byte) bool {
	head := strings.ToLower(string(bytes.TrimSpace(body[:min(len(body), 256)])))
	return strings.HasPrefix(head, "<!doctype html") || strings.HasPrefix(head, "<html") || strings.HasPrefix(head, "<head") || strings.HasPrefix(head, "<body")
}

// HTMLToText keeps what a reader of the page sees: the visible text with line breaks at block elements, without
// scripts, styles, markup and navigation attributes. Input that does not parse is returned unchanged.
func HTMLToText(body []byte) string {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		return string(body)
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(n.Data)
		case n.Type == html.ElementNode && skipTags[n.Data]:
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && breakTags[n.Data] {
			b.WriteString("\n")
		}
	}
	walk(doc)
	text := spaces.ReplaceAllString(b.String(), " ")
	text = strings.NewReplacer(" \n", "\n", "\n ", "\n").Replace(text)
	return strings.TrimSpace(blankLines.ReplaceAllString(text, "\n\n"))
}
