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

// CallTool runs one GitHub operation by its former MCP tool name; the provider-neutral tools in package pr call it.
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
