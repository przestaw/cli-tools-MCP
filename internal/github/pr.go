// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package github

import (
	"context"
	"fmt"

	"workctl/internal/api"
	"workctl/internal/vcs"
)

func getPR(ctx context.Context, client *api.Client, repo string, id int) ([]byte, error) {
	return get(ctx, client, prPath(repo, id, ""), nil)
}

func listPRs(ctx context.Context, client *api.Client, repo, search, state string) ([]byte, error) {
	if state == "" {
		state = "all"
	}
	query := perPage100()
	if search == "" {
		query.Set("state", state)
		return get(ctx, client, "/repos/"+repo+"/pulls", query)
	}
	text := "repo:" + repo + " is:pr " + search
	switch state {
	case "open", "closed":
		text += " is:" + state
	case "merged":
		text += " is:merged"
	}
	query.Set("q", text)
	return get(ctx, client, "/search/issues", query)
}

// PRReader adapts GitHub pull requests to the provider-neutral reader.
type PRReader struct{ client *api.Client }

// NewPRReader wraps a GitHub client for the provider-neutral `workctl pr` command.
func NewPRReader(client *api.Client) *PRReader { return &PRReader{client: client} }

var _ vcs.PullRequestReader = (*PRReader)(nil)

// ListPullRequests lists pull requests. GitHub has no "merged" state, so merged and closed pull requests are
// fetched as closed and told apart by their merged_at field.
func (g *PRReader) ListPullRequests(ctx context.Context, opt vcs.ListOptions) (vcs.Result, error) {
	state := opt.State
	if state == "merged" && opt.Search == "" {
		state = "closed"
	}
	body, err := listPRs(ctx, g.client, opt.Repository, opt.Search, state)
	if err != nil {
		return vcs.Result{}, err
	}
	res, err := vcs.Normalize("github", opt.Repository, body, fillPullRequest)
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

// GetPullRequest fetches one pull request by number.
func (g *PRReader) GetPullRequest(ctx context.Context, opt vcs.ListOptions, id int) (vcs.PullRequest, error) {
	body, err := getPR(ctx, g.client, opt.Repository, id)
	return vcs.Single("github", opt.Repository, fmt.Sprintf("pull request %d not found", id), body, err, fillPullRequest)
}

func fillPullRequest(pr *vcs.PullRequest) {
	value := pr.Details
	pr.ID = vcs.Number(value["number"])
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
