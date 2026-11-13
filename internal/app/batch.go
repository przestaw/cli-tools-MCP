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

type batchResult struct {
	Tool   string          `json:"tool"`
	Status string          `json:"status"` // ok, failed or skipped
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
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
		tool, ok := findTool(cfg, c.Tool)
		switch {
		case c.Tool == "batch":
			return nil, fmt.Errorf("calls[%d]: a batch cannot contain a batch", i)
		case !ok:
			return nil, fmt.Errorf("calls[%d]: unknown tool %q, or its backend is not configured", i, c.Tool)
		case !isWrite(cfg, c.Tool, c.Args):
			return nil, fmt.Errorf("calls[%d]: %s does not write; call it directly", i, c.Tool)
		}
		if err := validateBatchCall(cfg, tool, c.Args); err != nil {
			return nil, fmt.Errorf("calls[%d]: %w", i, err)
		}
		ask = ask || mcp.ArgBool(c.Args, "confirm")
		args, _ := json.Marshal(shortArgs(c.Args, 120))
		lines[i] = fmt.Sprintf("%d. %s %s", i+1, c.Tool, args)
	}
	msg := fmt.Sprintf("workctl: the agent wants to run %d writes, in this order; one that already ran is not undone if a later one fails.\n\n%s\n\nApprove all?", len(calls), strings.Join(lines, "\n"))
	if cfg.Policy.ConfirmSessionWrites {
		if err := mcp.ConfirmSession(ctx, strings.Replace(msg, "Approve all?", "Approve this session?", 1)); err != nil {
			return nil, err
		}
	}
	if ask {
		if err := mcp.Confirm(ctx, msg); err != nil {
			return nil, err
		}
	}
	stop := true
	if v, ok := a["stop_on_error"].(bool); ok {
		stop = v
	}
	results := make([]batchResult, len(calls))
	failed := false
	for i, c := range calls {
		r := &results[i]
		r.Tool = c.Tool
		if failed && stop || ctx.Err() != nil {
			r.Status = "skipped"
			continue
		}
		out, err := dispatch(ctx, cfg, c.Tool, c.Args)
		if err != nil {
			failed = true
			r.Status, r.Error = "failed", err.Error()
			continue
		}
		r.Status, r.Result = "ok", out
		if !json.Valid(out) {
			r.Result, _ = json.Marshal(string(out))
		}
	}
	return json.MarshalIndent(results, "", "  ")
}

// validateBatchCall checks one call against its tool schema and the profile policy, locally and before anything runs.
func validateBatchCall(cfg *config.Config, tool mcp.Tool, args map[string]any) error {
	required, _ := tool.InputSchema["required"].([]string)
	for _, key := range required {
		if v, ok := args[key]; !ok || v == nil || v == "" {
			return fmt.Errorf("%s requires %s", tool.Name, key)
		}
	}
	properties, _ := tool.InputSchema["properties"].(map[string]any)
	for key, value := range args {
		property, ok := properties[key].(map[string]any)
		if !ok {
			return fmt.Errorf("%s: unknown argument %s", tool.Name, key)
		}
		if _, isString := value.(string); property["type"] == "string" && !isString {
			return fmt.Errorf("%s: %s must be a string", tool.Name, key)
		}
	}
	if name := mcp.ArgString(args, "profile"); name != "" {
		profile, ok := cfg.Profiles[name]
		if !ok {
			return fmt.Errorf("profile %q is not configured", name)
		}
		if profile.ReadOnly {
			return fmt.Errorf("profile %q is read-only", name)
		}
	}
	switch tool.Name {
	case "request":
		return generic.ValidateRequest(cfg, args)
	case "pr_create", "pr_edit", "comment", "upload":
		return pr.ValidateWrite(cfg, tool.Name, args)
	case "issue_update":
		_, _, err := issue.ResolveProfile(cfg, mcp.ArgString(args, "profile"), mcp.ArgString(args, "key"))
		return err
	}
	return nil
}
