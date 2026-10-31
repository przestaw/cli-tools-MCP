package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"workctl/internal/vcs"
)

func gitlabGetMR(ctx context.Context, client *APIClient, project string, id int) ([]byte, error) {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d", glProjectID(project), id)
	return bodyOnly(client.do(ctx, http.MethodGet, path, nil, nil, ""))
}

func gitlabListMRs(ctx context.Context, client *APIClient, project, search, state string) ([]byte, error) {
	query := url.Values{"per_page": {"100"}, "scope": {"all"}, "state": {firstNonEmpty(state, "all")}}
	if search != "" {
		query.Set("search", search)
	}
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests", glProjectID(project))
	return bodyOnly(client.do(ctx, http.MethodGet, path, query, nil, ""))
}

type gitlabPRReader struct{ client *APIClient }

var _ vcs.PullRequestReader = (*gitlabPRReader)(nil)

func (g *gitlabPRReader) ListPullRequests(ctx context.Context, opt vcs.ListOptions) (vcs.Result, error) {
	state := opt.State
	if state == "open" {
		state = "opened"
	}
	body, err := gitlabListMRs(ctx, g.client, opt.Repository, opt.Search, state)
	if err != nil {
		return vcs.Result{}, err
	}
	return normalizePullRequests("gitlab", opt.Repository, body)
}

func (g *gitlabPRReader) GetPullRequest(ctx context.Context, opt vcs.ListOptions, id int) (vcs.PullRequest, error) {
	body, err := gitlabGetMR(ctx, g.client, opt.Repository, id)
	return singlePullRequest("gitlab", opt.Repository, fmt.Sprintf("merge request %d not found", id), body, err)
}

func fillGitLabPullRequest(pr *vcs.PullRequest) {
	value := pr.Details
	pr.ID = jsonNumber(value["iid"])
	pr.Description, _ = value["description"].(string)
	pr.State, _ = value["state"].(string)
	pr.SourceBranch, _ = value["source_branch"].(string)
	pr.TargetBranch, _ = value["target_branch"].(string)
	pr.URL, _ = value["web_url"].(string)
	if author, _ := value["author"].(map[string]any); author != nil {
		pr.Author, _ = author["username"].(string)
	}
}
