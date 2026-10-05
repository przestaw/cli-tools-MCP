package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	azureclient "workctl/internal/azure/client"
	"workctl/internal/render"
	"workctl/internal/vcs"
)

func cmdPullRequest(cfg *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("pr actions: list, show")
	}
	fs := flag.NewFlagSet("pr "+args[0], flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	provider := fs.String("provider", "", "github, gitlab, or azure")
	repository := fs.String("repo", "", "repository")
	project := fs.String("project", "", "project (Azure DevOps or GitLab alias)")
	state := fs.String("state", "all", "open, closed, merged or all")
	search := fs.String("search", "", "search text")
	formatValue := fs.String("format", "text", "text or json")
	jsonOutput := fs.Bool("json", false, "equivalent to --format json")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *jsonOutput {
		*formatValue = "json"
	}
	format, err := render.ParseFormat(*formatValue)
	if err != nil {
		return err
	}
	name, profile, err := prProfile(cfg, *profileName, *provider)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()

	switch args[0] {
	case "list":
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: pr list [--profile PROFILE] [--repo REPOSITORY] [--project PROJECT] [--state STATE] [--search TEXT]")
		}
		return printPullRequests(ctx, cfg, name, profile, *repository, *project, *search, *state, 0, format)
	case "show", "get":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: pr show [--profile PROFILE] [--repo REPOSITORY] [--project PROJECT] NUMBER")
		}
		id, err := strconv.Atoi(fs.Arg(0))
		if err != nil || id <= 0 {
			return fmt.Errorf("NUMBER must be a positive integer")
		}
		return printPullRequests(ctx, cfg, name, profile, *repository, *project, "", "", id, format)
	default:
		return fmt.Errorf("unknown pr action %q", args[0])
	}
}

