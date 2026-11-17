package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"strings"
)

// ValidateOutput checks a presentation option before any request is sent.
func ValidateOutput(output string) error {
	switch output {
	case "", "original", "compact":
		return nil
	default:
		return fmt.Errorf("output must be original or compact")
	}
}

// CompactResponse transforms only JSON and HTML. Other formats, including XML and binary data, stay byte-identical.
func CompactResponse(body []byte, contentType string) ([]byte, error) {
	mediaType, _, _ := mime.ParseMediaType(contentType)
	if mediaType == "application/xml" || mediaType == "text/xml" || strings.HasSuffix(mediaType, "+xml") {
		return body, nil
	}
	if mediaType == "text/html" || LooksLikeHTML(body) {
		return []byte(HTMLToText(body)), nil
	}
	if json.Valid(body) {
		var b bytes.Buffer
		if err := json.Compact(&b, body); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	if mediaType == "application/json" || strings.HasSuffix(mediaType, "+json") {
		return nil, fmt.Errorf("response received but JSON compaction failed: invalid JSON; do not replay a write")
	}
	return body, nil
}
