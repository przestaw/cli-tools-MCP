// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workctl/internal/config"
	"workctl/internal/generic"
	"workctl/internal/issue"
	"workctl/internal/mcp"
	"workctl/internal/pr"
)

// maxBatch bounds one batch so the question stays readable.
const maxBatch = 20

var batchTool = mcp.Tool{
	Name:        "batch",
	Description: "Run several writes (for example Jenkins builds through request POST, comments, uploads) after one approval instead of one question per write. Every call is checked first; then they run in order. Reads are refused here: call them directly. Needs policy.allow_write=true.",
	InputSchema: mcp.ObjSchema(map[string]any{
		"calls": map[string]any{"type": "array", "description": fmt.Sprintf("1 to %d writes, each {\"tool\": name, \"args\": {...}} as for the tool itself", maxBatch),
			"items": map[string]any{"type": "object", "properties": map[string]any{"tool": mcp.StrProp("Tool name"), "args": map[string]any{"type": "object"}}, "required": []string{"tool"}}},
		"stop_on_error": mcp.BoolProp("Skip the remaining calls after the first failure (default true)"),
	}, "calls"),
}

type batchCall struct {
	Tool string         `json:"tool"`
	Args map[string]any `json:"args"`
}

// callBatch checks every call, asks once for all of them, then runs them in order. Nothing runs when a call is
// invalid or the user declines; a write that already ran is not undone when a later one fails.
func callBatch(ctx context.Context, cfg *config.Config, a map[string]any) ([]byte, error) {
	if err := cfg.RequireWrite(); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a["calls"])
	if err != nil {
		return nil, err
	}
	var calls []batchCall
	if err := json.Unmarshal(raw, &calls); err != nil || len(calls) == 0 || len(calls) > maxBatch {
		return nil, fmt.Errorf("calls must be a list of 1 to %d {\"tool\", \"args\"} objects", maxBatch)
	}
	lines := make([]string, len(calls))
	ask := cfg.Policy.ConfirmWrites || mcp.ArgBool(a, "confirm")
	for i := range calls {
		c := &calls[i]
		if c.Args == nil {
			c.Args = map[string]any{}
		}
		switch {
		case c.Tool == "batch":
			return nil, fmt.Errorf("calls[%d]: a batch cannot contain a batch", i)
		case !listed(cfg, c.Tool):
			return nil, fmt.Errorf("calls[%d]: unknown tool %q, or its backend is not configured", i, c.Tool)
		case !isWrite(cfg, c.Tool, c.Args):
			return nil, fmt.Errorf("calls[%d]: %s does not write; call it directly", i, c.Tool)
		}
		if err := validateBatchCall(cfg, *c); err != nil {
			return nil, fmt.Errorf("calls[%d]: %w", i, err)
		}
		ask = ask || mcp.ArgBool(c.Args, "confirm")
		args, _ := json.Marshal(shortArgs(c.Args, 120))
		lines[i] = fmt.Sprintf("%d. %s %s", i+1, c.Tool, args)
	}
	if ask {
		msg := fmt.Sprintf("workctl: the agent wants to run %d writes, in this order; one that already ran is not undone if a later one fails.\n\n%s\n\nApprove all?", len(calls), strings.Join(lines, "\n"))
		if err := mcp.Confirm(ctx, msg); err != nil {
			return nil, err
		}
	}
	stop := true
	if v, ok := a["stop_on_error"].(bool); ok {
		stop = v
	}
	results := make([]map[string]any, len(calls))
	failed := false
	for i, c := range calls {
		r := map[string]any{"tool": c.Tool}
		results[i] = r
		if failed && stop || ctx.Err() != nil {
			r["status"] = "skipped"
			continue
		}
		out, err := dispatch(ctx, cfg, c.Tool, c.Args)
		if err != nil {
			failed = true
			r["status"], r["error"] = "failed", err.Error()
			continue
		}
		r["status"] = "ok"
		if json.Valid(out) {
			r["result"] = json.RawMessage(out)
		} else {
			r["result"] = string(out)
		}
	}
	return json.MarshalIndent(results, "", "  ")
}

func validateBatchCall(cfg *config.Config, call batchCall) error {
	for _, tool := range mcpTools(cfg) {
		if tool.Name != call.Tool {
			continue
		}
		required, _ := tool.InputSchema["required"].([]string)
		for _, key := range required {
			value, ok := call.Args[key]
			if !ok || value == nil || value == "" {
				return fmt.Errorf("%s requires %s", call.Tool, key)
			}
		}
		properties := tool.InputSchema["properties"].(map[string]any)
		for key, value := range call.Args {
			property, ok := properties[key].(map[string]any)
			if !ok {
				return fmt.Errorf("%s: unknown argument %s", call.Tool, key)
			}
			if property["type"] == "string" {
				if _, ok := value.(string); !ok {
					return fmt.Errorf("%s: %s must be a string", call.Tool, key)
				}
			}
		}
	}
	if name := mcp.ArgString(call.Args, "profile"); name != "" {
		profile, ok := cfg.Profiles[name]
		if !ok {
			return fmt.Errorf("profile %q is not configured", name)
		}
		if profile.ReadOnly {
			return fmt.Errorf("profile %q is read-only", name)
		}
	}
	if call.Tool == "request" {
		return generic.ValidateRequest(cfg, call.Args)
	}
	switch call.Tool {
	case "pr_create", "pr_edit", "comment", "upload":
		return pr.ValidateWrite(cfg, call.Tool, call.Args)
	case "issue_update":
		_, _, err := issue.ResolveProfile(cfg, mcp.ArgString(call.Args, "profile"), mcp.ArgString(call.Args, "key"))
		return err
	}
	return nil
}
