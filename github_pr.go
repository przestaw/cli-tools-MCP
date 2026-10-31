package main

import (
	"context"
	"fmt"
	"workctl/internal/vcs"
)

func githubGetPR(ctx context.Context, client *APIClient, repo string, id int) ([]byte, error) {
	return githubGet(ctx, client, githubPRPath(repo, id, ""), nil)
}

func githubListPRs(ctx context.Context, client *APIClient, repo, search, state string) ([]byte, error) {
	if state == "" {
		state = "all"
	}
	query := perPage100()
	if search == "" {
		query.Set("state", state)
		return githubGet(ctx, client, "/repos/"+repo+"/pulls", query)
	}
	text := "repo:" + repo + " is:pr " + search
	switch state {
	case "open", "closed":
		text += " is:" + state
	case "merged":
		text += " is:merged"
	}
	query.Set("q", text)
	return githubGet(ctx, client, "/search/issues", query)
}

type githubPRReader struct{ client *APIClient }

var _ vcs.PullRequestReader = (*githubPRReader)(nil)

func (g *githubPRReader) ListPullRequests(ctx context.Context, opt vcs.ListOptions) (vcs.Result, error) {
	state := opt.State
	if state == "merged" && opt.Search == "" {
		state = "closed"
	}
	body, err := githubListPRs(ctx, g.client, opt.Repository, opt.Search, state)
	if err != nil {
		return vcs.Result{}, err
	}
	res, err := normalizePullRequests("github", opt.Repository, body)
	if err != nil || (opt.State != "closed" && opt.State != "merged") {
		return res, err
	}
	kept := res.Items[:0]
	for _, item := range res.Items {
		details := item.Details
		if nested, ok := details["pull_request"].(map[string]any); ok {
			details = nested
		}
		if (details["merged_at"] != nil) == (opt.State == "merged") {
			kept = append(kept, item)
		}
	}
	res.Items = kept
	return res, nil
}

func (g *githubPRReader) GetPullRequest(ctx context.Context, opt vcs.ListOptions, id int) (vcs.PullRequest, error) {
	body, err := githubGetPR(ctx, g.client, opt.Repository, id)
	return singlePullRequest("github", opt.Repository, fmt.Sprintf("pull request %d not found", id), body, err)
}

func fillGitHubPullRequest(pr *vcs.PullRequest) {
	value := pr.Details
	pr.ID = jsonNumber(value["number"])
	pr.Description, _ = value["body"].(string)
	pr.State, _ = value["state"].(string)
	pr.URL, _ = value["html_url"].(string)
	if user, _ := value["user"].(map[string]any); user != nil {
		pr.Author, _ = user["login"].(string)
	}
	if head, _ := value["head"].(map[string]any); head != nil {
		pr.SourceBranch, _ = head["ref"].(string)
	}
	if base, _ := value["base"].(map[string]any); base != nil {
		pr.TargetBranch, _ = base["ref"].(string)
	}
}
