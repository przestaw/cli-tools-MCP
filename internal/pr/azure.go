package pr

import (
	"context"
	"fmt"
	"strings"

	azureclient "workctl/internal/azure/client"
	"workctl/internal/cliutil"
	"workctl/internal/vcs"
)

// azurePRReader adapts Azure DevOps pull requests, served by the embedded azure module's client.
type azurePRReader struct {
	client         *azureclient.Client
	defaultProject string
}

var _ vcs.PullRequestReader = (*azurePRReader)(nil)

func (a *azurePRReader) project(opt vcs.ListOptions) (string, error) {
	if project := cliutil.FirstNonEmpty(opt.Project, a.defaultProject); project != "" {
		return project, nil
	}
	return "", fmt.Errorf("the Azure DevOps project is required: set project (--project on the CLI) or default_project in the profile")
}

func (a *azurePRReader) ListPullRequests(ctx context.Context, opt vcs.ListOptions) (vcs.Result, error) {
	project, err := a.project(opt)
	if err != nil {
		return vcs.Result{}, err
	}
	status := map[string]string{"open": "active", "closed": "abandoned", "merged": "completed"}[opt.State]
	items, err := a.client.PullRequests(ctx, project, opt.Repository, azureclient.PullRequestSearch{Status: cliutil.FirstNonEmpty(status, opt.State)}, false)
	if err != nil {
		return vcs.Result{}, err
	}
	result := vcs.Result{Items: make([]vcs.PullRequest, 0, len(items))}
	search := strings.ToLower(opt.Search)
	for _, item := range items {
		norm := normalizeAzurePullRequest(a.client, project, item)
		if search != "" && !strings.Contains(strings.ToLower(norm.Title), search) && !strings.Contains(strings.ToLower(norm.Description), search) {
			continue
		}
		result.Items = append(result.Items, norm)
	}
	return result, nil
}

func (a *azurePRReader) GetPullRequest(ctx context.Context, opt vcs.ListOptions, id int) (vcs.PullRequest, error) {
	project, err := a.project(opt)
	if err != nil {
		return vcs.PullRequest{}, err
	}
	item, err := a.client.PullRequest(ctx, project, id)
	if err != nil {
		return vcs.PullRequest{}, err
	}
	return normalizeAzurePullRequest(a.client, project, *item), nil
}

func normalizeAzurePullRequest(client *azureclient.Client, project string, item azureclient.PullRequest) vcs.PullRequest {
	return vcs.PullRequest{
		Provider: "azure", Repository: item.Repository.Name, ID: item.PullRequestID,
		Title: item.Title, Description: item.Description, State: item.Status, Draft: item.IsDraft,
		Author:       item.CreatedBy.DisplayName,
		SourceBranch: strings.TrimPrefix(item.SourceRefName, "refs/heads/"),
		TargetBranch: strings.TrimPrefix(item.TargetRefName, "refs/heads/"),
		URL:          client.PRWebURL(project, item.Repository.Name, item.PullRequestID),
	}
}
