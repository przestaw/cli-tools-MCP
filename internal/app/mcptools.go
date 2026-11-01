package app

import (
	"context"
	"fmt"
	"os"
	"strings"

	"workctl/internal/config"
	"workctl/internal/github"
	"workctl/internal/gitlab"
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
	case strings.HasPrefix(name, "jira_"):
		return cfg.Has("jira")
	case strings.HasPrefix(name, "gitlab_"):
		return cfg.Has("gitlab")
	case strings.HasPrefix(name, "github_"):
		return cfg.Has("github")
	case strings.HasPrefix(name, "task_"):
		return cfg.Has("jira") && cfg.Has("gitlab")
	}
	return true
}

// mcpTools composes the MCP tools of every configured backend.
func mcpTools(cfg *config.Config) []mcp.Tool {
	all := append(jira.Tools(cfg), gitlab.Tools(cfg)...)
	all = append(all, mcp.Tool{Name: "task_get_context", Description: "Get Jira issue plus matching branches and merge requests from configured GitLab projects.", InputSchema: mcp.ObjSchema(map[string]any{"key": mcp.StrProp("Jira issue key")}, "key")})
	tools := []mcp.Tool{} // never nil: the JSON must say [] and not null
	for _, t := range all {
		if toolAvailable(cfg, t.Name) {
			tools = append(tools, t)
		}
	}
	if cfg.Has("github") {
		tools = append(tools, github.Tools(cfg)...)
	}
	if toolAvailable(cfg, "pr_list") {
		tools = append(tools, pr.Tools()...)
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
		Instructions:   "Azure DevOps (azure), Jira and Confluence Cloud (atlassian), Jira Server, GitLab and GitHub through workctl. Everything is read-only unless policy.allow_write=true in the config file.",
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
	}
	switch name {
	case "pr_list", "pr_show":
		return pr.CallTool(ctx, cfg, name, a)
	case "azure", "atlassian":
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
