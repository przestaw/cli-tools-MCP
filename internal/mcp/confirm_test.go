// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// confirmServer has one tool that asks the user before it answers "done".
func confirmServer() Server {
	return Server{
		Name: "demo", Version: "1",
		Tools: func() []Tool { return []Tool{{Name: "write", InputSchema: ObjSchema(nil)}} },
		Call: func(ctx context.Context, _ string, _ map[string]any) ([]byte, error) {
			if err := Confirm(ctx, "write it?"); err != nil {
				return nil, err
			}
			return []byte("done"), nil
		},
	}
}

// converse runs the server with a scripted client: it sends initialize (with or without elicitation), one tool
// call, answers an elicitation request with answer, and returns the text of the tool result.
func converse(t *testing.T, elicitation bool, answer string) (text string, asked string) {
	t.Helper()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- Serve(context.Background(), confirmServer(), inR, outW); _ = outW.Close() }()
	caps := `{}`
	if elicitation {
		caps = `{"elicitation":{}}`
	}
	write := func(line string) {
		_, _ = io.WriteString(inW, line+"\n") // fails only after the test closed the pipe
	}
	// The pipes are unbuffered: write while the loop below reads, or both sides wait for each other.
	go func() {
		write(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":` + caps + `}}`)
		write(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"write","arguments":{}}}`)
	}()
	out := bufio.NewScanner(outR)
	for out.Scan() {
		var msg struct {
			ID     json.RawMessage
			Method string
			Params struct{ Message string }
			Result struct {
				Content []struct{ Text string }
			}
		}
		if err := json.Unmarshal(out.Bytes(), &msg); err != nil {
			t.Fatal(err)
		}
		switch {
		case msg.Method == "elicitation/create":
			asked = msg.Params.Message
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

func TestConfirmAsksTheUserThroughTheClient(t *testing.T) {
	if text, asked := converse(t, true, `{"action":"accept","content":{"approve":true}}`); text != "done" || asked != "write it?" {
		t.Fatalf("accepted: text=%q asked=%q", text, asked)
	}
	for _, answer := range []string{`{"action":"decline"}`, `{"action":"cancel"}`, `{"action":"accept","content":{"approve":false}}`, `{"action":"accept"}`, `{"action":"accept","content":{}}`} {
		if text, _ := converse(t, true, answer); !strings.Contains(text, "declined") {
			t.Errorf("%s: text=%q", answer, text)
		}
	}
	if text, asked := converse(t, false, ""); asked != "" || !strings.Contains(text, "cannot ask the user") {
		t.Fatalf("client without elicitation: text=%q asked=%q", text, asked)
	}
}

func TestConfirmOutsideMCPIsTheUsersOwnCommand(t *testing.T) {
	if err := Confirm(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
}
