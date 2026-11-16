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

// toolCall is one MCP operation; write marks tools that need allow_write.
type toolCall struct {
	write bool
	run   func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error)
}

var toolCalls = map[string]toolCall{
	"github_get_repo": {run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return getRepo(ctx, c, repo)
	}},
	"github_list_pull_requests": {run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return listPRs(ctx, c, repo, mcp.ArgString(a, "search"), mcp.ArgString(a, "state"))
	}},
	"github_list_workflow_runs": {run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return listRuns(ctx, c, repo, mcp.ArgString(a, "branch"), mcp.ArgInt(a, "per_page", 30))
	}},
	"github_get_workflow_run": {run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return getRun(ctx, c, repo, mcp.ArgInt(a, "id", 0))
	}},
	"github_get_workflow_run_jobs": {run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return runJobs(ctx, c, repo, mcp.ArgInt(a, "id", 0))
	}},
	"github_list_branches": {run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return listBranches(ctx, c, repo, mcp.ArgString(a, "search"))
	}},
	"github_get_file": {run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return getFile(ctx, c, repo, mcp.ArgString(a, "path"), mcp.ArgString(a, "ref"))
	}},
	"github_add_pull_request_comment": {write: true, run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return addComment(ctx, c, repo, mcp.ArgInt(a, "number", 0), mcp.ArgString(a, "body"))
	}},
	"github_create_pull_request": {write: true, run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return createPR(ctx, c, repo, mcp.ArgString(a, "head"), mcp.ArgString(a, "base"), mcp.ArgString(a, "title"), mcp.ArgString(a, "body"), mcp.ArgBool(a, "draft"), cliutil.SplitCommaList(mcp.ArgString(a, "reviewers")))
	}},
	"github_request_reviewers": {write: true, run: func(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
		return requestReviewers(ctx, c, repo, mcp.ArgInt(a, "number", 0), cliutil.SplitCommaList(mcp.ArgString(a, "reviewers")))
	}},
	"github_edit_pull_request": {write: true, run: editPRTool},
}

func editPRTool(ctx context.Context, c *api.Client, repo string, a map[string]any) ([]byte, error) {
	fields := prEditFields(mcp.ArgString(a, "title"), mcp.ArgString(a, "body"), mcp.ArgString(a, "base"), mcp.ArgString(a, "state"))
	last, err := applyPREdit(ctx, c, repo, mcp.ArgInt(a, "number", 0), fields, cliutil.SplitCommaList(mcp.ArgString(a, "add_reviewers")), cliutil.SplitCommaList(mcp.ArgString(a, "remove_reviewers")))
	if err != nil {
		return nil, err
	}
	if last == nil {
		return nil, fmt.Errorf("at least one of title, body, base, state, add_reviewers or remove_reviewers must be provided")
	}
	return last, nil
}

// CallTool runs one GitHub operation by its former MCP tool name; the provider-neutral tools in package pr call it.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	call, ok := toolCalls[name]
	var read prRead
	if !ok {
		for _, entry := range prReads {
			if entry.tool == name {
				read, ok = entry, true
				break
			}
		}
	}
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	cfg, err := cfg.ScopedFor(mcp.ArgString(a, "profile"), "github", mcp.ArgString(a, "repo"))
	if err != nil {
		return nil, err
	}
	if call.write {
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	c, err := api.NewKindClient(cfg, "github")
	if err != nil {
		return nil, err
	}
	repo, err := repoOf(cfg, mcp.ArgString(a, "repo"))
	if err != nil {
		return nil, err
	}
	if read.read != nil {
		return read.read(ctx, c, repo, mcp.ArgInt(a, "number", 0))
	}
	return call.run(ctx, c, repo, a)
}
