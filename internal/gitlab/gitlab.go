// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package gitlab is the thin GitLab provider: REST calls, the `workctl gitlab ...`
// commands, the gitlab_* MCP tools and the provider-neutral merge request adapter.
package gitlab

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
)

// projectOf picks the project (group/name) of a call: the argument, then the gitlab profile defaults.
func projectOf(cfg *config.Config, project string) (string, error) {
	_, profile, _ := cfg.ProfileOf("gitlab")
	return config.ProfileRepository(profile, project)
}

func projectID(project string) string { return url.PathEscape(project) }

func get(ctx context.Context, c *api.Client, path string, q url.Values) ([]byte, error) {
	return api.BodyOnly(c.Do(ctx, http.MethodGet, path, q, nil, ""))
}

func getProject(ctx context.Context, c *api.Client, project string) ([]byte, error) {
	return get(ctx, c, "/api/v4/projects/"+projectID(project), nil)
}

func getMRChanges(ctx context.Context, c *api.Client, project string, iid int) ([]byte, error) {
	return get(ctx, c, fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/changes", projectID(project), iid), nil)
}

func getMRNotes(ctx context.Context, c *api.Client, project string, iid int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", projectID(project), iid)
	return get(ctx, c, p, url.Values{"per_page": {"100"}})
}

func addMRNote(ctx context.Context, c *api.Client, project string, iid int, text string) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests/%d/notes", projectID(project), iid)
	q := url.Values{"body": {text}}
	return api.BodyOnly(c.Do(ctx, http.MethodPost, p, q, nil, ""))
}

func createMR(ctx context.Context, c *api.Client, project, source, target, title, description string, draft bool) ([]byte, error) {
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
	p := fmt.Sprintf("/api/v4/projects/%s/merge_requests", projectID(project))
	return api.BodyOnly(c.Do(ctx, http.MethodPost, p, q, nil, ""))
}

func listPipelines(ctx context.Context, c *api.Client, project, ref string, perPage int) ([]byte, error) {
	q := url.Values{"per_page": {strconv.Itoa(cliutil.MustPositive(perPage, 30))}}
	if ref != "" {
		q.Set("ref", ref)
	}
	return get(ctx, c, fmt.Sprintf("/api/v4/projects/%s/pipelines", projectID(project)), q)
}

func getPipeline(ctx context.Context, c *api.Client, project string, id int) ([]byte, error) {
	return get(ctx, c, fmt.Sprintf("/api/v4/projects/%s/pipelines/%d", projectID(project), id), nil)
}

func listPipelineJobs(ctx context.Context, c *api.Client, project string, id int) ([]byte, error) {
	p := fmt.Sprintf("/api/v4/projects/%s/pipelines/%d/jobs", projectID(project), id)
	return get(ctx, c, p, url.Values{"per_page": {"100"}})
}

func jobTrace(ctx context.Context, c *api.Client, project string, id int) ([]byte, error) {
	return get(ctx, c, fmt.Sprintf("/api/v4/projects/%s/jobs/%d/trace", projectID(project), id), nil)
}

// ListBranches lists or searches branches; the task context uses it to find branches that mention an issue key.
func ListBranches(ctx context.Context, c *api.Client, project, search string) ([]byte, error) {
	q := url.Values{"per_page": {"100"}}
	if search != "" {
		q.Set("search", search)
	}
	return get(ctx, c, fmt.Sprintf("/api/v4/projects/%s/repository/branches", projectID(project)), q)
}

func getFile(ctx context.Context, c *api.Client, project, filePath, ref string) ([]byte, error) {
	if ref == "" {
		ref = "HEAD"
	}
	q := url.Values{"ref": {ref}}
	p := fmt.Sprintf("/api/v4/projects/%s/repository/files/%s/raw", projectID(project), url.PathEscape(filePath))
	return get(ctx, c, p, q)
}

// Version fetches the server version; doctor and the setup wizard use it as the connectivity check.
func Version(ctx context.Context, c *api.Client) ([]byte, error) {
	return get(ctx, c, "/api/v4/version", nil)
}

func projectFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	p := fs.String("project", "", "group/project (default from config)")
	return fs, p
}

