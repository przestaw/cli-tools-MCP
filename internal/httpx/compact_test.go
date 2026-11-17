package httpx

import (
	"strings"
	"testing"
)

func TestParseMode(t *testing.T) {
	for _, tc := range []struct {
		output   string
		htmlOnly bool
		want     Mode
		wantErr  string
	}{
		{"", false, Original, ""},
		{"original", false, Original, ""},
		{"compact", false, Compact, ""},
		{"", true, HTMLOnly, ""},
		{"compact", true, Compact, ""},
		{"original", true, Original, "conflict"},
		{"toon", false, Original, "output must be"},
		{"Compact", false, Original, "output must be"},
	} {
		got, err := ParseMode(tc.output, tc.htmlOnly)
		if (err == nil) != (tc.wantErr == "") || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) || got != tc.want {
			t.Errorf("ParseMode(%q, %v) = %v, %v; want %v, %q", tc.output, tc.htmlOnly, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestModeApply(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    Mode
		media   string
		body    string
		want    string
		wantErr bool
	}{
		{"json numbers untouched", Compact, "application/json", `{ "id": 9007199254740993, "v": 1.2300, "e": 1E+2, "nil": null, "empty": [] }`, `{"id":9007199254740993,"v":1.2300,"e":1E+2,"nil":null,"empty":[]}`, false},
		{"json strings untouched", Compact, "application/json", "{ \"s\": \" a  b \\n <&> \" }", "{\"s\":\" a  b \\n <&> \"}", false},
		{"json without media type", Compact, "", ` [1, 2] `, `[1,2]`, false},
		{"json in text/plain", Compact, "text/plain", ` {"a": 1} `, `{"a":1}`, false},
		{"invalid json", Compact, "application/json", `{`, "", true},
		{"invalid +json", Compact, "application/vnd.api+json", `{`, "", true},
		{"truncated text/plain is kept", Compact, "text/plain", `{"a":`, `{"a":`, false},
		{"html by type", Compact, "text/html; charset=utf-8", `<div>Caption <b>text</b></div>`, "Caption text", false},
		{"html by content", Compact, "application/octet-stream", `<!doctype html><p>x</p>`, "x", false},
		{"xml unchanged", Compact, "application/xml", `<p>A <b>B</b>  C</p>`, `<p>A <b>B</b>  C</p>`, false},
		{"soap unchanged", Compact, "application/soap+xml", "<html>\n <b>x</b></html>", "<html>\n <b>x</b></html>", false},
		{"plain text unchanged", Compact, "text/plain", "plain\n  text", "plain\n  text", false},
		{"html only: json unchanged", HTMLOnly, "application/json", `{ "a": 1 }`, `{ "a": 1 }`, false},
		{"html only: invalid json unchanged", HTMLOnly, "application/json", `{`, `{`, false},
		{"html only: html converted", HTMLOnly, "text/html", `<p>a</p>`, "a", false},
		{"original", Original, "text/html", `<p>a</p>`, `<p>a</p>`, false},
		{"unparsable html is an error", Compact, "text/html", strings.Repeat("<div>", 5000), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.mode.Apply([]byte(tc.body), tc.media)
			if (err != nil) != tc.wantErr || string(got) != tc.want {
				t.Fatalf("got %q, %v; want %q, error %v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
