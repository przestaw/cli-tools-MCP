package httpx

import (
	"bytes"
	"testing"
)

func TestCompactResponsePreservesJSONNumbersAndXML(t *testing.T) {
	for _, tc := range []struct {
		body, media, want string
	}{
		{`{ "id": 9007199254740993, "v": 1.2300, "nil": null, "empty": [] }`, "application/json", `{"id":9007199254740993,"v":1.2300,"nil":null,"empty":[]}`},
		{`<div>Caption <b>text</b></div>`, "text/html", "Caption text"},
		{`<p>A <b>B</b> C</p>`, "application/xml", `<p>A <b>B</b> C</p>`},
		{"plain\ntext", "text/plain", "plain\ntext"},
	} {
		got, err := CompactResponse([]byte(tc.body), tc.media)
		if err != nil || !bytes.Equal(got, []byte(tc.want)) {
			t.Fatalf("%s: got %q, %v", tc.media, got, err)
		}
	}
	if _, err := CompactResponse([]byte("{"), "application/json"); err == nil {
		t.Fatal("malformed JSON accepted")
	}
	if err := ValidateOutput("toon"); err == nil {
		t.Fatal("unimplemented output accepted")
	}
}
