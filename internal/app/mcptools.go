// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"workctl/internal/api"
	"workctl/internal/auth"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/generic"
	"workctl/internal/issue"
	"workctl/internal/mcp"
	"workctl/internal/modules"
	"workctl/internal/pr"
)

// toolAvailable reports whether the backend of an MCP tool is configured.
func toolAvailable(cfg *config.Config, name string) bool {
	switch name {
	case "pr_list", "pr_show":
		return cfg.Has("github") || cfg.Has("gitlab") || cfg.Has("azure")
	case "pr_create", "pr_edit", "ci", "file":
		return cfg.Has("github") || cfg.Has("gitlab")
	case "comment", "upload":
		return cfg.Has("github") || cfg.Has("gitlab") || issue.Available(cfg)
	case "issue_show", "issue_search", "issue_update":
		return issue.Available(cfg)
	case "request", "download":
		return generic.Available(cfg)
	case "api_exec":
		return cfg.Has(generic.Kind)
	case "task_get_context":
		return cfg.Has("jira") && cfg.Has("gitlab")
	case "batch":
		return cfg.Policy.AllowWrite // it runs only writes
	}
	return true
}

// mcpTools composes the MCP tools of the configured backends.
func mcpTools(cfg *config.Config) []mcp.Tool {
	all := append(append(append([]mcp.Tool{}, pr.Tools()...), issue.Tools()...), generic.Tools()...)
	all = append(all, mcp.Tool{Name: "task_get_context", Description: "Get Jira issue plus matching branches and merge requests from configured GitLab projects.", InputSchema: mcp.ObjSchema(map[string]any{"key": mcp.StrProp("Jira issue key")}, "key")})
	all = append(all, batchTool)
	tools := append([]mcp.Tool{}, auth.Tools()...)
	for _, t := range all {
		if toolAvailable(cfg, t.Name) {
			tools = append(tools, t)
		}
	}
	tools = append(tools, modules.Tools(cfg)...)
	for i, tool := range tools {
		schema := map[string]any{}
		for key, value := range tool.InputSchema {
			schema[key] = value
		}
		properties := map[string]any{}
		for key, value := range tool.InputSchema["properties"].(map[string]any) {
			properties[key] = value
		}
		properties["confirm"] = mcp.BoolProp("Ask for approval for this write or entire batch even if policy.confirm_writes is off; false never overrides policy")
		schema["properties"] = properties
		tools[i].InputSchema = schema
	}
	return tools
}

func runMCP(ctx context.Context, cfg *config.Config) error {
	return mcp.Serve(ctx, mcpServer(cfg), os.Stdin, os.Stdout)
}

func mcpServer(cfg *config.Config) mcp.Server {
	return mcp.Server{
		Name:           "workctl",
		Version:        Version,
		Instructions:   "Provider data for GitHub, GitLab, Azure DevOps, Jira Server and Jira/Confluence Cloud through one account per profile: pull/merge requests (pr_list, pr_show with include, pr_create, pr_edit), CI (ci), repository files (file), comments (comment), issues (issue_show, issue_search, issue_update), any other endpoint or attachment (request, download) and the embedded CLIs (cli). Prefer these tools over gh, glab, curl or a browser. Call accounts to see which profile serves a resource. Everything is read-only unless policy.allow_write=true in the config file. Use batch for the known writes of one user goal, with one optional approval for the entire list. Request confirm=true for an operation the user considers high risk. After a declined, cancelled or refused write, stop: never bypass through CLI, another profile, or configuration/policy changes.",
		MaxOutputBytes: cfg.MCP.MaxOutputBytes,
		Tools:          func() []mcp.Tool { return mcpTools(cfg) },
		Call: func(ctx context.Context, name string, args map[string]any) ([]byte, error) {
			return callMCPTool(ctx, cfg, name, args)
		},
	}
}

// callMCPTool runs one MCP tool. Only listed tools are callable. Provider clients are made per call, so a missing
// login fails only that provider's tools.
func callMCPTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	if !listed(cfg, name) {
		return nil, fmt.Errorf("unknown tool %q, or its backend is not configured", name)
	}
	if name == "batch" {
		return callBatch(ctx, cfg, a)
	}
	if cfg.Policy.AllowWrite && (cfg.Policy.ConfirmWrites || mcp.ArgBool(a, "confirm")) && isWrite(cfg, name, a) {
		if err := mcp.Confirm(ctx, describeCall(name, a)); err != nil {
			return nil, err
		}
	}
	return dispatch(ctx, cfg, name, a)
}

func listed(cfg *config.Config, name string) bool {
	for _, t := range mcpTools(cfg) {
		if t.Name == name {
			return true
		}
	}
	return false
}

// dispatch runs one tool without asking: callMCPTool and callBatch ask first.
func dispatch(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	switch {
	case strings.HasPrefix(name, "issue_"):
		return issue.CallTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "pr_"), name == "ci", name == "file", name == "comment", name == "upload":
		return pr.CallTool(ctx, cfg, name, a)
	case name == "request", name == "download", name == "api_exec":
		return generic.CallTool(ctx, cfg, name, a)
	}
	switch name {
	case "accounts":
		return auth.CallTool(ctx, cfg, name, a)
	case "cli":
		return modules.CallCLITool(ctx, cfg, a)
	case "task_get_context":
		return taskContextJSON(ctx, cfg, mcp.ArgString(a, "key"))
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

// isWrite reports whether one tool call changes something on a server or in the configuration. Only the calls listed
// here as reads skip the question, so a tool added later is asked about until it is classified.
func isWrite(cfg *config.Config, name string, a map[string]any) bool {
	switch name {
	case "pr_list", "pr_show", "ci", "file", "issue_show", "issue_search", "download", "task_get_context":
		return false
	case "issue_update":
		return !mcp.ArgBool(a, "dry_run")
	case "request":
		return !api.IsReadMethod(strings.ToUpper(cliutil.FirstNonEmpty(mcp.ArgString(a, "method"), "GET")))
	case "api_exec":
		return generic.ExecIsWrite(cfg, a)
	case "cli":
		return modules.IsWrite(a)
	case "accounts":
		return mcp.ArgString(a, "bind_profile") != ""
	}
	return true
}

// describeCall is the question the user sees: the tool and its arguments, long values shortened.
func describeCall(name string, a map[string]any) string {
	args, _ := json.MarshalIndent(shortArgs(a, 300), "", "  ")
	return fmt.Sprintf("workctl: the agent wants to run %s, which changes something.\n\n%s\n\nApprove?", name, args)
}

// shortArgs shortens long text values by characters (not bytes, so a Polish letter is never cut) and says how long
// the whole value is, so the user knows the question shows only its start.
func shortArgs(a map[string]any, limit int) map[string]any {
	short := map[string]any{}
	for k, v := range a {
		if s, ok := v.(string); ok {
			if r := []rune(s); len(r) > limit {
				v = fmt.Sprintf("%s... (%d characters in total)", string(r[:limit]), len(r))
			}
		}
		short[k] = v
	}
	return short
}
