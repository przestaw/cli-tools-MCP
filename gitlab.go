package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func glProject(cfg *Config, project string) (string, error) {
	_, profile, _ := cfg.profileOf("gitlab")
	return profileRepository(profile, project)
}

func glProjectID(project string) string { return url.PathEscape(project) }

func gitlabGetProject(ctx context.Context, c *APIClient, project string) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, "/api/v4/projects/"+glProjectID(project), nil, nil, ""))
}

func gitlabGetMRChanges(ctx context.Context, c *APIClient, project string, iid int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/changes", glProjectID(project), iid)
	return bodyOnly(c.do(ctx, http.MethodGet, p, nil, nil, ""))
}

func gitlabGetMRNotes(ctx context.Context, c *APIClient, project string, iid int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", glProjectID(project), iid)
	return bodyOnly(c.do(ctx, http.MethodGet, p, url.Values{"per_page": {"100"}}, nil, ""))
}

func gitlabAddMRNote(ctx context.Context, c *APIClient, project string, iid int, text string) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", glProjectID(project), iid)
	q := url.Values{"body": {text}}
	return bodyOnly(c.do(ctx, http.MethodPost, p, q, nil, ""))
}

func gitlabCreateMR(ctx context.Context, c *APIClient, project, source, target, title, description string, draft bool) ([]byte, error) {
	if draft && !strings.HasPrefix(strings.ToLower(title), "draft:") && !strings.HasPrefix(strings.ToLower(title), "wip:") {
		title = "Draft: " + title
	}
	q := url.Values{}
	q.Set("source_branch", source)
	q.Set("target_branch", target)
	q.Set("title", title)
	if description != "" {
		q.Set("description", description)
	}
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests", glProjectID(project))
	return bodyOnly(c.do(ctx, http.MethodPost, p, q, nil, ""))
}

func gitlabListPipelines(ctx context.Context, c *APIClient, project, ref string, perPage int) ([]byte, error) {
	q := url.Values{"per_page": {strconv.Itoa(mustPositive(perPage, 30))}}
	if ref != "" {
		q.Set("ref", ref)
	}
	p := fmt.Sprintf("/api/v4/projects/%s/pipelines", glProjectID(project))
	return bodyOnly(c.do(ctx, http.MethodGet, p, q, nil, ""))
}

func gitlabGetPipeline(ctx context.Context, c *APIClient, project string, id int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/pipelines/%d", glProjectID(project), id)
	return bodyOnly(c.do(ctx, http.MethodGet, p, nil, nil, ""))
}

func gitlabListPipelineJobs(ctx context.Context, c *APIClient, project string, id int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/pipelines/%d/jobs", glProjectID(project), id)
	return bodyOnly(c.do(ctx, http.MethodGet, p, url.Values{"per_page": {"100"}}, nil, ""))
}

func gitlabJobTrace(ctx context.Context, c *APIClient, project string, id int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/jobs/%d/trace", glProjectID(project), id)
	return bodyOnly(c.do(ctx, http.MethodGet, p, nil, nil, ""))
}

func gitlabListBranches(ctx context.Context, c *APIClient, project, search string) ([]byte, error) {
	q := url.Values{"per_page": {"100"}}
	if search != "" {
		q.Set("search", search)
	}
	p := fmt.Sprintf("/api/v4/projects/%s/repository/branches", glProjectID(project))
	return bodyOnly(c.do(ctx, http.MethodGet, p, q, nil, ""))
}

func gitlabGetFile(ctx context.Context, c *APIClient, project, filePath, ref string) ([]byte, error) {
	if ref == "" {
		ref = "HEAD"
	}
	q := url.Values{"ref": {ref}}
	p := fmt.Sprintf("/api/v4/projects/%s/repository/files/%s/raw", glProjectID(project), url.PathEscape(filePath))
	return bodyOnly(c.do(ctx, http.MethodGet, p, q, nil, ""))
}

func gitlabVersion(ctx context.Context, c *APIClient) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, "/api/v4/version", nil, nil, ""))
}

