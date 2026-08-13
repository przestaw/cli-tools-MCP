package output

import (
	"html"
	"regexp"
	"strings"
)

var (
	reTag       = regexp.MustCompile(`(?s)<[^>]+>`)
	reBlankRuns = regexp.MustCompile(`\n{3,}`)
	reSpaces    = regexp.MustCompile(`[ \t]+`)
	reLi        = regexp.MustCompile(`(?i)<li[^>]*>`)
	reEmphasis  = regexp.MustCompile(`(?is)<(b|strong|h[1-6])[^>]*>(.*?)</\w+>`)
	reMacroName = regexp.MustCompile(`(?is)<ac:structured-macro[^>]*ac:name="([^"]+)"`)
)

// StorageToText renders Confluence storage-format XHTML to readable terminal
// text: block tags become newlines, list items bullets, macros a [macro:name]
// marker, remaining tags are stripped and entities decoded.
func (p *Printer) StorageToText(s string) string {
	if s == "" {
		return ""
	}
	// Macros: keep a visible marker with the macro name before stripping.
	s = reMacroName.ReplaceAllString(s, "\n[macro:$1]$0")
	repl := strings.NewReplacer(
		"<br>", "\n", "<br/>", "\n", "<br />", "\n",
		"</p>", "\n\n", "</div>", "\n", "</tr>", "\n",
		"</h1>", "\n", "</h2>", "\n", "</h3>", "\n", "</h4>", "\n",
		"</li>", "", "</table>", "\n", "</td>", " | ", "</th>", " | ",
	)
	s = repl.Replace(s)
	s = reLi.ReplaceAllString(s, "\n  - ")
	if p.Color {
		s = reEmphasis.ReplaceAllString(s, ansiBold+"$2"+ansiReset)
	}
	s = reTag.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = reSpaces.ReplaceAllString(s, " ")
	s = reBlankRuns.ReplaceAllString(s, "\n\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