func prProfile(cfg *Config, requestedName, requestedKind string) (string, Profile, error) {
	if requestedName == "" {
		requestedName = strings.TrimSpace(os.Getenv("WORKCTL_PROFILE"))
	}
	if requestedName != "" {
		profile, ok := cfg.Profiles[requestedName]
		if !ok {
			return "", Profile{}, fmt.Errorf("profile %q is not configured", requestedName)
		}
		if requestedKind != "" && profile.Kind != requestedKind {
			return "", Profile{}, fmt.Errorf("profile %q is %s, not %s", requestedName, profile.Kind, requestedKind)
		}
		return requestedName, profile, nil
	}
	if requestedKind != "" {
		if name := cfg.Defaults[requestedKind]; name != "" {
			return name, cfg.Profiles[name], nil
		}
	}
	candidates := make([]string, 0, len(cfg.Profiles))
	for name, profile := range cfg.Profiles {
		if requestedKind == "" || profile.Kind == requestedKind {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) == 1 {
		name := candidates[0]
		return name, cfg.Profiles[name], nil
	}
	if len(candidates) == 0 {
		return "", Profile{}, fmt.Errorf("no matching profile is configured")
	}
	return "", Profile{}, fmt.Errorf("multiple profiles match; specify --profile %s", strings.Join(candidates, " or "))
}

func profileRepository(profile Profile, repository string) (string, error) {
	if repository != "" {
		return repository, nil
	}
	if profile.DefaultRepo != "" {
		return profile.DefaultRepo, nil
	}
	if len(profile.Repos) == 1 {
		return profile.Repos[0], nil
	}
	if profile.Kind == "gitlab" && profile.DefaultProject != "" {
		return profile.DefaultProject, nil
	}
	if profile.Kind == "gitlab" && len(profile.Projects) == 1 {
		return profile.Projects[0], nil
	}
	return "", fmt.Errorf("repository is required; pass --repo or set a default in the profile")
}

func printPullRequests(ctx context.Context, cfg *Config, name string, profile Profile, repository, project, search, state string, id int, format render.Format) error {
	var (
		body []byte
		err  error
	)
	repository, err = profileRepository(profile, repository)
	if err != nil {
		return err
	}
	switch profile.Kind {
	case "github":
		client, clientErr := newProfileClient(cfg, name, profile)
		if clientErr != nil {
			return clientErr
		}
		if id > 0 {
			body, err = githubGetPR(ctx, client, repository, id)
		} else {
			body, err = githubListPRs(ctx, client, repository, search, state)
		}
	case "gitlab":
		client, clientErr := newProfileClient(cfg, name, profile)
		if clientErr != nil {
			return clientErr
		}
		if id > 0 {
			body, err = gitlabGetMR(ctx, client, repository, id)
		} else {
			body, err = gitlabListMRs(ctx, client, repository, search, state)
		}
	case "azure":
		return printAzurePullRequests(ctx, cfg, name, profile, project, repository, state, id, format)
	default:
		return fmt.Errorf("profile %q has unsupported provider %q", name, profile.Kind)
	}
	if err != nil {
		return err
	}
	result, err := normalizePullRequests(profile.Kind, repository, body)
	if err != nil {
		return err
	}
	return render.PullRequests(os.Stdout, os.Stderr, result, format)
}

func normalizePullRequests(provider, repository string, body []byte) (vcs.Result, error) {
	var values []map[string]any
	if err := json.Unmarshal(body, &values); err != nil {
		var one map[string]any
		if err := json.Unmarshal(body, &one); err != nil {
			return vcs.Result{}, err
		}
		if items, ok := one["items"].([]any); ok {
			for _, item := range items {
				if value, ok := item.(map[string]any); ok {
					values = append(values, value)
				}
			}
		} else {
			values = []map[string]any{one}
		}
	}
	result := vcs.Result{Items: make([]vcs.PullRequest, 0, len(values))}
	for _, value := range values {
		pr, err := normalizePullRequest(provider, repository, value)
		if err != nil {
			return vcs.Result{}, err
		}
		result.Items = append(result.Items, pr)
	}
	return result, nil
}

func normalizePullRequest(provider, repository string, value map[string]any) (vcs.PullRequest, error) {
	pr := vcs.PullRequest{Provider: provider, Repository: repository, Details: value}
	switch provider {
	case "github":
		pr.ID = jsonNumber(value["number"])
		pr.Title, _ = value["title"].(string)
		pr.State, _ = value["state"].(string)
		pr.Draft, _ = value["draft"].(bool)
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
	case "gitlab":
		pr.ID = jsonNumber(value["iid"])
		pr.Title, _ = value["title"].(string)
		pr.State, _ = value["state"].(string)
		pr.SourceBranch, _ = value["source_branch"].(string)
		pr.TargetBranch, _ = value["target_branch"].(string)
		pr.URL, _ = value["web_url"].(string)
		if author, _ := value["author"].(map[string]any); author != nil {
			pr.Author, _ = author["username"].(string)
		}
		pr.Draft, _ = value["draft"].(bool)
	}
	if pr.ID <= 0 {
		return vcs.PullRequest{}, fmt.Errorf("%s response does not contain a pull request number", provider)
	}
	return pr, nil
}

func jsonNumber(value any) int {
	number, ok := value.(float64)
	if !ok || number != float64(int(number)) {
		return 0
	}
	return int(number)
}

func mustJSON(value any) []byte {
	body, _ := json.MarshalIndent(value, "", "  ")
	return body
}

func printAzurePullRequests(ctx context.Context, cfg *Config, name string, profile Profile, project, repository, state string, id int, format render.Format) error {
	if project == "" {
		project = profile.DefaultProject
	}
	if project == "" {
		return fmt.Errorf("Azure DevOps project is required; pass --project or set default_project in the profile")
	}
	credential, err := resolveProfileSecret(name, profile)
	if err != nil {
		return err
	}
	client := azureclient.New(azureclient.Options{OrgURL: profile.URL, Token: credential, Timeout: profileTimeout(cfg, profile), VerifyTLS: profile.VerifyTLS})
	if id > 0 {
		item, err := client.PullRequest(ctx, project, id)
		if err != nil {
			return err
		}
		return render.PullRequests(os.Stdout, os.Stderr, vcs.Result{Items: []vcs.PullRequest{normalizeAzurePullRequest(client, project, *item)}}, format)
	}
	status := state
	if status == "" || status == "open" {
		status = "active"
	}
	items, err := client.PullRequests(ctx, project, repository, azureclient.PullRequestSearch{Status: status}, false)
	if err != nil {
		return err
	}
	result := vcs.Result{Items: make([]vcs.PullRequest, 0, len(items))}
	for _, item := range items {
		result.Items = append(result.Items, normalizeAzurePullRequest(client, project, item))
	}
	return render.PullRequests(os.Stdout, os.Stderr, result, format)
}

func normalizeAzurePullRequest(client *azureclient.Client, project string, item azureclient.PullRequest) vcs.PullRequest {
	return vcs.PullRequest{
		Provider:     "azure",
		Repository:   item.Repository.Name,
		ID:           item.PullRequestID,
		Title:        item.Title,
		State:        item.Status,
		Draft:        item.IsDraft,
		Author:       item.CreatedBy.DisplayName,
		SourceBranch: strings.TrimPrefix(item.SourceRefName, "refs/heads/"),
		TargetBranch: strings.TrimPrefix(item.TargetRefName, "refs/heads/"),
		URL:          client.PRWebURL(project, item.Repository.Name, item.PullRequestID),
	}
}
