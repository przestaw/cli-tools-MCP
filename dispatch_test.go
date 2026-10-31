package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestCommandRegistry(t *testing.T) {
	handlers := commandHandlers()
	for _, name := range []string{"jira", "gitlab", "github", "pr", "mr", "task", "mcp", "doctor", "git-credential", "azure", "atlassian", "bitbucket", "asana"} {
		if handlers[name] == nil {
			t.Errorf("missing handler %s", name)
		}
	}
	cfg := defaultConfig()
	for _, name := range []string{"pr", "mr", "task", "unknown"} {
		err := dispatchCommand(cfg, name, nil)
		if err == nil {
			t.Errorf("%s should require arguments or reject command", name)
		}
	}
	if err := dispatchCommand(cfg, "unknown", nil); err == nil || !strings.Contains(err.Error(), `unknown command "unknown"`) {
		t.Fatal(err)
	}
}

func TestMCPProtocolDispatch(t *testing.T) {
	input := strings.Join([]string{
		`{`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":[]}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"unknown"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"missing"}`,
		`{"jsonrpc":"2.0","id":7,"method":"server/discover"}`,
	}, "\n")
	var output bytes.Buffer
	if err := serveMCP(context.Background(), defaultConfig(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 8 {
		t.Fatalf("unexpected responses: %s", output.String())
	}
	for index, code := range []int{-32700, 0, 0, 0, -32602, 0, -32601, -32601} {
		var resp rpcResponse
		if err := json.Unmarshal([]byte(lines[index]), &resp); err != nil {
			t.Fatal(err)
		}
		if code != 0 && (resp.Error == nil || resp.Error.Code != code) || code == 0 && resp.Error != nil {
			t.Fatalf("response %d: %+v", index, resp)
		}
	}
	if !strings.Contains(lines[1], `"protocolVersion":"2024-11-05"`) || !strings.Contains(lines[5], `"isError":true`) {
		t.Fatal(output.String())
	}
}

func TestMCPTruncationAndPolicy(t *testing.T) {
	calls := fakeChildProcess(t)
	cfg := defaultConfig()
	cfg.MCP.MaxOutputBytes = 5
	result, err := callToolRPC(context.Background(), cfg, json.RawMessage(`{"name":"azure","arguments":{"args":["pr","get",1]}}`))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(result)
	if !strings.Contains(string(body), "ARGS:") || !strings.Contains(string(body), "output truncated") {
		t.Fatal(string(body))
	}
	before := len(*calls)
	result, err = callToolRPC(context.Background(), cfg, json.RawMessage(`{"name":"azure","arguments":{"args":["pr","create"]}}`))
	if err != nil || len(*calls) != before {
		t.Fatalf("policy failed: %v", err)
	}
	body, _ = json.Marshal(result)
	if !strings.Contains(string(body), `"isError":true`) || !strings.Contains(string(body), "allow_write") {
		t.Fatal(string(body))
	}
}

type rpcFailedWriter struct{}

func (rpcFailedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestMCPPropagatesOutputErrors(t *testing.T) {
	for _, input := range []string{`{`, `{"id":1,"method":"ping"}`, `{"id":2,"method":"unknown"}`, `{"id":3,"method":"server/discover"}`} {
		err := serveMCP(context.Background(), defaultConfig(), strings.NewReader(input), rpcFailedWriter{})
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("input=%s err=%v", input, err)
		}
	}
}
