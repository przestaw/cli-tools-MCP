// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package pr is the provider-neutral pull request surface: `workctl pr|mr ...` and
// the pr_list/pr_show MCP tools, served by the per-provider vcs.PullRequestReader adapters.
package pr

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"

	"workctl/internal/api"
	azureclient "workctl/internal/azure/client"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/github"
	"workctl/internal/gitlab"
	"workctl/internal/mcp"
	"workctl/internal/render"
	"workctl/internal/vcs"
)

// Command runs `workctl pr ...` (alias mr).
func Command(cfg *config.Config, args []string) error {
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
	name, profile, err := cfg.PRProfileFor(*profileName, *provider, cliutil.FirstNonEmpty(*repository, *project))
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ProfileTimeout(profile))
	defer cancel()

	switch args[0] {
	case "list":
		if fs.NArg() != 0 {
			return fmt.Errorf("usage: pr list [--profile PROFILE] [--repo REPOSITORY] [--project PROJECT] [--state STATE] [--search TEXT]")
		}
		res, err := Fetch(ctx, cfg, name, profile, *repository, *project, *search, *state, 0)
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
		res, err := Fetch(ctx, cfg, name, profile, *repository, *project, "", "", id)
		if err != nil {
			return err
		}
		return render.PullRequests(os.Stdout, os.Stderr, res, format, true)
	default:
		return fmt.Errorf("unknown pr action %q", args[0])
	}
}

// Fetch lists (id 0) or gets one pull request; state is open, closed (not merged), merged or all.
func Fetch(ctx context.Context, cfg *config.Config, name string, profile config.Profile, repository, project, search, state string, id int) (vcs.Result, error) {
	switch state {
	case "":
		state = "all"
	case "open", "closed", "merged", "all":
	default:
		return vcs.Result{}, fmt.Errorf("unsupported state %q (use open, closed, merged or all)", state)
	}
	if profile.Kind != "azure" {
		var err error
		if repository, err = config.ProfileRepository(profile, repository); err != nil {
			return vcs.Result{}, err
		}
	}
	reader, err := newReader(cfg, name, profile)
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

func newReader(cfg *config.Config, name string, profile config.Profile) (vcs.PullRequestReader, error) {
	switch profile.Kind {
	case "github", "gitlab":
		c, err := api.NewProfileClient(cfg, name, profile)
		if err != nil {
			return nil, err
		}
		if profile.Kind == "github" {
			return github.NewPRReader(c), nil
		}
		return gitlab.NewPRReader(c), nil
	case "azure":
		cred, err := config.ResolveProfileSecret(name, profile)
		if err != nil {
			return nil, err
		}
		c := azureclient.New(azureclient.Options{
			OrgURL:    profile.URL,
			Token:     cred,
			Timeout:   cfg.ProfileTimeout(profile),
			VerifyTLS: profile.VerifyTLS,
		})
		return &azurePRReader{client: c, defaultProject: profile.DefaultProject}, nil
	default:
		return nil, vcs.UnsupportedCapabilityError{Provider: profile.Kind, Capability: "pull requests"}
	}
}

// Tools are the provider-neutral repository tools: pr_list, pr_show, pr_create, pr_edit, ci, file and comment.
func Tools() []mcp.Tool {
	return append([]mcp.Tool{
		{Name: "pr_list", Description: "List pull requests or merge requests from GitHub, GitLab, or Azure DevOps using unified profiles.", InputSchema: mcp.ObjSchema(map[string]any{
			"profile": mcp.StrProp("Profile name (optional if default profile exists)"),
			"repo":    mcp.StrProp("Repository name or path (optional if default_repo is set)"),
			"project": mcp.StrProp("Project name (required for Azure DevOps unless set in profile)"),
			"state":   mcp.StrProp("Filter state: open, closed, merged, all"),
			"search":  mcp.StrProp("Search text (title and description)"),
			"format":  mcp.StrProp("Output format: json or text (default json)"),
		})},
		{Name: "pr_show", Description: "Get details and description of a pull request or merge request; include adds files, reviews, reviewers, comments (inline), discussion and checks (GitHub and GitLab).", InputSchema: mcp.ObjSchema(map[string]any{
			"number":  mcp.IntProp("Pull request or merge request number"),
			"id":      mcp.IntProp("Same as number (older name)"),
			"include": mcp.ArrProp("Optional parts: files, reviews, reviewers, comments, discussion, checks"),
			"profile": mcp.StrProp("Profile name (optional if default profile exists)"),
			"repo":    mcp.StrProp("Repository name or path (optional if default_repo is set)"),
			"project": mcp.StrProp("Project name (required for Azure DevOps unless set in profile)"),
			"format":  mcp.StrProp("json (default), text, or raw: with include, parts are compact (the same fields for GitHub and GitLab) unless format is raw"),
		})},
	}, neutralTools()...)
}

// CallTool runs one of the repository tools.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	if name != "pr_list" && name != "pr_show" {
		return callNeutral(ctx, cfg, name, a)
	}
	id := 0
	include, err := mcp.ArgStrings(a, "include")
	if err != nil {
		return nil, err
	}
	if name == "pr_show" {
		if id = number(a); id <= 0 {
			return nil, fmt.Errorf("number must be a positive integer")
		}
	}
	raw := isRaw(a)
	if raw || len(include) > 0 {
		a["format"] = "json"
	}
	pName, profile, err := cfg.PRProfileFor(mcp.ArgString(a, "profile"), "", cliutil.FirstNonEmpty(mcp.ArgString(a, "repo"), mcp.ArgString(a, "project")))
	if err != nil {
		return nil, err
	}
	res, err := Fetch(ctx, cfg, pName, profile, mcp.ArgString(a, "repo"), mcp.ArgString(a, "project"), mcp.ArgString(a, "search"), mcp.ArgString(a, "state"), id)
	if err != nil {
		return nil, err
	}
	format, err := render.ParseFormat(cliutil.FirstNonEmpty(mcp.ArgString(a, "format"), "json"))
	if err != nil {
		return nil, err
	}
	var stdout, stderr bytes.Buffer
	if err := render.PullRequests(&stdout, &stderr, res, format, id > 0); err != nil {
		return nil, err
	}
	if len(include) > 0 {
		return showParts(ctx, cfg, a, stdout.Bytes(), include, raw)
	}
	return stdout.Bytes(), nil
}
