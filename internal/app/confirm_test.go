// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"workctl/internal/config"
	"workctl/internal/mcp"
	"workctl/internal/testutil"
)

func TestIsWriteClassifiesEveryWritingCall(t *testing.T) {
	cfg := config.Default()
	cases := []struct {
		name  string
		args  map[string]any
		write bool
	}{
		{"pr_show", map[string]any{"number": 1}, false},
		{"pr_create", map[string]any{}, true},
		{"pr_edit", map[string]any{}, true},
		{"comment", map[string]any{"body": "x"}, true},
		{"issue_update", map[string]any{"key": "A-1"}, true},
		{"issue_update", map[string]any{"key": "A-1", "dry_run": true}, false},
		{"request", map[string]any{"path": "/x"}, false},
		{"request", map[string]any{"path": "/x", "method": "head"}, false},
		{"request", map[string]any{"path": "/job/x/build", "method": "post"}, true},
		{"download", map[string]any{"path": "/x"}, false},
		{"cli", map[string]any{"module": "azure", "args": []any{"pr", "list"}}, false},
		{"cli", map[string]any{"module": "ado", "args": []any{"pr", "create"}}, true},
		{"accounts", map[string]any{}, false},
		{"accounts", map[string]any{"bind_profile": "gh", "bind_repo": "o/*"}, true},
		{"ci", map[string]any{"ref": "main"}, false},
		{"task_get_context", map[string]any{"key": "A-1"}, false},
		{"upload", map[string]any{"path": "/tmp/a.png"}, true},
		{"a_tool_added_later", map[string]any{}, true},
	}
	for _, c := range cases {
		if got := isWrite(cfg, c.name, c.args); got != c.write {
			t.Errorf("%s %v: write=%v, want %v", c.name, c.args, got, c.write)
		}
	}
}

// jenkinsLike is a config whose api profile records every request, with writes allowed and confirmation on.
func jenkinsLike(t *testing.T) (*config.Config, *[]testutil.Request) {
	t.Helper()
	t.Setenv("TEST_JENKINS_TOKEN", "secret")
	base, seen := testutil.RecordingServer(t, 201, ``)
	cfg := config.Default()
	cfg.Profiles["jenkins"] = config.Profile{Kind: "api", URL: base, TokenEnv: "TEST_JENKINS_TOKEN", VerifyTLS: true}
	cfg.Policy.AllowWrite, cfg.Policy.ConfirmWrites = true, true
	return cfg, seen
}

// buildOverMCP calls request POST /job/x/build through the MCP loop; the scripted client answers an elicitation
// with answer (empty: the client does not support elicitation).
func buildOverMCP(t *testing.T, cfg *config.Config, answer string) (text, asked string) {
	t.Helper()
	return callOverMCP(t, cfg, answer, `{"name":"request","arguments":{"profile":"jenkins","method":"POST","path":"/job/x/build"}}`)
}

// callOverMCP runs one tools/call (params JSON) through the MCP loop and returns its text and the question asked.
func callOverMCP(t *testing.T, cfg *config.Config, answer, call string) (text, asked string) {
	t.Helper()
	var line bytes.Buffer // one message per line: the test calls are written over several lines
	if err := json.Compact(&line, []byte(call)); err != nil {
		t.Fatal(err)
	}
	call = line.String()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- mcp.Serve(context.Background(), mcpServer(cfg), inR, outW); _ = outW.Close() }()
	caps := `{}`
	if answer != "" {
		caps = `{"elicitation":{}}`
	}
	write := func(line string) { _, _ = io.WriteString(inW, line+"\n") }
	go func() {
		write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":` + caps + `}}`)
		write(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":` + call + `}`)
	}()
	out := bufio.NewScanner(outR)
	for out.Scan() {
		var msg struct {
			ID     json.RawMessage
			Method string
			Params struct{ Message string }
			Result struct{ Content []struct{ Text string } }
		}
		if err := json.Unmarshal(out.Bytes(), &msg); err != nil {
			t.Fatal(err)
		}
		switch {
		case msg.Method == "elicitation/create":
			asked = msg.Params.Message
			if answer == "cancel" { // the client gives up on the tool call instead of answering
				go write(`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":2}}`)
				continue
			}
			go write(`{"jsonrpc":"2.0","id":` + string(msg.ID) + `,"result":` + answer + `}`)
		case string(msg.ID) == "2":
			_ = inW.Close()
			for out.Scan() {
			}
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			return msg.Result.Content[0].Text, asked
		}
	}
	t.Fatal("no answer to the tool call")
	return "", ""
}

func TestWritesWaitForTheUsersApproval(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	text, asked := buildOverMCP(t, cfg, `{"action":"decline"}`)
	if !strings.Contains(text, "declined") || len(*seen) != 0 || !strings.Contains(asked, "/job/x/build") {
		t.Fatalf("declined: text=%q asked=%q requests=%v", text, asked, *seen)
	}
	if text, _ := buildOverMCP(t, cfg, ""); !strings.Contains(text, "cannot ask the user") || len(*seen) != 0 {
		t.Fatalf("client without elicitation: text=%q requests=%v", text, *seen)
	}
	if _, _ = buildOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`); len(*seen) != 1 || (*seen)[0].Method != "POST" {
		t.Fatalf("approved: requests=%v", *seen)
	}
	cfg.Policy.ConfirmWrites = false
	if _, asked := buildOverMCP(t, cfg, `{"action":"decline"}`); asked != "" || len(*seen) != 2 {
		t.Fatalf("confirm_writes off must not ask: asked=%q requests=%v", asked, *seen)
	}
}
