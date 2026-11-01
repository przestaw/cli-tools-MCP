package mcp

import (
	"strings"
	"testing"
)

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
