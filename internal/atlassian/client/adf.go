package client

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ADF (Atlassian Document Format) is the rich-text JSON Jira Cloud API v3 uses
// for descriptions and comments. This file renders ADF to readable terminal
// text and builds minimal ADF from plain text.

// TextToADF wraps plain text in a minimal ADF document: blank-line separated
// blocks become paragraphs, single newlines become hard breaks.
func TextToADF(text string) map[string]any {
	var content []any
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n\n") {
		lines := strings.Split(para, "\n")
		var inner []any
		for i, line := range lines {
			if i > 0 {
				inner = append(inner, map[string]any{"type": "hardBreak"})
			}
			if line != "" {
				inner = append(inner, map[string]any{"type": "text", "text": line})
			}
		}
		if len(inner) == 0 {
			continue
		}
		content = append(content, map[string]any{"type": "paragraph", "content": inner})
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "paragraph", "content": []any{}})
	}
	return map[string]any{"type": "doc", "version": 1, "content": content}
}

// MarkdownToADF converts a Markdown subset into an ADF document. It supports
// ATX headings (#..######), bullet lists (-, *, +), ordered lists (1.), fenced
// code blocks (```lang), blockquotes (>), thematic breaks (---), and inline
// **bold**, *italic*, `code`, and [text](url) links. Unrecognised lines stay
// as paragraph text, so content is never lost. This is the rough inverse of
// the ADFToText renderer below.
func MarkdownToADF(md string) map[string]any {
	md = strings.ReplaceAll(md, "\r\n", "\n")
	md = strings.ReplaceAll(md, "\r", "\n")
	lines := strings.Split(md, "\n")
	var content []any
	i := 0
	for i < len(lines) {
		t := strings.TrimSpace(lines[i])
		switch {
		case t == "":
			i++
		case strings.HasPrefix(t, "```"):
			lang := strings.TrimSpace(strings.TrimPrefix(t, "```"))
			i++
			var code []string
			for i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), "```") {
				code = append(code, lines[i])
				i++
			}
			if i < len(lines) {
				i++ // consume closing fence
			}
			node := map[string]any{"type": "codeBlock"}
			if lang != "" {
				node["attrs"] = map[string]any{"language": lang}
			}
			if len(code) > 0 {
				node["content"] = []any{map[string]any{"type": "text", "text": strings.Join(code, "\n")}}
			} else {
				node["content"] = []any{}
			}
			content = append(content, node)
		case headingLevel(t) > 0:
			lvl := headingLevel(t)
			content = append(content, map[string]any{
				"type":    "heading",
				"attrs":   map[string]any{"level": lvl},
				"content": inlineToADF(strings.TrimSpace(t[lvl:])),
			})
			i++
		case t == "---" || t == "***" || t == "___":
			content = append(content, map[string]any{"type": "rule"})
			i++
		case strings.HasPrefix(t, ">"):
			var qs []string
			for i < len(lines) {
				lt := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(lt, ">") {
					break
				}
				qs = append(qs, strings.TrimPrefix(strings.TrimPrefix(lt, ">"), " "))
				i++
			}
			content = append(content, map[string]any{
				"type":    "blockquote",
				"content": []any{mdParagraph(strings.Join(qs, "\n"))},
			})
		case isBullet(t):
			var items []any
			for i < len(lines) {
				lt := strings.TrimSpace(lines[i])
				if !isBullet(lt) {
					break
				}
				items = append(items, mdListItem(bulletText(lt)))
				i++
			}
			content = append(content, map[string]any{"type": "bulletList", "content": items})
		case isOrdered(t):
			var items []any
			for i < len(lines) {
				lt := strings.TrimSpace(lines[i])
				if !isOrdered(lt) {
					break
				}
				items = append(items, mdListItem(orderedText(lt)))
				i++
			}
			content = append(content, map[string]any{"type": "orderedList", "content": items})
		default:
			var para []string
			for i < len(lines) {
				lt := strings.TrimSpace(lines[i])
				if lt == "" || headingLevel(lt) > 0 || isBullet(lt) || isOrdered(lt) ||
					strings.HasPrefix(lt, "```") || strings.HasPrefix(lt, ">") ||
					lt == "---" || lt == "***" || lt == "___" {
					break
				}
				para = append(para, lines[i])
				i++
			}
			content = append(content, mdParagraph(strings.Join(para, "\n")))
		}
	}
	if len(content) == 0 {
		content = append(content, map[string]any{"type": "paragraph", "content": []any{}})
	}
	return map[string]any{"type": "doc", "version": 1, "content": content}
}

