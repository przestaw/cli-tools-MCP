// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cliutil

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitCommaList(t *testing.T) {
	cases := map[string][]string{
		"":              nil,
		"   ":           nil,
		"a":             {"a"},
		" a, b ,, c , ": {"a", "b", "c"},
	}
	for in, want := range cases {
		if got := SplitCommaList(in); !reflect.DeepEqual(got, want) {
			t.Errorf("SplitCommaList(%q) = %#v, want %#v", in, got, want)
		}
	}
}

func TestReorderIDToBack(t *testing.T) {
	cases := []struct{ in, want []string }{
		{[]string{"7", "--repo", "o/r"}, []string{"--repo", "o/r", "7"}},
		{[]string{"--repo", "o/r", "7"}, []string{"--repo", "o/r", "7"}}, // already in order
		{[]string{"7"}, []string{"7"}},                                   // nothing to reorder
		{[]string{"abc", "--x"}, []string{"abc", "--x"}},                 // not a number
		{nil, nil},
	}
	for _, c := range cases {
		if got := ReorderIDToBack(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("ReorderIDToBack(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFirstNonEmptyAndMustPositive(t *testing.T) {
	if got := FirstNonEmpty("", "", "x", "y"); got != "x" {
		t.Errorf("FirstNonEmpty = %q", got)
	}
	if got := FirstNonEmpty(); got != "" {
		t.Errorf("FirstNonEmpty() = %q", got)
	}
	for in, want := range map[int]int{-3: 30, 0: 30, 5: 5} {
		if got := MustPositive(in, 30); got != want {
			t.Errorf("MustPositive(%d, 30) = %d, want %d", in, got, want)
		}
	}
}

func TestPrettyJSON(t *testing.T) {
	if got := string(PrettyJSON([]byte(`{"a":1}`))); got != "{\n  \"a\": 1\n}\n" {
		t.Errorf("valid JSON = %q", got)
	}
	if got := string(PrettyJSON([]byte("plain text"))); got != "plain text" {
		t.Errorf("non-JSON must pass through unchanged, got %q", got)
	}
}

func TestJSONField(t *testing.T) {
	body := []byte(`{"login":"octo","count":3}`)
	if got := JSONField(body, "login"); got != "octo" {
		t.Errorf("login = %q", got)
	}
	for _, key := range []string{"missing", "count"} { // absent and not a string
		if got := JSONField(body, key); got != "" {
			t.Errorf("%s = %q, want empty", key, got)
		}
	}
	if got := JSONField([]byte("not json"), "login"); got != "" {
		t.Errorf("invalid JSON = %q", got)
	}
}

func TestReadBodyArg(t *testing.T) {
	if b, err := ReadBodyArg(""); err != nil || b != nil {
		t.Errorf("empty = %q, %v", b, err)
	}
	if b, _ := ReadBodyArg(`{"x":1}`); string(b) != `{"x":1}` {
		t.Errorf("literal = %q", b)
	}
	file := filepath.Join(t.TempDir(), "body.json")
	if err := os.WriteFile(file, []byte(`{"from":"file"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := ReadBodyArg("@" + file); err != nil || string(b) != `{"from":"file"}` {
		t.Errorf("@file = %q, %v", b, err)
	}
	if _, err := ReadBodyArg("@" + filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("a missing file must be an error")
	}
}

// captureStdout runs fn and returns what it wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	fn()
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestPrintBody(t *testing.T) {
	json := captureStdout(t, func() { _ = PrintBody([]byte(`{"a":1}`)) })
	if json != "{\n  \"a\": 1\n}\n" {
		t.Errorf("JSON output = %q", json)
	}
	text := captureStdout(t, func() { _ = PrintBody([]byte("hello")) })
	if !strings.HasSuffix(text, "\n") || strings.TrimSpace(text) != "hello" {
		t.Errorf("text output = %q, want one trailing newline", text)
	}
	if empty := captureStdout(t, func() { _ = PrintBody(nil) }); empty != "" {
		t.Errorf("empty body printed %q", empty)
	}
}
