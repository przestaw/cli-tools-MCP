// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestObjSchema(t *testing.T) {
	s := ObjSchema(map[string]any{"a": StrProp("x")}, "a")
	if s["type"] != "object" || s["additionalProperties"] != false {
		t.Errorf("schema = %v", s)
	}
	if req, ok := s["required"].([]string); !ok || len(req) != 1 || req[0] != "a" {
		t.Errorf("required = %v", s["required"])
	}
	if _, has := ObjSchema(nil)["required"]; has {
		t.Error("a schema without required fields must not list any")
	}
}

func TestArgReaders(t *testing.T) {
	args := map[string]any{
		"s": "text", "n": float64(3), "i": 4, "j": json.Number("5"), "bad": "x", "b": true,
	}
	if ArgString(args, "s") != "text" || ArgString(args, "n") != "" || ArgString(args, "none") != "" {
		t.Error("ArgString")
	}
	for key, want := range map[string]int{"n": 3, "i": 4, "j": 5, "bad": 9, "none": 9} {
		if got := ArgInt(args, key, 9); got != want {
			t.Errorf("ArgInt(%s) = %d, want %d", key, got, want)
		}
	}
	if !ArgBool(args, "b") || ArgBool(args, "s") || ArgBool(args, "none") {
		t.Error("ArgBool")
	}
}

func TestArgStrings(t *testing.T) {
	got, err := ArgStrings(map[string]any{"args": []any{"a", float64(1), 2.5, true}}, "args")
	if err != nil || strings.Join(got, "|") != "a|1|2.5|true" {
		t.Fatalf("got %v, err %v", got, err)
	}
	if _, err := ArgStrings(map[string]any{"args": "not an array"}, "args"); err == nil {
		t.Fatal("a string is not an array")
	}
	if got, err := ArgStrings(map[string]any{}, "args"); err != nil || got != nil {
		t.Fatalf("absent args = %v, %v", got, err)
	}
}

// serveLines runs the protocol loop on the given request lines and returns one decoded response per line.
func serveLines(t *testing.T, s Server, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	if err := Serve(context.Background(), s, strings.NewReader(strings.Join(lines, "\n")), &out); err != nil {
		t.Fatal(err)
	}
	var responses []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("response %q: %v", line, err)
		}
		responses = append(responses, m)
	}
	return responses
}

// toolText returns the text of a tools/call response and whether it is flagged as an error.
func toolText(t *testing.T, response map[string]any) (text string, isError bool) {
	t.Helper()
	result := response["result"].(map[string]any)
	content := result["content"].([]any)[0].(map[string]any)
	isError, _ = result["isError"].(bool)
	return content["text"].(string), isError
}

func TestServeUsesTheInjectedTools(t *testing.T) {
	server := Server{
		Name: "demo", Version: "9.9", MaxOutputBytes: 8,
		Tools: func() []Tool { return []Tool{{Name: "echo", InputSchema: ObjSchema(nil)}} },
		Call: func(_ context.Context, name string, args map[string]any) ([]byte, error) {
			switch name {
			case "echo":
				return []byte(ArgString(args, "text")), nil
			default:
				return nil, errors.New("boom")
			}
		},
	}
	got := serveLines(t, server,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"echo","arguments":{"text":"hi"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"other"}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"echo","arguments":{"text":"0123456789"}}}`,
	)
	if len(got) != 5 {
		t.Fatalf("got %d responses", len(got))
	}
	info := got[0]["result"].(map[string]any)["serverInfo"].(map[string]any)
	if info["name"] != "demo" || info["version"] != "9.9" {
		t.Errorf("serverInfo = %v", info)
	}
	if tools := got[1]["result"].(map[string]any)["tools"].([]any); len(tools) != 1 {
		t.Errorf("tools = %v", tools)
	}
	if text, isErr := toolText(t, got[2]); text != "hi" || isErr {
		t.Errorf("echo = %q, error=%v", text, isErr)
	}
	if text, isErr := toolText(t, got[3]); text != "boom" || !isErr {
		t.Errorf("failing tool = %q, error=%v (a tool error is a result, not a protocol error)", text, isErr)
	}
	if text, _ := toolText(t, got[4]); !strings.HasPrefix(text, "01234567\n") || !strings.Contains(text, "truncated") {
		t.Errorf("output not truncated to 8 bytes: %q", text)
	}
}
