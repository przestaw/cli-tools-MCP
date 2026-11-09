// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package pr

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/generic"
	"workctl/internal/github"
	"workctl/internal/gitlab"
	"workctl/internal/issue"
	"workctl/internal/mcp"
)

// The provider-neutral repository tools translate one vocabulary (repo, number, head/base, ref, run, job) to the
// github_* and gitlab_* functions, which stay as the implementation but are no longer listed.

// repoTarget is the resolved repository of a call: the profile, its kind and the base arguments of its tools.
type repoTarget struct {
	kind    string
	profile string
	repo    string
	base    map[string]any
}

func resolveRepo(cfg *config.Config, a map[string]any) (repoTarget, error) {
	repo := cliutil.FirstNonEmpty(mcp.ArgString(a, "repo"), mcp.ArgString(a, "project"))
	name, profile, err := cfg.PRProfileFor(mcp.ArgString(a, "profile"), "", repo)
	if err != nil {
		return repoTarget{}, err
	}
	switch profile.Kind {
	case "github", "gitlab":
	default:
		return repoTarget{}, fmt.Errorf("profile %q is of kind %s: this tool serves github and gitlab; use the cli tool (module %s)", name, profile.Kind, profile.Kind)
	}
	if repo, err = config.ProfileRepository(profile, repo); err != nil {
		return repoTarget{}, err
	}
	t := repoTarget{kind: profile.Kind, profile: name, repo: repo, base: map[string]any{"profile": name}}
	if t.kind == "github" {
		t.base["repo"] = repo
	} else {
		t.base["project"] = repo
	}
	return t, nil
}

// call runs the provider's tool (githubTool or gitlabTool) with the base arguments plus extra.
func (t repoTarget) call(ctx context.Context, cfg *config.Config, githubTool, gitlabTool string, extra map[string]any) ([]byte, error) {
	a := map[string]any{}
	for k, v := range t.base {
		a[k] = v
	}
	for k, v := range extra {
		a[k] = v
	}
	if t.kind == "github" {
		if githubTool == "" {
			return nil, fmt.Errorf("not supported for github")
		}
		return github.CallTool(ctx, cfg, githubTool, a)
	}
	if gitlabTool == "" {
		return nil, fmt.Errorf("not supported for gitlab")
	}
	if n, ok := a["number"]; ok {
		a["iid"] = n
	}
	return gitlab.CallTool(ctx, cfg, gitlabTool, a)
}

// number reads the pull request number (number, or the older id/iid).
func number(a map[string]any) int {
	for _, k := range []string{"number", "id", "iid"} {
		if n := mcp.ArgInt(a, k, 0); n > 0 {
			return n
		}
	}
	return 0
}

// prParts maps pr_show include parts to the provider tools.
var prParts = map[string][2]string{
	"files":      {"github_get_pull_request_files", "gitlab_get_merge_request_changes"},
	"reviews":    {"github_get_pull_request_reviews", "gitlab_get_merge_request_approvals"},
	"reviewers":  {"github_get_pull_request_reviewers", "gitlab_get_merge_request_approvals"},
	"comments":   {"github_get_pull_request_comments", "gitlab_get_merge_request_notes"},
	"discussion": {"github_get_pull_request_discussion", "gitlab_get_merge_request_notes"},
	"checks":     {"github_get_pull_request_checks", "gitlab_get_merge_request_pipelines"},
}

// showParts adds the include parts to the pull request JSON: {"pull_request": ..., "files": ..., ...}.
// A part that fails is reported in its place instead of failing the whole call.
func showParts(ctx context.Context, cfg *config.Config, a map[string]any, pullRequest []byte, parts []string, raw bool) ([]byte, error) {
	t, err := resolveRepo(cfg, a)
	if err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{"pull_request": json.RawMessage(pullRequest)}
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		tools, ok := prParts[part]
		if !ok {
			return nil, fmt.Errorf("include: unknown part %q (files, reviews, reviewers, comments, discussion, checks)", part)
		}
		data, err := t.call(ctx, cfg, tools[0], tools[1], map[string]any{"number": number(a)})
		if err != nil {
			data, _ = json.Marshal(map[string]string{"error": err.Error()})
		} else if !json.Valid(data) {
			data, _ = json.Marshal(string(data))
		} else if !raw {
			data = compactPart(t.kind, part, data)
		}
		out[part] = data
	}
	return json.MarshalIndent(out, "", "  ")
}

