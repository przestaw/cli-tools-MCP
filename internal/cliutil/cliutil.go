// Package cliutil holds small helpers shared by the command implementations:
// output printing, flag-argument parsing and the replaceable stdin of the wizards.
package cliutil

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strconv"
	"strings"
)

// Stdin is where the wizards read answers and piped tokens from; tests replace it.
var Stdin io.Reader = os.Stdin

// FirstNonEmpty returns the first non-empty string.
func FirstNonEmpty(items ...string) string {
	for _, s := range items {
		if s != "" {
			return s
		}
	}
	return ""
}

// MustPositive returns n when it is positive, else def.
func MustPositive(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

// SplitCommaList splits a comma-separated flag value into trimmed, non-empty items.
func SplitCommaList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if t := strings.TrimSpace(item); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ReorderIDToBack moves a leading integer argument to the back so flag.FlagSet can parse flags that follow it.
func ReorderIDToBack(args []string) []string {
	if len(args) > 1 && !strings.HasPrefix(args[0], "-") {
		if _, err := strconv.Atoi(args[0]); err == nil {
			res := make([]string, 0, len(args))
			res = append(res, args[1:]...)
			res = append(res, args[0])
			return res
		}
	}
	return args
}

// PrettyJSON indents a JSON body for the terminal; anything else passes through unchanged.
func PrettyJSON(b []byte) []byte {
	if !json.Valid(b) {
		return b
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return b
	}
	out.WriteByte('\n')
	return out.Bytes()
}

// PrintBody writes an API response to stdout: JSON indented, anything else with a trailing newline.
func PrintBody(b []byte) error {
	if json.Valid(b) {
		_, err := os.Stdout.Write(PrettyJSON(b))
		return err
	}
	if len(b) > 0 {
		if _, err := os.Stdout.Write(b); err != nil {
			return err
		}
		if b[len(b)-1] != '\n' {
			_, _ = os.Stdout.Write([]byte("\n"))
		}
	}
	return nil
}

// ReadBodyArg reads a --body value: a literal string, or the contents of a file after "@".
func ReadBodyArg(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "@") {
		return os.ReadFile(strings.TrimPrefix(s, "@"))
	}
	return []byte(s), nil
}

// JSONField returns one top-level string field of a JSON object, or "".
func JSONField(b []byte, key string) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}
