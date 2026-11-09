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

// CallTool runs one GitLab operation by its former MCP tool name against the profile named by the call (default:
// defaults.gitlab); the provider-neutral tools in package pr call it.
// The client is made per call, so a missing login fails only these tools.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	cfg, err := cfg.ScopedFor(mcp.ArgString(a, "profile"), "gitlab", mcp.ArgString(a, "project"))
	if err != nil {
		return nil, err
	}
	project, err := projectOf(cfg, mcp.ArgString(a, "project"))
	if err != nil {
		return nil, err
	}
	c, err := api.NewKindClient(cfg, "gitlab")
	if err != nil {
		return nil, err
	}
	iid, id := mcp.ArgInt(a, "iid", 0), mcp.ArgInt(a, "id", 0)
	switch name {
	case "gitlab_get_project":
		return getProject(ctx, c, project)
	case "gitlab_get_merge_request":
		return getMR(ctx, c, project, iid)
	case "gitlab_get_merge_request_changes":
		return getMRChanges(ctx, c, project, iid)
	case "gitlab_get_merge_request_notes":
		return getMRNotes(ctx, c, project, iid)
	case "gitlab_list_merge_requests":
		return ListMRs(ctx, c, project, mcp.ArgString(a, "search"), mcp.ArgString(a, "state"))
	case "gitlab_list_pipelines":
		return listPipelines(ctx, c, project, mcp.ArgString(a, "ref"), mcp.ArgInt(a, "per_page", 30))
	case "gitlab_get_pipeline":
		return getPipeline(ctx, c, project, id)
	case "gitlab_get_pipeline_jobs":
		return listPipelineJobs(ctx, c, project, id)
	case "gitlab_get_job_trace":
		return jobTrace(ctx, c, project, mcp.ArgInt(a, "job_id", 0))
	case "gitlab_list_branches":
		return ListBranches(ctx, c, project, mcp.ArgString(a, "search"))
	case "gitlab_get_file":
		return getFile(ctx, c, project, mcp.ArgString(a, "path"), mcp.ArgString(a, "ref"))
	case "gitlab_get_merge_request_pipelines":
		return mrPipelines(ctx, c, project, iid)
	case "gitlab_get_merge_request_approvals":
		return mrApprovals(ctx, c, project, iid)
	case "gitlab_edit_merge_request":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		return editMR(ctx, c, project, iid, mcp.ArgString(a, "title"), mcp.ArgString(a, "description"), mcp.ArgString(a, "target"), mcp.ArgString(a, "state"))
	case "gitlab_add_merge_request_note":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		return addMRNote(ctx, c, project, iid, mcp.ArgString(a, "body"))
	case "gitlab_create_merge_request":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		return createMR(ctx, c, project, mcp.ArgString(a, "source"), mcp.ArgString(a, "target"), mcp.ArgString(a, "title"), mcp.ArgString(a, "description"), mcp.ArgBool(a, "draft"))
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