// headingLevel returns 1..6 for an ATX heading line ("## x"), else 0.
func headingLevel(s string) int {
	n := 0
	for n < len(s) && s[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n >= len(s) || s[n] != ' ' {
		return 0
	}
	return n
}

func isBullet(s string) bool {
	return len(s) >= 2 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && s[1] == ' '
}

func bulletText(s string) string { return strings.TrimSpace(s[2:]) }

// isOrdered reports a leading "<digits>. " marker.
func isOrdered(s string) bool {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n > 0 && n+1 < len(s) && s[n] == '.' && s[n+1] == ' '
}

func orderedText(s string) string {
	dot := strings.IndexByte(s, '.')
	return strings.TrimSpace(s[dot+1:])
}

// mdListItem wraps one list line's inline content in a listItem > paragraph.
func mdListItem(text string) map[string]any {
	return map[string]any{"type": "listItem", "content": []any{mdParagraph(text)}}
}

// mdParagraph builds a paragraph node; single newlines become hardBreaks and
// inline Markdown marks are parsed per line.
func mdParagraph(text string) map[string]any {
	lines := strings.Split(text, "\n")
	var inner []any
	for idx, ln := range lines {
		if idx > 0 {
			inner = append(inner, map[string]any{"type": "hardBreak"})
		}
		inner = append(inner, inlineToADF(ln)...)
	}
	if inner == nil {
		inner = []any{}
	}
	return map[string]any{"type": "paragraph", "content": inner}
}

// inlineToADF parses inline Markdown (`code`, [links](url), **bold**, *italic*)
// into ADF text nodes. Code spans take precedence and are literal inside.
func inlineToADF(s string) []any {
	var out []any
	var plain strings.Builder
	flush := func() {
		if plain.Len() > 0 {
			out = append(out, mdText(plain.String(), nil))
			plain.Reset()
		}
	}
	r := []rune(s)
	i := 0
	for i < len(r) {
		switch {
		case r[i] == '`':
			if j := idxRune(r, '`', i+1); j >= 0 {
				flush()
				out = append(out, mdText(string(r[i+1:j]), []map[string]any{{"type": "code"}}))
				i = j + 1
				continue
			}
			plain.WriteRune(r[i])
			i++
		case r[i] == '[':
			if c := idxRune(r, ']', i+1); c > i && c+1 < len(r) && r[c+1] == '(' {
				if p := idxRune(r, ')', c+2); p > c+1 {
					flush()
					out = append(out, mdText(string(r[i+1:c]), []map[string]any{{"type": "link", "attrs": map[string]any{"href": string(r[c+2 : p])}}}))
					i = p + 1
					continue
				}
			}
			plain.WriteRune(r[i])
			i++
		case hasPrefixAt(r, i, "**"):
			if j := idxDelim(r, i+2, "**"); j > i+1 {
				flush()
				out = append(out, mdText(string(r[i+2:j]), []map[string]any{{"type": "strong"}}))
				i = j + 2
				continue
			}
			plain.WriteRune(r[i])
			i++
		case r[i] == '*':
			if j := idxRune(r, '*', i+1); j > i+1 {
				flush()
				out = append(out, mdText(string(r[i+1:j]), []map[string]any{{"type": "em"}}))
				i = j + 1
				continue
			}
			plain.WriteRune(r[i])
			i++
		default:
			plain.WriteRune(r[i])
			i++
		}
	}
	flush()
	if out == nil {
		out = []any{}
	}
	return out
}

func mdText(text string, marks []map[string]any) map[string]any {
	n := map[string]any{"type": "text", "text": text}
	if len(marks) > 0 {
		n["marks"] = marks
	}
	return n
}

func idxRune(r []rune, target rune, from int) int {
	for i := from; i < len(r); i++ {
		if r[i] == target {
			return i
		}
	}
	return -1
}

func hasPrefixAt(r []rune, i int, p string) bool {
	pr := []rune(p)
	if i+len(pr) > len(r) {
		return false
	}
	for k, c := range pr {
		if r[i+k] != c {
			return false
		}
	}
	return true
}

func idxDelim(r []rune, from int, delim string) int {
	dl := len([]rune(delim))
	for i := from; i+dl <= len(r); i++ {
		if hasPrefixAt(r, i, delim) {
			return i
		}
	}
	return -1
}

// ADFToText renders an ADF document (raw JSON) as plain readable text.
// Non-ADF input (a bare JSON string) is returned as-is.
func ADFToText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var node adfNode
	if err := json.Unmarshal(raw, &node); err != nil {
		return strings.TrimSpace(string(raw))
	}
	var b strings.Builder
	renderADF(&b, &node, 0, "")
	return tidyText(b.String())
}

// adfNode is a tolerant ADF node representation.
type adfNode struct {
	Type    string           `json:"type"`
	Text    string           `json:"text"`
	Content []adfNode        `json:"content"`
	Attrs   map[string]any   `json:"attrs"`
	Marks   []map[string]any `json:"marks"`
}