// isRaw reports whether the call asked for the provider JSON instead of the compact shape.
func isRaw(a map[string]any) bool { return strings.EqualFold(mcp.ArgString(a, "format"), "raw") }

// neutralTools are the repository tools next to pr_list and pr_show. Write tools are always listed and refused
// without policy.allow_write.
func neutralTools() []mcp.Tool {
	repo := mcp.StrProp("Repository owner/name (GitHub) or group/project (GitLab); omit for the profile's default")
	profile := mcp.StrProp("Profile (account); omit to resolve by repository and defaults")
	num := mcp.IntProp("Pull request / merge request number")
	format := mcp.StrProp("compact (default: the fields an agent needs, the same for GitHub and GitLab) or raw (provider JSON)")
	return []mcp.Tool{
		{Name: "pr_create", Description: "Create a pull request (GitHub) or merge request (GitLab). Needs policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{
			"repo": repo, "head": mcp.StrProp("Source branch"), "base": mcp.StrProp("Target branch"), "title": mcp.StrProp("Title"),
			"body": mcp.StrProp("Description"), "draft": mcp.BoolProp("Create as draft"), "reviewers": mcp.ArrProp("Reviewer user names (GitHub)"), "format": format, "profile": profile,
		}, "head", "base", "title")},
		{Name: "pr_edit", Description: "Edit a pull/merge request: title, body, base, state (open or closed), reviewers (GitHub). Needs policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{
			"repo": repo, "number": num, "title": mcp.StrProp("New title"), "body": mcp.StrProp("New description"), "base": mcp.StrProp("New target branch"),
			"state": mcp.StrProp("open or closed"), "add_reviewers": mcp.ArrProp("Reviewers to add (GitHub)"), "remove_reviewers": mcp.ArrProp("Reviewers to remove (GitHub)"), "format": format, "profile": profile,
		}, "number")},
		{Name: "ci", Description: "CI runs (GitHub Actions) or pipelines (GitLab): list for a ref, one run with include jobs, or the log of one job (GitHub logs are downloaded to a file).", InputSchema: mcp.ObjSchema(map[string]any{
			"repo": repo, "ref": mcp.StrProp("Branch or tag to list runs for"), "run": mcp.IntProp("Run / pipeline id"), "job": mcp.IntProp("Job id for its log"),
			"include": mcp.ArrProp("With run: jobs"), "per_page": mcp.IntProp("Items per page when listing"), "format": format, "profile": profile,
		})},
		{Name: "file", Description: "Get raw repository file contents at a ref (GitHub or GitLab).", InputSchema: mcp.ObjSchema(map[string]any{
			"repo": repo, "path": mcp.StrProp("Repository-relative file path"), "ref": mcp.StrProp("Branch, tag or commit; default branch if omitted"), "profile": profile,
		}, "path")},
		{Name: "comment", Description: "Add a comment to a pull/merge request (repo + number) or to an issue (issue key, Jira Server or Cloud). Needs policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{
			"repo": repo, "number": num, "issue": mcp.StrProp("Issue key, e.g. ABC-123, instead of repo + number"), "body": mcp.StrProp("Comment text"), "format": format, "profile": profile,
		}, "body")},
		uploadTool(repo, profile),
	}
}