// Command runs `workctl gitlab ...`.
func Command(cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("gitlab commands: project, mr, pipelines, pipeline, job, branches, file, raw")
	}
	cfg, err := cfg.ScopedFor("", "gitlab", cliutil.FlagValue(args[1:], "project"))
	if err != nil {
		return err
	}
	c, err := api.NewKindClient(cfg, "gitlab")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.CommandTimeout("gitlab"))
	defer cancel()
	switch args[0] {
	case "project":
		fs, p := projectFlagSet("gitlab project")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := getProject(ctx, c, project)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "mr":
		return mrCommand(ctx, c, cfg, args[1:])
	case "pipelines":
		if len(args) < 2 || args[1] != "list" {
			return fmt.Errorf("usage: gitlab pipelines list [--project P] [--ref R]")
		}
		fs, p := projectFlagSet("gitlab pipelines list")
		ref := fs.String("ref", "", "ref")
		per := fs.Int("per-page", 30, "per page")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := listPipelines(ctx, c, project, *ref, *per)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "pipeline":
		if len(args) < 2 {
			return fmt.Errorf("usage: gitlab pipeline get|jobs PIPELINE_ID")
		}
		action := args[1]
		fs, p := projectFlagSet("gitlab pipeline " + action)
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("pipeline ID required")
		}
		id, e := strconv.Atoi(fs.Arg(0))
		if e != nil {
			return e
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		var b []byte
		switch action {
		case "get":
			b, e = getPipeline(ctx, c, project, id)
		case "jobs":
			b, e = listPipelineJobs(ctx, c, project, id)
		default:
			return fmt.Errorf("unknown pipeline action %q", action)
		}
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "job":
		if len(args) < 2 || args[1] != "trace" {
			return fmt.Errorf("usage: gitlab job trace [--project P] JOB_ID")
		}
		fs, p := projectFlagSet("gitlab job trace")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("job ID required")
		}
		id, e := strconv.Atoi(fs.Arg(0))
		if e != nil {
			return e
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := jobTrace(ctx, c, project, id)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "branches":
		fs, p := projectFlagSet("gitlab branches")
		search := fs.String("search", "", "search")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := ListBranches(ctx, c, project, *search)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "file":
		if len(args) < 2 || args[1] != "get" {
			return fmt.Errorf("usage: gitlab file get [--project P] [--ref R] PATH")
		}
		fs, p := projectFlagSet("gitlab file get")
		ref := fs.String("ref", "HEAD", "ref")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("file path required")
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := getFile(ctx, c, project, fs.Arg(0), *ref)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "raw":
		return api.RawCommand(ctx, c, cfg, args[1:])
	default:
		return fmt.Errorf("unknown gitlab command %q", args[0])
	}
}

func mrCommand(ctx context.Context, c *api.Client, cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("mr actions: get, changes, notes, list, note, create")
	}
	action := args[0]
	switch action {
	case "get", "changes", "notes":
		fs, p := projectFlagSet("gitlab mr " + action)
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("MR IID required")
		}
		iid, e := strconv.Atoi(fs.Arg(0))
		if e != nil {
			return e
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		var b []byte
		switch action {
		case "get":
			b, e = getMR(ctx, c, project, iid)
		case "changes":
			b, e = getMRChanges(ctx, c, project, iid)
		default:
			b, e = getMRNotes(ctx, c, project, iid)
		}
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "list":
		fs, p := projectFlagSet("gitlab mr list")
		search := fs.String("search", "", "search")
		state := fs.String("state", "all", "state")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := ListMRs(ctx, c, project, *search, *state)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "note":
		if len(args) < 2 || args[1] != "add" {
			return fmt.Errorf("usage: gitlab mr note add [--project P] --body TEXT IID")
		}
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
		fs, p := projectFlagSet("gitlab mr note add")
		body := fs.String("body", "", "note body")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *body == "" {
			return fmt.Errorf("IID and --body required")
		}
		iid, e := strconv.Atoi(fs.Arg(0))
		if e != nil {
			return e
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := addMRNote(ctx, c, project, iid, *body)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	case "create":
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
		fs, p := projectFlagSet("gitlab mr create")
		source := fs.String("source", "", "source branch")
		target := fs.String("target", "master", "target branch")
		title := fs.String("title", "", "title")
		desc := fs.String("description", "", "description")
		draft := fs.Bool("draft", true, "create Draft MR")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *source == "" || *title == "" {
			return fmt.Errorf("--source and --title required")
		}
		project, e := projectOf(cfg, *p)
		if e != nil {
			return e
		}
		b, e := createMR(ctx, c, project, *source, *target, *title, *desc, *draft)
		if e != nil {
			return e
		}
		return cliutil.PrintBody(b)
	default:
		return fmt.Errorf("unknown mr action %q", action)
	}
}
