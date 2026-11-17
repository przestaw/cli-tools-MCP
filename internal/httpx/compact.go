// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package httpx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"strings"
)

// Mode is how a raw response is presented. Callers resolve their options into one Mode up front, then apply it.
type Mode int

const (
	// Original returns the body byte for byte.
	Original Mode = iota
	// Compact minifies JSON and turns HTML into text; XML and other formats stay unchanged.
	Compact
	// HTMLOnly is the legacy `compact: true`: only HTML becomes text, JSON stays as sent.
	HTMLOnly
)

// ParseMode resolves the output option ("", original or compact) and the legacy HTML-only flag into one Mode.
// It rejects unknown values and the contradiction of htmlOnly with output original, before any request is sent.
func ParseMode(output string, htmlOnly bool) (Mode, error) {
	switch output {
	case "", "original", "compact":
	default:
		return Original, fmt.Errorf("output must be original or compact")
	}
	switch {
	case htmlOnly && output == "original":
		return Original, fmt.Errorf("compact and output options conflict")
	case output == "compact":
		return Compact, nil
	case htmlOnly:
		return HTMLOnly, nil
	}
	return Original, nil
}

// Apply presents body (with its Content-Type) in the mode. Callers keep binary bodies away from it.
// A response that cannot be converted is an error rather than a silently different body.
func (m Mode) Apply(body []byte, contentType string) ([]byte, error) {
	if m == Original {
		return body, nil
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	isXML := mediaType == "application/xml" || mediaType == "text/xml" || strings.HasSuffix(mediaType, "+xml")
	switch {
	case m == Compact && isXML:
		return body, nil
	case mediaType == "text/html" || LooksLikeHTML(body):
		text, err := HTMLToText(body)
		return []byte(text), err
	case m == HTMLOnly:
		return body, nil
	case json.Valid(body):
		var b bytes.Buffer
		err := json.Compact(&b, body)
		return b.Bytes(), err
	case mediaType == "application/json", strings.HasSuffix(mediaType, "+json"):
		return nil, fmt.Errorf("response received but JSON compaction failed: invalid JSON; do not replay a write")
	}
	return body, nil
}