func callNeutral(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	if name == "upload" {
		return upload(ctx, cfg, a)
	}
	if name == "comment" && mcp.ArgString(a, "issue") != "" {
		return issue.CallTool(ctx, cfg, "issue_update", map[string]any{"key": mcp.ArgString(a, "issue"), "comment": mcp.ArgString(a, "body"), "profile": mcp.ArgString(a, "profile")})
	}
	switch name {
	case "pr_create", "pr_edit", "comment":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	t, err := resolveRepo(cfg, a)
	if err != nil {
		return nil, err
	}
	out, err := runNeutral(ctx, cfg, t, name, a)
	if err != nil || isRaw(a) {
		return out, err
	}
	switch name {
	case "ci":
		return compactCI(out), nil
	case "pr_create", "pr_edit", "comment":
		return compactWrite(out), nil
	}
	return out, nil
}

func runNeutral(ctx context.Context, cfg *config.Config, t repoTarget, name string, a map[string]any) ([]byte, error) {
	n := number(a)
	needNumber := func() error {
		if n <= 0 {
			return fmt.Errorf("number must be a positive integer")
		}
		return nil
	}
	switch name {
	case "file":
		return t.call(ctx, cfg, "github_get_file", "gitlab_get_file", map[string]any{"path": mcp.ArgString(a, "path"), "ref": mcp.ArgString(a, "ref")})
	case "comment":
		if err := needNumber(); err != nil {
			return nil, err
		}
		return t.call(ctx, cfg, "github_add_pull_request_comment", "gitlab_add_merge_request_note", map[string]any{"number": n, "body": mcp.ArgString(a, "body")})
	case "pr_create":
		reviewers, err := mcp.ArgStrings(a, "reviewers")
		if err != nil {
			return nil, err
		}
		if t.kind == "gitlab" {
			if len(reviewers) > 0 {
				return nil, fmt.Errorf("reviewers are supported for github only")
			}
			return t.call(ctx, cfg, "", "gitlab_create_merge_request", map[string]any{"source": mcp.ArgString(a, "head"), "target": mcp.ArgString(a, "base"),
				"title": mcp.ArgString(a, "title"), "description": mcp.ArgString(a, "body"), "draft": mcp.ArgBool(a, "draft")})
		}
		return t.call(ctx, cfg, "github_create_pull_request", "", map[string]any{"head": mcp.ArgString(a, "head"), "base": mcp.ArgString(a, "base"),
			"title": mcp.ArgString(a, "title"), "body": mcp.ArgString(a, "body"), "draft": mcp.ArgBool(a, "draft"), "reviewers": strings.Join(reviewers, ",")})
	case "pr_edit":
		if err := needNumber(); err != nil {
			return nil, err
		}
		add, err := mcp.ArgStrings(a, "add_reviewers")
		if err != nil {
			return nil, err
		}
		remove, err := mcp.ArgStrings(a, "remove_reviewers")
		if err != nil {
			return nil, err
		}
		if t.kind == "gitlab" {
			if len(add)+len(remove) > 0 {
				return nil, fmt.Errorf("reviewers are supported for github only")
			}
			return t.call(ctx, cfg, "", "gitlab_edit_merge_request", map[string]any{"number": n, "title": mcp.ArgString(a, "title"),
				"description": mcp.ArgString(a, "body"), "target": mcp.ArgString(a, "base"), "state": mcp.ArgString(a, "state")})
		}
		return t.call(ctx, cfg, "github_edit_pull_request", "", map[string]any{"number": n, "title": mcp.ArgString(a, "title"), "body": mcp.ArgString(a, "body"),
			"base": mcp.ArgString(a, "base"), "state": mcp.ArgString(a, "state"), "add_reviewers": strings.Join(add, ","), "remove_reviewers": strings.Join(remove, ",")})
	case "ci":
		return ci(ctx, cfg, t, a)
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

func ci(ctx context.Context, cfg *config.Config, t repoTarget, a map[string]any) ([]byte, error) {
	run, job := mcp.ArgInt(a, "run", 0), mcp.ArgInt(a, "job", 0)
	include, err := mcp.ArgStrings(a, "include")
	if err != nil {
		return nil, err
	}
	switch {
	case job > 0 && t.kind == "gitlab":
		return t.call(ctx, cfg, "", "gitlab_get_job_trace", map[string]any{"job_id": job})
	case job > 0:
		// GitHub answers with a redirect to log storage; download follows it without the credential.
		return generic.CallTool(ctx, cfg, "download", map[string]any{"profile": t.profile,
			"path": fmt.Sprintf("/repos/%s/actions/jobs/%d/logs", t.repo, job), "filename": fmt.Sprintf("job-%d.log", job)})
	case run > 0:
		data, err := t.call(ctx, cfg, "github_get_workflow_run", "gitlab_get_pipeline", map[string]any{"id": run})
		if err != nil || !hasPart(include, "jobs") {
			return data, err
		}
		jobs, err := t.call(ctx, cfg, "github_get_workflow_run_jobs", "gitlab_get_pipeline_jobs", map[string]any{"id": run})
		if err != nil {
			return nil, err
		}
		return json.MarshalIndent(map[string]json.RawMessage{"run": data, "jobs": jobs}, "", "  ")
	}
	extra := map[string]any{"per_page": mcp.ArgInt(a, "per_page", 30)}
	if t.kind == "github" {
		extra["branch"] = mcp.ArgString(a, "ref")
	} else {
		extra["ref"] = mcp.ArgString(a, "ref")
	}
	return t.call(ctx, cfg, "github_list_workflow_runs", "gitlab_list_pipelines", extra)
}

func hasPart(parts []string, want string) bool {
	for _, p := range parts {
		if strings.EqualFold(strings.TrimSpace(p), want) {
			return true
		}
	}
	return false
}