// callGitLabTool runs one gitlab_* MCP tool against the profile named by the call (default: defaults.gitlab).
// The client is made per call, so a missing login fails only these tools.
func callGitLabTool(ctx context.Context, cfg *Config, name string, a map[string]any) ([]byte, error) {
	cfg, err := scopedConfig(cfg, a, "gitlab")
	if err != nil {
		return nil, err
	}
	project, err := glProject(cfg, argString(a, "project"))
	if err != nil {
		return nil, err
	}
	c, err := newKindClient(cfg, "gitlab")
	if err != nil {
		return nil, err
	}
	iid, id := argInt(a, "iid", 0), argInt(a, "id", 0)
	switch name {
	case "gitlab_get_project":
		return gitlabGetProject(ctx, c, project)
	case "gitlab_get_merge_request":
		return gitlabGetMR(ctx, c, project, iid)
	case "gitlab_get_merge_request_changes":
		return gitlabGetMRChanges(ctx, c, project, iid)
	case "gitlab_get_merge_request_notes":
		return gitlabGetMRNotes(ctx, c, project, iid)
	case "gitlab_list_merge_requests":
		return gitlabListMRs(ctx, c, project, argString(a, "search"), argString(a, "state"))
	case "gitlab_list_pipelines":
		return gitlabListPipelines(ctx, c, project, argString(a, "ref"), argInt(a, "per_page", 30))
	case "gitlab_get_pipeline":
		return gitlabGetPipeline(ctx, c, project, id)
	case "gitlab_get_pipeline_jobs":
		return gitlabListPipelineJobs(ctx, c, project, id)
	case "gitlab_get_job_trace":
		return gitlabJobTrace(ctx, c, project, argInt(a, "job_id", 0))
	case "gitlab_list_branches":
		return gitlabListBranches(ctx, c, project, argString(a, "search"))
	case "gitlab_get_file":
		return gitlabGetFile(ctx, c, project, argString(a, "path"), argString(a, "ref"))
	case "gitlab_add_merge_request_note":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		return gitlabAddMRNote(ctx, c, project, iid, argString(a, "body"))
	case "gitlab_create_merge_request":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		return gitlabCreateMR(ctx, c, project, argString(a, "source"), argString(a, "target"), argString(a, "title"), argString(a, "description"), argBool(a, "draft"))
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

type taskProjectContext struct {
	Project  string          `json:"project"`
	Branches json.RawMessage `json:"branches,omitempty"`
	MRs      json.RawMessage `json:"merge_requests,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type taskContext struct {
	Key    string               `json:"key"`
	Jira   json.RawMessage      `json:"jira,omitempty"`
	GitLab []taskProjectContext `json:"gitlab,omitempty"`
	Errors []string             `json:"errors,omitempty"`
}

// getTaskContext collects the Jira issue and the matching GitLab branches and merge requests of the default profiles.
func getTaskContext(ctx context.Context, cfg *Config, key string) ([]byte, error) {
	jc, err := newKindClient(cfg, "jira")
	if err != nil {
		return nil, err
	}
	gc, err := newKindClient(cfg, "gitlab")
	if err != nil {
		return nil, err
	}
	out := taskContext{Key: key}
	issue, err := jiraGetIssue(ctx, jc, key, nil)
	if err != nil {
		out.Errors = append(out.Errors, "jira: "+err.Error())
	} else {
		out.Jira = json.RawMessage(issue)
	}
	_, profile, _ := cfg.profileOf("gitlab")
	projects := profile.Projects
	if len(projects) == 0 && profile.DefaultProject != "" {
		projects = []string{profile.DefaultProject}
	}
	for _, project := range projects {
		pc := taskProjectContext{Project: project}
		branches, berr := gitlabListBranches(ctx, gc, project, key)
		mrs, merr := gitlabListMRs(ctx, gc, project, key, "all")
		if berr != nil || merr != nil {
			var parts []string
			if berr != nil {
				parts = append(parts, "branches: "+berr.Error())
			}
			if merr != nil {
				parts = append(parts, "MRs: "+merr.Error())
			}
			pc.Error = strings.Join(parts, "; ")
		}
		if berr == nil {
			pc.Branches = json.RawMessage(branches)
		}
		if merr == nil {
			pc.MRs = json.RawMessage(mrs)
		}
		out.GitLab = append(out.GitLab, pc)
	}
	return json.MarshalIndent(out, "", "  ")
}
