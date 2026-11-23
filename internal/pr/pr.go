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
	"strings"

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
	formatValue := fs.String("format", "text", "text, json or compact (json, details compacted); raw: json with the provider details and, with show --include, the provider JSON of each part")
	include := fs.String("include", "", "show: extra parts, comma separated: files, diff, reviews, reviewers, comments, discussion, checks")
	jsonOutput := fs.Bool("json", false, "equivalent to --format json")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *jsonOutput {
		*formatValue = "json"
	}
	formatName, rawFormat, err := resolveFormat(*formatValue)
	if err != nil {
		return err
	}
	format, err := render.ParseFormat(formatName)
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
		res, err := Fetch(ctx, cfg, name, profile, FetchRequest{Repository: *repository, Project: *project, Search: *search, State: *state})
		if err != nil {
			return err
		}
		if !rawFormat {
			compactItems(&res)
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
		if *include != "" || rawFormat {
			// The same code as the pr_show tool, so that CLI and MCP return the same parts.
			a := map[string]any{"number": id, "repo": *repository, "project": *project, "profile": name, "include": splitList(*include)}
			if rawFormat {
				a["format"] = "raw"
			}
			out, err := CallTool(ctx, cfg, "pr_show", a)
			if err != nil {
				return err
			}
			return cliutil.PrintBody(out)
		}
		res, err := Fetch(ctx, cfg, name, profile, FetchRequest{Repository: *repository, Project: *project, ID: id})
		if err != nil {
			return err
		}
		compactItems(&res)
		return render.PullRequests(os.Stdout, os.Stderr, res, format, true)
	default:
		return fmt.Errorf("unknown pr action %q", args[0])
	}
}

// resolveFormat maps a --format / format value to the render format name and whether the provider data is kept:
// compact is the JSON output with the compact details (the tool default), raw is the same JSON without the projection.
func resolveFormat(value string) (name string, raw bool, err error) {
	switch v := strings.ToLower(strings.TrimSpace(value)); v {
	case "", "json", "text":
		return v, false, nil
	case "compact":
		return "json", false, nil
	case "raw":
		return "json", true, nil
	}
	return "", false, fmt.Errorf("unsupported format %q (use compact, raw, json or text)", value)
}

// compactItems replaces the provider details of every item with the short form; raw output skips it.
func compactItems(res *vcs.Result) {
	for i := range res.Items {
		res.Items[i].Details = compactDetails(res.Items[i].Details)
	}
}

// splitList turns "a, b" into []any{"a", "b"} for tool arguments; an empty string gives an empty list.
func splitList(s string) []any {
	var out []any
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// FetchRequest selects the pull requests Fetch returns; an ID above zero asks for that one pull request.
type FetchRequest struct {
	Repository, Project, Search, State string
	ID                                 int
}

// Fetch lists (ID 0) or gets one pull request; state is open, closed (not merged), merged or all.
func Fetch(ctx context.Context, cfg *config.Config, name string, profile config.Profile, req FetchRequest) (vcs.Result, error) {
	switch req.State {
	case "":
		req.State = "all"
	case "open", "closed", "merged", "all":
	default:
		return vcs.Result{}, fmt.Errorf("unsupported state %q (use open, closed, merged or all)", req.State)
	}
	if profile.Kind != "azure" {
		var err error
		if req.Repository, err = config.ProfileRepository(profile, req.Repository); err != nil {
			return vcs.Result{}, err
		}
	}
	reader, err := newReader(cfg, name, profile)
	if err != nil {
		return vcs.Result{}, err
	}
	opt := vcs.ListOptions{Repository: req.Repository, Project: req.Project, Search: req.Search, State: req.State}
	if req.ID > 0 {
		item, err := reader.GetPullRequest(ctx, opt, req.ID)
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
			"format":  mcp.StrProp("compact (default, same as json: details compacted), text, or raw (json with the provider details)"),
		})},
		{Name: "pr_show", Description: fmt.Sprintf("Get details and description of a pull request or merge request; include adds files, diff (unified diff text, capped at %d bytes), reviews, reviewers, comments (inline), discussion and checks (GitHub and GitLab).", diffCap), InputSchema: mcp.ObjSchema(map[string]any{
			"number":  mcp.IntProp("Pull request or merge request number"),
			"id":      mcp.IntProp("Same as number (older name)"),
			"include": mcp.ArrProp(fmt.Sprintf("Optional parts: files, diff, reviews, reviewers, comments, discussion, checks. diff is a string of unified diff text; over %d bytes it is an object {diff, truncated, bytes_total, bytes_returned, hint}", diffCap)),
			"profile": mcp.StrProp("Profile name (optional if default profile exists)"),
			"repo":    mcp.StrProp("Repository name or path (optional if default_repo is set)"),
			"project": mcp.StrProp("Project name (required for Azure DevOps unless set in profile)"),
			"format":  mcp.StrProp("compact (default, same as json), text, or raw: with include, parts are compact (the same fields for GitHub and GitLab) unless format is raw"),
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
	formatName, raw, err := resolveFormat(mcp.ArgString(a, "format"))
	if err != nil {
		return nil, err
	}
	if formatName == "" || len(include) > 0 {
		formatName = "json"
	}
	pName, profile, err := cfg.PRProfileFor(mcp.ArgString(a, "profile"), "", cliutil.FirstNonEmpty(mcp.ArgString(a, "repo"), mcp.ArgString(a, "project")))
	if err != nil {
		return nil, err
	}
	res, err := Fetch(ctx, cfg, pName, profile, FetchRequest{Repository: mcp.ArgString(a, "repo"), Project: mcp.ArgString(a, "project"), Search: mcp.ArgString(a, "search"), State: mcp.ArgString(a, "state"), ID: id})
	if err != nil {
		return nil, err
	}
	if !raw {
		compactItems(&res)
	}
	format, err := render.ParseFormat(formatName)
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
