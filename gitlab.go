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
	if project != "" {
		return project, nil
	}
	if cfg.GitLab.DefaultProject != "" {
		return cfg.GitLab.DefaultProject, nil
	}
	if len(cfg.GitLab.Projects) == 1 {
		return cfg.GitLab.Projects[0], nil
	}
	return "", fmt.Errorf("GitLab project not specified and gitlab.default_project is empty")
}

func glProjectID(project string) string { return url.PathEscape(project) }

func gitlabGetProject(ctx context.Context, c *APIClient, project string) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, "/api/v4/projects/"+glProjectID(project), nil, nil, ""))
}

func gitlabGetMR(ctx context.Context, c *APIClient, project string, iid int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d", glProjectID(project), iid)
	return bodyOnly(c.do(ctx, http.MethodGet, p, nil, nil, ""))
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

func gitlabListMRs(ctx context.Context, c *APIClient, project, search, state string) ([]byte, error) {
	q := url.Values{"per_page": {"100"}, "scope": {"all"}}
	if search != "" {
		q.Set("search", search)
	}
	if state != "" {
		q.Set("state", state)
	} else {
		q.Set("state", "all")
	}
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests", glProjectID(project))
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

func getTaskContext(ctx context.Context, cfg *Config, jc, gc *APIClient, key string) ([]byte, error) {
	out := taskContext{Key: key}
	issue, err := jiraGetIssue(ctx, jc, key, nil)
	if err != nil {
		out.Errors = append(out.Errors, "jira: "+err.Error())
	} else {
		out.Jira = json.RawMessage(issue)
	}
	projects := cfg.GitLab.Projects
	if len(projects) == 0 && cfg.GitLab.DefaultProject != "" {
		projects = []string{cfg.GitLab.DefaultProject}
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
