package httpx

import (
	"strings"
	"testing"
)

func TestHTMLToText(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"paragraphs and headings", `<h1>Title</h1><p>One</p><p>Two</p>`, "Title\nOne\nTwo"},
		{"inline stays on one line", "<p>A <b>B</b>\n   C</p>", "A B C"},
		{"adjacent links stay apart", `<div><a href="/u">Name</a><a href="/out">log out</a></div>`, "Name log out"},
		{"adjacent buttons stay apart", `<p><button>Save</button><button>Cancel</button></p>`, "Save Cancel"},
		{"a link inside a word is not split", `<p>pre<a href="/x">fix</a>.</p>`, "prefix."},
		{"bold inside a word is not split", `<p>un<b>believ</b>able</p>`, "unbelievable"},
		{"br", `a<br>b<br/>c`, "a\nb\nc"},
		{"list", `<ul><li>one</li><li>two</li></ul>`, "one\ntwo"},
		{"entities", `<p>a &amp; b &lt;c&gt; &#65; &nbsp;d</p>`, "a & b <c> A d"},
		{"comment", `<p>a<!-- secret -->b</p>`, "ab"},
		{"script and style in body", `<body><p>a</p><script>var k="TOKEN"</script><style>.x{}</style><p>b</p></body>`, "a\nb"},
		{"noscript template svg", `<p>a</p><noscript>NS</noscript><template>TPL</template><svg><text>SVG</text></svg>`, "a"},
		{"head is dropped", `<html><head><title>T</title><script>x()</script></head><body>text</body></html>`, "text"},
		{"unclosed tags", `<div><p>one<p>two<b>bold`, "one\ntwobold"},
		{"stray close tags", `</div></p>text</span>`, "text"},
		{"table cells", `<table><tr><th>A</th><th>B</th></tr><tr><td>1</td><td>2</td></tr></table>`, "A | B\n1 | 2"},
		{"nested table", `<table><tr><td>a<table><tr><td>x</td><td>y</td></tr></table></td><td>b</td></tr></table>`, "a\nx | y\nb"},
		{"pre keeps indentation and lines", "<pre>line1\n    indented\n\tTab</pre><p>after</p>", "line1\n    indented\n\tTab\nafter"},
		{"pre inside text", "<p>before <pre>a  b</pre> after</p>", "before\na  b\nafter"},
		{"input values are not text", `<form><input type="hidden" name="csrf" value="SECRET"><input type="password" value="PW"><input value="V" placeholder="P"><button value="B">Go</button></form>`, "Go"},
		{"attributes are not text", `<a href="https://x/?token=T" title="TT" data-k="D">link</a><img src="s" alt="ALT">`, "link"},
		{"textarea and select are page text only", `<select><option value="OV">Shown</option></select>`, "Shown"},
		{"plain text fragment", "just  text", "just text"},
		{"blank lines collapse", "<p>a</p>\n\n\n<p>b</p>", "a\nb"},
		{"empty", ``, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HTMLToText([]byte(tc.in))
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			for _, leak := range []string{"SECRET", "PW", "TOKEN", "OV", "ALT", "https://", "<"} {
				if strings.Contains(got, leak) && !strings.Contains(tc.want, leak) {
					t.Fatalf("leaked %q in %q", leak, got)
				}
			}
		})
	}
}

func TestHTMLToTextHugeInput(t *testing.T) {
	wide := strings.Repeat("<p>row</p>", 300000)
	if got, err := HTMLToText([]byte(wide)); err != nil || !strings.HasPrefix(got, "row\nrow") {
		t.Fatalf("wide: %q, %v", got[:min(len(got), 40)], err)
	}
	// The parser refuses very deep nesting; the markup must then not leak as "text".
	deep := "<div><script>var k=1</script>" + strings.Repeat("<div>", 5000) + "deep"
	if got, err := HTMLToText([]byte(deep)); err == nil || got != "" {
		t.Fatalf("deep nesting: got %q, %v", got[:min(len(got), 40)], err)
	}
}

func TestLooksLikeHTML(t *testing.T) {
	for in, want := range map[string]bool{
		"<!DOCTYPE html><html>": true, "  \n<HTML>": true, "<body>x": true, "<head>": true,
		`{"html":"<html>"}`: false, "<xml/>": false, "plain": false, "": false, "<htmlish": true,
	} {
		if got := LooksLikeHTML([]byte(in)); got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}
