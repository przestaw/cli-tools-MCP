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

// Tools are the MCP tools of the GitLab module; write tools exist only with policy.allow_write.
func Tools(cfg *config.Config) []mcp.Tool {
	project := mcp.StrProp("Project path group/name; omit to use default")
	tools := []mcp.Tool{
		{Name: "gitlab_get_project", Description: "Get GitLab project metadata.", InputSchema: mcp.ObjSchema(map[string]any{"project": project})},
		{Name: "gitlab_get_merge_request", Description: "Get a GitLab merge request.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "iid": mcp.IntProp("Merge request IID")}, "iid")},
		{Name: "gitlab_get_merge_request_changes", Description: "Get merge request diff/changes (compatible with older GitLab v4 APIs).", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "iid": mcp.IntProp("Merge request IID")}, "iid")},
		{Name: "gitlab_get_merge_request_notes", Description: "Get merge request notes/comments.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "iid": mcp.IntProp("Merge request IID")}, "iid")},
		{Name: "gitlab_list_merge_requests", Description: "List/search merge requests in a project.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "search": mcp.StrProp("Search text"), "state": mcp.StrProp("opened, closed, merged, all")})},
		{Name: "gitlab_list_pipelines", Description: "List pipelines, optionally for a ref.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "ref": mcp.StrProp("Branch/tag ref"), "per_page": mcp.IntProp("Items per page")})},
		{Name: "gitlab_get_pipeline", Description: "Get one pipeline.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "id": mcp.IntProp("Pipeline ID")}, "id")},
		{Name: "gitlab_get_pipeline_jobs", Description: "Get jobs belonging to a pipeline.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "id": mcp.IntProp("Pipeline ID")}, "id")},
		{Name: "gitlab_get_job_trace", Description: "Get a GitLab CI job log/trace.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "job_id": mcp.IntProp("Job ID")}, "job_id")},
		{Name: "gitlab_list_branches", Description: "List/search branches in a project.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "search": mcp.StrProp("Branch search text")})},
		{Name: "gitlab_get_file", Description: "Get raw repository file contents at a ref.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "path": mcp.StrProp("Repository-relative file path"), "ref": mcp.StrProp("Branch/tag/commit; default HEAD")}, "path")},
	}
	if cfg.Policy.AllowWrite {
		tools = append(tools,
			mcp.Tool{Name: "gitlab_add_merge_request_note", Description: "Add a GitLab merge request note. Enabled only when policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "iid": mcp.IntProp("MR IID"), "body": mcp.StrProp("Note body")}, "iid", "body")},
			mcp.Tool{Name: "gitlab_create_merge_request", Description: "Create a merge request. Enabled only when policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{"project": project, "source": mcp.StrProp("Source branch"), "target": mcp.StrProp("Target branch"), "title": mcp.StrProp("Title"), "description": mcp.StrProp("Description"), "draft": mcp.BoolProp("Prefix title with Draft")}, "source", "target", "title")},
		)
	}
	mcp.WithProfileArg(tools, "gitlab_", "GitLab profile (account); omit to use defaults.gitlab")
	return tools
}

// CallTool runs one gitlab_* MCP tool against the profile named by the call (default: defaults.gitlab).
// The client is made per call, so a missing login fails only these tools.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	cfg, err := cfg.Scoped(mcp.ArgString(a, "profile"), "gitlab")
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
