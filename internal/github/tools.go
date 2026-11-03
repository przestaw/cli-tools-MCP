// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package github

import (
	"context"
	"fmt"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

// Tools are the MCP tools of the GitHub module; write tools exist only with policy.allow_write.
func Tools(cfg *config.Config) []mcp.Tool {
	repo := mcp.StrProp("Repository owner/name; omit to use the default")
	num := mcp.IntProp("Pull request number")
	byNum := mcp.ObjSchema(map[string]any{"repo": repo, "number": num}, "number")
	tools := []mcp.Tool{
		{Name: "github_get_repo", Description: "Get GitHub repository metadata.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo})},
		{Name: "github_get_pull_request", Description: "Get a GitHub pull request.", InputSchema: byNum},
		{Name: "github_get_pull_request_files", Description: "Get the changed files and patches of a pull request.", InputSchema: byNum},
		{Name: "github_get_pull_request_reviews", Description: "Get the reviews (approve, request changes, comment) of a pull request.", InputSchema: byNum},
		{Name: "github_get_pull_request_reviewers", Description: "Get requested reviewers (users and teams) of a pull request.", InputSchema: byNum},
		{Name: "github_get_pull_request_comments", Description: "Get the inline review comments (threads on the diff) of a pull request.", InputSchema: byNum},
		{Name: "github_get_pull_request_discussion", Description: "Get the conversation comments of a pull request.", InputSchema: byNum},
		{Name: "github_get_pull_request_checks", Description: "Get the check runs of the head commit of a pull request.", InputSchema: byNum},
		{Name: "github_list_pull_requests", Description: "List or search pull requests of a repository.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "search": mcp.StrProp("Search text"), "state": mcp.StrProp("open, closed, merged, all")})},
		{Name: "github_list_workflow_runs", Description: "List GitHub Actions runs, optionally for a branch.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "branch": mcp.StrProp("Branch"), "per_page": mcp.IntProp("Items per page")})},
		{Name: "github_get_workflow_run", Description: "Get one GitHub Actions run.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "id": mcp.IntProp("Run ID")}, "id")},
		{Name: "github_get_workflow_run_jobs", Description: "Get the jobs of a GitHub Actions run.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "id": mcp.IntProp("Run ID")}, "id")},
		{Name: "github_list_branches", Description: "List or search branches of a repository.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "search": mcp.StrProp("Branch search text")})},
		{Name: "github_get_file", Description: "Get raw repository file contents at a ref.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "path": mcp.StrProp("Repository-relative file path"), "ref": mcp.StrProp("Branch, tag or commit; default branch if omitted")}, "path")},
	}
	if cfg.Policy.AllowWrite {
		tools = append(tools,
			mcp.Tool{Name: "github_add_pull_request_comment", Description: "Add a comment to a pull request. Enabled only when policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "number": num, "body": mcp.StrProp("Comment body")}, "number", "body")},
			mcp.Tool{Name: "github_create_pull_request", Description: "Create a pull request. Enabled only when policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "head": mcp.StrProp("Source branch"), "base": mcp.StrProp("Target branch"), "title": mcp.StrProp("Title"), "body": mcp.StrProp("Description"), "draft": mcp.BoolProp("Create as draft"), "reviewers": mcp.StrProp("Comma-separated reviewer usernames")}, "head", "base", "title")},
			mcp.Tool{Name: "github_request_reviewers", Description: "Request reviewers for a pull request. Enabled only when policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "number": num, "reviewers": mcp.StrProp("Comma-separated reviewer usernames")}, "number", "reviewers")},
			mcp.Tool{Name: "github_edit_pull_request", Description: "Edit a pull request title, body, state, base or reviewers. Enabled only when policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{"repo": repo, "number": num, "title": mcp.StrProp("New title"), "body": mcp.StrProp("New description"), "base": mcp.StrProp("New base branch"), "state": mcp.StrProp("open or closed"), "add_reviewers": mcp.StrProp("Comma-separated reviewer usernames to add"), "remove_reviewers": mcp.StrProp("Comma-separated reviewer usernames to remove")}, "number")},
		)
	}
	mcp.WithProfileArg(tools, "github_", "GitHub profile (account); omit to use defaults.github")
	return tools
}

// CallTool runs one github_* MCP tool.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	cfg, err := cfg.ScopedFor(mcp.ArgString(a, "profile"), "github", mcp.ArgString(a, "repo"))
	if err != nil {
		return nil, err
	}
	c, err := api.NewKindClient(cfg, "github")
	if err != nil {
		return nil, err
	}
	repo, err := repoOf(cfg, mcp.ArgString(a, "repo"))
	if err != nil {
		return nil, err
	}
	n, id := mcp.ArgInt(a, "number", 0), mcp.ArgInt(a, "id", 0)
	for _, entry := range prReads {
		if entry.tool == name {
			return entry.read(ctx, c, repo, n)
		}
	}
	switch name {
	case "github_get_repo":
		return getRepo(ctx, c, repo)
	case "github_list_pull_requests":
		return listPRs(ctx, c, repo, mcp.ArgString(a, "search"), mcp.ArgString(a, "state"))
	case "github_list_workflow_runs":
		return listRuns(ctx, c, repo, mcp.ArgString(a, "branch"), mcp.ArgInt(a, "per_page", 30))
	case "github_get_workflow_run":
		return getRun(ctx, c, repo, id)
	case "github_get_workflow_run_jobs":
		return runJobs(ctx, c, repo, id)
	case "github_list_branches":
		return listBranches(ctx, c, repo, mcp.ArgString(a, "search"))
	case "github_get_file":
		return getFile(ctx, c, repo, mcp.ArgString(a, "path"), mcp.ArgString(a, "ref"))
	case "github_add_pull_request_comment":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		return addComment(ctx, c, repo, n, mcp.ArgString(a, "body"))
	case "github_create_pull_request":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		return createPR(ctx, c, repo, mcp.ArgString(a, "head"), mcp.ArgString(a, "base"), mcp.ArgString(a, "title"), mcp.ArgString(a, "body"), mcp.ArgBool(a, "draft"), cliutil.SplitCommaList(mcp.ArgString(a, "reviewers")))
	case "github_request_reviewers":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		return requestReviewers(ctx, c, repo, n, cliutil.SplitCommaList(mcp.ArgString(a, "reviewers")))
	case "github_edit_pull_request":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		fields := prEditFields(mcp.ArgString(a, "title"), mcp.ArgString(a, "body"), mcp.ArgString(a, "base"), mcp.ArgString(a, "state"))
		last, err := applyPREdit(ctx, c, repo, n, fields, cliutil.SplitCommaList(mcp.ArgString(a, "add_reviewers")), cliutil.SplitCommaList(mcp.ArgString(a, "remove_reviewers")))
		if err != nil {
			return nil, err
		}
		if last == nil {
			return nil, fmt.Errorf("at least one of title, body, base, state, add_reviewers or remove_reviewers must be provided")
		}
		return last, nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
