package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/vcs"
)

func getMR(ctx context.Context, client *api.Client, project string, id int) ([]byte, error) {
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d", projectID(project), id)
	return api.BodyOnly(client.Do(ctx, http.MethodGet, path, nil, nil, ""))
}

// ListMRs lists or searches merge requests; the task context uses it to find merge requests that mention an issue key.
func ListMRs(ctx context.Context, client *api.Client, project, search, state string) ([]byte, error) {
	query := url.Values{"per_page": {"100"}, "scope": {"all"}, "state": {cliutil.FirstNonEmpty(state, "all")}}
	if search != "" {
		query.Set("search", search)
	}
	path := fmt.Sprintf("/api/v4/projects/%s/merge_requests", projectID(project))
	return api.BodyOnly(client.Do(ctx, http.MethodGet, path, query, nil, ""))
}

// PRReader adapts GitLab merge requests to the provider-neutral reader.
type PRReader struct{ client *api.Client }

// NewPRReader wraps a GitLab client for the provider-neutral `workctl pr` command.
func NewPRReader(client *api.Client) *PRReader { return &PRReader{client: client} }

var _ vcs.PullRequestReader = (*PRReader)(nil)

func (g *PRReader) ListPullRequests(ctx context.Context, opt vcs.ListOptions) (vcs.Result, error) {
	state := opt.State
	if state == "open" {
		state = "opened"
	}
	body, err := ListMRs(ctx, g.client, opt.Repository, opt.Search, state)
	if err != nil {
		return vcs.Result{}, err
	}
	return vcs.Normalize("gitlab", opt.Repository, body, fillPullRequest)
}

func (g *PRReader) GetPullRequest(ctx context.Context, opt vcs.ListOptions, id int) (vcs.PullRequest, error) {
	body, err := getMR(ctx, g.client, opt.Repository, id)
	return vcs.Single("gitlab", opt.Repository, fmt.Sprintf("merge request %d not found", id), body, err, fillPullRequest)
}

func fillPullRequest(pr *vcs.PullRequest) {
	value := pr.Details
	pr.ID = vcs.Number(value["iid"])
	pr.Description, _ = value["description"].(string)
	pr.State, _ = value["state"].(string)
	pr.SourceBranch, _ = value["source_branch"].(string)
	pr.TargetBranch, _ = value["target_branch"].(string)
	pr.URL, _ = value["web_url"].(string)
	if author, _ := value["author"].(map[string]any); author != nil {
		pr.Author, _ = author["username"].(string)
	}
}
