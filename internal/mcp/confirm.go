// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"
)

// ConfirmTimeout bounds how long a confirmation waits for the user.
var ConfirmTimeout = 10 * time.Minute

// session is one client connection: the shared output and the server's own requests waiting for an answer.
type session struct {
	mu          sync.Mutex // guards out
	out         *bufio.Writer
	pmu         sync.Mutex // guards the fields below
	pending     map[string]chan rpcRequest
	running     map[string]context.CancelFunc // tool calls in flight, by the raw JSON of their id
	next        int
	elicitation bool
	approvalMu  sync.Mutex
	approved    bool
	closed      chan struct{} // closed when the client's input ends
}

type sessionKey struct{}

type serverRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      string `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

func (s *session) send(v any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if resp, ok := v.(rpcResponse); ok {
		return sendRPC(s.out, resp)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := s.out.Write(append(b, '\n')); err != nil {
		return err
	}
	return s.out.Flush()
}

func (s *session) setElicitation(on bool) {
	s.pmu.Lock()
	s.elicitation = on
	s.pmu.Unlock()
}

// track gives one tool call a context that notifications/cancelled can end; done forgets it.
func (s *session) track(ctx context.Context, id json.RawMessage) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	s.pmu.Lock()
	s.running[string(id)] = cancel
	s.pmu.Unlock()
	return ctx, func() {
		s.pmu.Lock()
		delete(s.running, string(id))
		s.pmu.Unlock()
		cancel()
	}
}

// cancel ends the tool call named by a notifications/cancelled, including a question it is waiting on.
func (s *session) cancel(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	s.pmu.Lock()
	cancel := s.running[string(p.RequestID)]
	s.pmu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// answer hands a client's response to the request waiting for it; unknown ids are dropped.
func (s *session) answer(resp rpcRequest) {
	var id string
	if json.Unmarshal(resp.ID, &id) != nil {
		id = string(resp.ID)
	}
	s.pmu.Lock()
	ch := s.pending[id]
	delete(s.pending, id)
	s.pmu.Unlock()
	if ch != nil {
		ch <- resp
	}
}

// request sends a request to the client and waits for its answer.
func (s *session) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.pmu.Lock()
	s.next++
	id := "workctl-" + strconv.Itoa(s.next)
	ch := make(chan rpcRequest, 1)
	s.pending[id] = ch
	s.pmu.Unlock()
	drop := func() {
		s.pmu.Lock()
		delete(s.pending, id)
		s.pmu.Unlock()
	}
	if err := s.send(serverRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params}); err != nil {
		drop()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, ConfirmTimeout)
	defer cancel()
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, resp.Error.Message)
		}
		return resp.Result, nil
	case <-ctx.Done():
		drop()
		return nil, fmt.Errorf("%s: no answer from the user: %w", method, ctx.Err())
	case <-s.closed:
		drop()
		return nil, fmt.Errorf("%s: the client closed the connection", method)
	}
}

// ErrDeclined is returned by Confirm when the user said no.
var ErrDeclined = errors.New("the user declined; do not retry without asking them first")

// Confirm asks the user, through the MCP client, to approve one action described by message. It returns nil only
// after an explicit approval. Outside an MCP call (the CLI, where the user typed the command) it returns nil.
// A client that cannot ask (no elicitation capability) gets an error: the action is refused, not done silently.
func Confirm(ctx context.Context, message string) error {
	sess, ok := ctx.Value(sessionKey{}).(*session)
	if !ok {
		return nil
	}

	sess.pmu.Lock()
	can := sess.elicitation
	sess.pmu.Unlock()
	if !can {
		return fmt.Errorf("write confirmation is on but this MCP client cannot ask the user (no elicitation support); stop and report this to the user, do not bypass through CLI or configuration changes")
	}
	asked := time.Now()
	raw, err := sess.request(ctx, "elicitation/create", map[string]any{
		"message": message,
		"requestedSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"approve": map[string]any{"type": "boolean", "title": "Approve", "description": "Run this change", "default": true},
			},
			"required": []string{"approve"},
		},
	})
	if err != nil {
		return err
	}
	var r struct {
		Action  string `json:"action"`
		Content struct {
			Approve *bool `json:"approve"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("elicitation: %w", err)
	}
	if r.Action != "accept" || r.Content.Approve == nil || !*r.Content.Approve {
		return declinedBy(r.Action, r.Content.Approve != nil, time.Since(asked))
	}
	return nil
}

// quickAnswer is faster than a person can read a prompt: such an answer came from the client, not from the user.
const quickAnswer = 500 * time.Millisecond

// declinedBy wraps ErrDeclined with what the client actually answered, so a user can tell a refusal from a client
// that never showed the prompt. Every answer but an explicit approval still ends the action.
func declinedBy(action string, answered bool, took time.Duration) error {
	what := fmt.Sprintf("the client answered %q", action)
	switch {
	case action == "accept" && !answered:
		what = "the client accepted the form without the approval field"
	case action == "accept":
		what = "the client accepted the form with approval switched off"
	case action == "cancel":
		what = "the client cancelled the prompt (dismissed, or never shown)"
	case action == "decline":
		what = "the client declined the prompt"
	}
	hint := ""
	if took < quickAnswer {
		hint = "; it answered in " + took.Round(time.Millisecond).String() + ", so no person was asked: this client may answer elicitation requests automatically"
	}
	return fmt.Errorf("%w (%s%s)", ErrDeclined, what, hint)
}

// ConfirmSession asks once per MCP connection. A later explicit Confirm remains a separate detailed approval.
func ConfirmSession(ctx context.Context, message string) error {
	sess, ok := ctx.Value(sessionKey{}).(*session)
	if !ok {
		return nil
	}
	sess.approvalMu.Lock()
	defer sess.approvalMu.Unlock()
	// A call may have been cancelled while another call was waiting for session approval.
	if err := ctx.Err(); err != nil {
		return err
	}
	if sess.approved {
		return nil
	}
	if err := Confirm(ctx, message); err != nil {
		return err
	}
	sess.approved = true
	return nil
}
