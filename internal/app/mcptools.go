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
	"workctl/internal/github"
	"workctl/internal/gitlab"
	"workctl/internal/issue"
	"workctl/internal/jira"
	"workctl/internal/mcp"
	"workctl/internal/modules"
	"workctl/internal/pr"
)

// toolAvailable reports whether the backend of an MCP tool is configured (Jira and GitLab are each optional).
func toolAvailable(cfg *config.Config, name string) bool {
	switch {
	case name == "pr_list" || name == "pr_show":
		return cfg.Has("github") || cfg.Has("gitlab") || cfg.Has("azure")
	case name == "pr_create" || name == "pr_edit" || name == "ci" || name == "file":
		return cfg.Has("github") || cfg.Has("gitlab")
	case name == "comment":
		return cfg.Has("github") || cfg.Has("gitlab") || issue.Available(cfg)
	case name == "request" || name == "download" || name == "api_request" || name == "api_download":
		return generic.Available(cfg)
	case name == "api_exec":
		return cfg.Has(generic.Kind)
	case strings.HasPrefix(name, "jira_"):
		return cfg.Has("jira")
	case strings.HasPrefix(name, "issue_"):
		return issue.Available(cfg)
	case strings.HasPrefix(name, "gitlab_"):
		return cfg.Has("gitlab")
	case strings.HasPrefix(name, "github_"):
		return cfg.Has("github")
	case strings.HasPrefix(name, "task_"):
		return cfg.Has("jira") && cfg.Has("gitlab")
	case strings.HasPrefix(name, "api_"):
		return cfg.Has(generic.Kind)
	}
	return true
}

// mcpTools composes the listed MCP tools of the configured backends. The provider-specific github_*, gitlab_*,
// jira_*, api_request/api_download, azure and atlassian tools stay callable for one release but are not listed:
// the provider-neutral tools cover them.
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

// callMCPTool runs one MCP tool. Provider clients are made per call, so a missing login fails only that provider's tools.
func callMCPTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	if !toolAvailable(cfg, name) {
		return nil, fmt.Errorf("tool %q is not available: its backend is not configured", name)
	}
	switch {
	case strings.HasPrefix(name, "github_"):
		return github.CallTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "gitlab_"):
		return gitlab.CallTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "jira_"):
		return jira.CallTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "issue_"):
		return issue.CallTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "api_"):
		return generic.CallTool(ctx, cfg, name, a)
	}
	switch name {
	case "accounts":
		return auth.CallTool(ctx, cfg, name, a)
	case "pr_list", "pr_show", "pr_create", "pr_edit", "ci", "file", "comment":
		return pr.CallTool(ctx, cfg, name, a)
	case "request", "download":
		return generic.CallTool(ctx, cfg, name, a)
	case "cli":
		return modules.CallCLITool(ctx, cfg, a)
	case "azure", "atlassian": // hidden aliases of cli for one release
		args, err := mcp.ArgStrings(a, "args")
		if err != nil {
			return nil, err
		}
		return modules.CallTool(ctx, cfg, name, args)
	case "task_get_context":
		return taskContextJSON(ctx, cfg, mcp.ArgString(a, "key"))
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