// renderADF walks an ADF tree appending text. depth tracks list nesting,
// listMarker is the bullet/number prefix applied to listItem children.
func renderADF(b *strings.Builder, n *adfNode, depth int, listMarker string) {
	indent := strings.Repeat("  ", depth)
	switch n.Type {
	case "doc":
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
	case "paragraph":
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
		b.WriteString("\n\n")
	case "heading":
		level := 1
		if l, ok := n.Attrs["level"].(float64); ok {
			level = int(l)
		}
		b.WriteString(strings.Repeat("#", level) + " ")
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
		b.WriteString("\n\n")
	case "text":
		text := n.Text
		for _, m := range n.Marks {
			if m["type"] == "link" {
				if attrs, ok := m["attrs"].(map[string]any); ok {
					if href, ok := attrs["href"].(string); ok && href != text {
						text = text + " (" + href + ")"
					}
				}
			}
			if m["type"] == "code" {
				text = "`" + text + "`"
			}
		}
		b.WriteString(text)
	case "hardBreak":
		b.WriteString("\n")
	case "bulletList":
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "- ")
		}
		if depth == 0 {
			b.WriteString("\n")
		}
	case "orderedList":
		start := 1
		if s, ok := n.Attrs["order"].(float64); ok {
			start = int(s)
		}
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, fmt.Sprintf("%d. ", start+i))
		}
		if depth == 0 {
			b.WriteString("\n")
		}
	case "listItem":
		b.WriteString(indent + listMarker)
		// Render item content inline-ish: first paragraph on the marker line.
		var inner strings.Builder
		for i := range n.Content {
			renderADF(&inner, &n.Content[i], depth+1, "")
		}
		s := strings.TrimRight(inner.String(), "\n")
		s = strings.ReplaceAll(s, "\n\n", "\n")
		b.WriteString(strings.TrimLeft(s, " ") + "\n")
	case "codeBlock":
		lang := ""
		if l, ok := n.Attrs["language"].(string); ok {
			lang = l
		}
		b.WriteString("```" + lang + "\n")
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
		b.WriteString("\n```\n\n")
	case "blockquote":
		var inner strings.Builder
		for i := range n.Content {
			renderADF(&inner, &n.Content[i], depth, "")
		}
		for _, line := range strings.Split(strings.TrimRight(inner.String(), "\n"), "\n") {
			b.WriteString("> " + line + "\n")
		}
		b.WriteString("\n")
	case "rule":
		b.WriteString("---\n\n")
	case "mention":
		if t, ok := n.Attrs["text"].(string); ok {
			b.WriteString(t)
		} else {
			b.WriteString("@unknown")
		}
	case "emoji":
		if t, ok := n.Attrs["shortName"].(string); ok {
			b.WriteString(t)
		}
	case "date":
		if t, ok := n.Attrs["timestamp"].(string); ok {
			b.WriteString("(date:" + t + ")")
		}
	case "status":
		if t, ok := n.Attrs["text"].(string); ok {
			b.WriteString("[" + strings.ToUpper(t) + "]")
		}
	case "inlineCard", "blockCard", "embedCard":
		if u, ok := n.Attrs["url"].(string); ok {
			b.WriteString(u)
		}
		if n.Type != "inlineCard" {
			b.WriteString("\n\n")
		}
	case "table":
		renderADFTable(b, n)
	case "panel":
		ptype := "panel"
		if t, ok := n.Attrs["panelType"].(string); ok {
			ptype = t
		}
		b.WriteString("[" + strings.ToUpper(ptype) + "]\n")
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
	case "taskList":
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
		if depth == 0 {
			b.WriteString("\n")
		}
	case "taskItem":
		marker := "[ ] "
		if s, ok := n.Attrs["state"].(string); ok && s == "DONE" {
			marker = "[x] "
		}
		b.WriteString(indent + marker)
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
		b.WriteString("\n")
	case "mediaGroup", "mediaSingle":
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
	case "media":
		name := "(attachment)"
		if a, ok := n.Attrs["alt"].(string); ok && a != "" {
			name = "(media: " + a + ")"
		}
		b.WriteString(name + "\n")
	case "expand", "nestedExpand":
		if t, ok := n.Attrs["title"].(string); ok && t != "" {
			b.WriteString("[> " + t + "]\n")
		}
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
	default:
		// Unknown node: render children so no text is lost.
		for i := range n.Content {
			renderADF(b, &n.Content[i], depth, "")
		}
	}
}

// renderADFTable prints an ADF table as pipe-separated rows.
func renderADFTable(b *strings.Builder, n *adfNode) {
	for _, row := range n.Content {
		if row.Type != "tableRow" {
			continue
		}
		var cells []string
		for i := range row.Content {
			var cb strings.Builder
			for j := range row.Content[i].Content {
				renderADF(&cb, &row.Content[i].Content[j], 0, "")
			}
			cell := strings.TrimSpace(strings.ReplaceAll(cb.String(), "\n", " "))
			cells = append(cells, cell)
		}
		b.WriteString("| " + strings.Join(cells, " | ") + " |\n")
	}
	b.WriteString("\n")
}

// tidyText collapses excessive blank lines and trims the result.
func tidyText(s string) string {
	for strings.Contains(s, "\n\n\n") {
		s = strings.ReplaceAll(s, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(s)
}
