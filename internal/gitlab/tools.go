// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package gitlab

import (
	"context"
	"fmt"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

// toolCall is one MCP operation; write marks tools that need allow_write.
type toolCall struct {
	write bool
	run   func(ctx context.Context, c *api.Client, project string, a map[string]any) ([]byte, error)
}

var toolCalls = map[string]toolCall{
	"gitlab_get_project": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return getProject(ctx, c, p)
	}},
	"gitlab_get_merge_request": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return getMR(ctx, c, p, mcp.ArgInt(a, "iid", 0))
	}},
	"gitlab_get_merge_request_changes": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return getMRChanges(ctx, c, p, mcp.ArgInt(a, "iid", 0))
	}},
	"gitlab_get_merge_request_diff": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return getMRDiff(ctx, c, p, mcp.ArgInt(a, "iid", 0))
	}},
	"gitlab_get_merge_request_notes": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return getMRNotes(ctx, c, p, mcp.ArgInt(a, "iid", 0))
	}},
	"gitlab_list_merge_requests": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return ListMRs(ctx, c, p, mcp.ArgString(a, "search"), mcp.ArgString(a, "state"))
	}},
	"gitlab_list_pipelines": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return listPipelines(ctx, c, p, mcp.ArgString(a, "ref"), mcp.ArgInt(a, "per_page", 30))
	}},
	"gitlab_get_pipeline": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return getPipeline(ctx, c, p, mcp.ArgInt(a, "id", 0))
	}},
	"gitlab_get_pipeline_jobs": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return listPipelineJobs(ctx, c, p, mcp.ArgInt(a, "id", 0))
	}},
	"gitlab_get_job_trace": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return jobTrace(ctx, c, p, mcp.ArgInt(a, "job_id", 0))
	}},
	"gitlab_list_branches": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return ListBranches(ctx, c, p, mcp.ArgString(a, "search"))
	}},
	"gitlab_get_file": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return getFile(ctx, c, p, mcp.ArgString(a, "path"), mcp.ArgString(a, "ref"))
	}},
	"gitlab_get_merge_request_pipelines": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return mrPipelines(ctx, c, p, mcp.ArgInt(a, "iid", 0))
	}},
	"gitlab_get_merge_request_approvals": {run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return mrApprovals(ctx, c, p, mcp.ArgInt(a, "iid", 0))
	}},
	"gitlab_edit_merge_request": {write: true, run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return editMR(ctx, c, p, mcp.ArgInt(a, "iid", 0), mcp.ArgString(a, "title"), mcp.ArgString(a, "description"), mcp.ArgString(a, "target"), mcp.ArgString(a, "state"))
	}},
	"gitlab_add_merge_request_note": {write: true, run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return addMRNote(ctx, c, p, mcp.ArgInt(a, "iid", 0), mcp.ArgString(a, "body"))
	}},
	"gitlab_create_merge_request": {write: true, run: func(ctx context.Context, c *api.Client, p string, a map[string]any) ([]byte, error) {
		return createMR(ctx, c, p, mcp.ArgString(a, "source"), mcp.ArgString(a, "target"), mcp.ArgString(a, "title"), mcp.ArgString(a, "description"), mcp.ArgBool(a, "draft"))
	}},
}

// CallTool runs one GitLab operation by its former MCP tool name against the profile named by the call (default:
// defaults.gitlab); the provider-neutral tools in package pr call it.
// The client is made per call, so a missing login fails only these tools.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	call, ok := toolCalls[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	cfg, err := cfg.ScopedFor(mcp.ArgString(a, "profile"), "gitlab", mcp.ArgString(a, "project"))
	if err != nil {
		return nil, err
	}
	if call.write {
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	project, err := projectOf(cfg, mcp.ArgString(a, "project"))
	if err != nil {
		return nil, err
	}
	c, err := api.NewKindClient(cfg, "gitlab")
	if err != nil {
		return nil, err
	}
	return call.run(ctx, c, project, a)
}
