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
		res, err := fetchPullRequests(ctx, cfg, name, profile, *repository, *project, *search, *state, 0)
		if err != nil {
			return err
		}
		return render.PullRequests(os.Stdout, os.Stderr, res, format, false)
	case "show", "get":
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: pr show [--profile PROFILE] [--repo REPOSITORY] [--project PROJECT] NUMBER")
		}
		id, err := strconv.Atoi(fs.Arg(0))
		if err != nil || id <= 0 {
			return fmt.Errorf("NUMBER must be a positive integer")
		}
		res, err := fetchPullRequests(ctx, cfg, name, profile, *repository, *project, "", "", id)
		if err != nil {
			return err
		}
		return render.PullRequests(os.Stdout, os.Stderr, res, format, true)
	default:
		return fmt.Errorf("unknown pr action %q", args[0])
	}
}

func prProfile(cfg *Config, requestedName, requestedKind string) (string, Profile, error) {
	if requestedName == "" {
		requestedName = firstNonEmpty(cfg.profile, strings.TrimSpace(os.Getenv("WORKCTL_PROFILE")))
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
	return cfg.profileOf(requestedKind)
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
	return "", fmt.Errorf("repository is required: set repo (--repo on the CLI) or default_repo in the profile")
}

// fetchPullRequests lists (id 0) or gets one; state is open, closed (not merged), merged or all.
func fetchPullRequests(ctx context.Context, cfg *Config, name string, profile Profile, repository, project, search, state string, id int) (vcs.Result, error) {
	switch state {
	case "":
		state = "all"
	case "open", "closed", "merged", "all":
	default:
		return vcs.Result{}, fmt.Errorf("unsupported state %q (use open, closed, merged or all)", state)
	}
	if profile.Kind != "azure" {
		var err error
		if repository, err = profileRepository(profile, repository); err != nil {
			return vcs.Result{}, err
		}
	}
	reader, err := newPullRequestReader(ctx, cfg, name, profile)
	if err != nil {
		return vcs.Result{}, err
	}
	opt := vcs.ListOptions{Repository: repository, Project: project, Search: search, State: state}
	if id > 0 {
		item, err := reader.GetPullRequest(ctx, opt, id)
		if err != nil {
			return vcs.Result{}, err
		}
		return vcs.Result{Items: []vcs.PullRequest{item}}, nil
	}
	return reader.ListPullRequests(ctx, opt)
}

func newPullRequestReader(ctx context.Context, cfg *Config, name string, profile Profile) (vcs.PullRequestReader, error) {
	switch profile.Kind {
	case "github", "gitlab":
		c, err := newProfileClient(cfg, name, profile)
		if err != nil {
			return nil, err
		}
		if profile.Kind == "github" {
			return &githubPRReader{client: c}, nil
		}
		return &gitlabPRReader{client: c}, nil
	case "azure":
		cred, err := resolveProfileSecret(name, profile)
		if err != nil {
			return nil, err
		}
		c := azureclient.New(azureclient.Options{
			OrgURL:    profile.URL,
			Token:     cred,
			Timeout:   profileTimeout(cfg, profile),
			VerifyTLS: profile.VerifyTLS,
		})
		return &azurePRReader{client: c, defaultProject: profile.DefaultProject}, nil
	default:
		return nil, vcs.UnsupportedCapabilityError{Provider: profile.Kind, Capability: "pull requests"}
	}
}

func singlePullRequest(provider, repository, notFound string, body []byte, requestErr error) (vcs.PullRequest, error) {
	if requestErr != nil {
		return vcs.PullRequest{}, requestErr
	}
	res, err := normalizePullRequests(provider, repository, body)
	if err != nil {
		return vcs.PullRequest{}, err
	}
	if len(res.Items) == 0 {
		return vcs.PullRequest{}, fmt.Errorf("%s", notFound)
	}
	return res.Items[0], nil
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
	pr.Title, _ = value["title"].(string)
	pr.Draft, _ = value["draft"].(bool)
	switch provider {
	case "github":
		fillGitHubPullRequest(&pr)
	case "gitlab":
		fillGitLabPullRequest(&pr)
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
