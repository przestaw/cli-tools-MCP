// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"workctl/internal/auth"
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
	case "comment":
		return cfg.Has("github") || cfg.Has("gitlab") || issue.Available(cfg)
	case "issue_show", "issue_search", "issue_update":
		return issue.Available(cfg)
	case "request", "download":
		return generic.Available(cfg)
	case "api_exec":
		return cfg.Has(generic.Kind)
	case "task_get_context":
		return cfg.Has("jira") && cfg.Has("gitlab")
	}
	return true
}

// mcpTools composes the MCP tools of the configured backends.
func mcpTools(cfg *config.Config) []mcp.Tool {
	all := append(append(append([]mcp.Tool{}, pr.Tools()...), issue.Tools()...), generic.Tools()...)
	all = append(all, mcp.Tool{Name: "task_get_context", Description: "Get Jira issue plus matching branches and merge requests from configured GitLab projects.", InputSchema: mcp.ObjSchema(map[string]any{"key": mcp.StrProp("Jira issue key")}, "key")})
	tools := append([]mcp.Tool{}, auth.Tools()...)
	for _, t := range all {
		if toolAvailable(cfg, t.Name) {
			tools = append(tools, t)
		}
	}
	return append(tools, modules.Tools(cfg)...)
}

func runMCP(ctx context.Context, cfg *config.Config) error {
	return mcp.Serve(ctx, mcpServer(cfg), os.Stdin, os.Stdout)
}

func mcpServer(cfg *config.Config) mcp.Server {
	return mcp.Server{
		Name:           "workctl",
		Version:        Version,
		Instructions:   "Provider data for GitHub, GitLab, Azure DevOps, Jira Server and Jira/Confluence Cloud through one account per profile: pull/merge requests (pr_list, pr_show with include, pr_create, pr_edit), CI (ci), repository files (file), comments (comment), issues (issue_show, issue_search, issue_update), any other endpoint or attachment (request, download) and the embedded CLIs (cli). Prefer these tools over gh, glab, curl or a browser. Call accounts to see which profile serves a resource. Everything is read-only unless policy.allow_write=true in the config file.",
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
	listed := false
	for _, t := range mcpTools(cfg) {
		listed = listed || t.Name == name
	}
	if !listed {
		return nil, fmt.Errorf("unknown tool %q, or its backend is not configured", name)
	}
	switch {
	case strings.HasPrefix(name, "issue_"):
		return issue.CallTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "pr_"), name == "ci", name == "file", name == "comment":
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
