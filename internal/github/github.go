// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package github is the thin GitHub provider: REST calls, the `workctl github ...`
// commands, the github_* MCP tools and the provider-neutral pull request adapter.
package github

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
)

// The owner cannot start with a dot, so ".." never gets into a request path; a repository may be named ".github".
var repoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

// repoOf picks the repository (owner/name) of a call: the argument, then the github profile defaults.
func repoOf(cfg *config.Config, repo string) (string, error) {
	_, profile, _ := cfg.ProfileOf("github")
	repo, err := config.ProfileRepository(profile, repo)
	if err != nil {
		return "", err
	}
	if !repoRe.MatchString(repo) || strings.HasSuffix(repo, "/.") || strings.HasSuffix(repo, "/..") {
		return "", fmt.Errorf("invalid repository %q, expected owner/name", repo)
	}
	return repo, nil
}

func get(ctx context.Context, c *api.Client, path string, q url.Values) ([]byte, error) {
	return api.BodyOnly(c.Do(ctx, http.MethodGet, path, q, nil, ""))
}

func perPage100() url.Values { return url.Values{"per_page": {"100"}} }

// maxPages bounds a pull request list: 30 pages of 100 cover GitHub's own limit of 3000 files.
const maxPages = 30

// User fetches the authenticated user; doctor and the setup wizard use it as the connectivity check.
func User(ctx context.Context, c *api.Client) ([]byte, error) {
	return get(ctx, c, "/user", nil)
}

func getRepo(ctx context.Context, c *api.Client, repo string) ([]byte, error) {
	return get(ctx, c, "/repos/"+repo, nil)
}

func prPath(repo string, n int, tail string) string {
	return fmt.Sprintf("/repos/%s/pulls/%d%s", repo, n, tail)
}

func prFiles(ctx context.Context, c *api.Client, repo string, n int) ([]byte, error) {
	return c.GetAllPages(ctx, prPath(repo, n, "/files"), nil, maxPages)
}

func prReviews(ctx context.Context, c *api.Client, repo string, n int) ([]byte, error) {
	return c.GetAllPages(ctx, prPath(repo, n, "/reviews"), nil, maxPages)
}

// prRequestedReviewers lists the requested reviewers and teams of a pull request.
func prRequestedReviewers(ctx context.Context, c *api.Client, repo string, n int) ([]byte, error) {
	return get(ctx, c, prPath(repo, n, "/requested_reviewers"), perPage100())
}

// prComments are the inline review comments (the threads on the diff).
func prComments(ctx context.Context, c *api.Client, repo string, n int) ([]byte, error) {
	return c.GetAllPages(ctx, prPath(repo, n, "/comments"), nil, maxPages)
}

// prDiscussion is the conversation tab: the plain comments of the pull request.
func prDiscussion(ctx context.Context, c *api.Client, repo string, n int) ([]byte, error) {
	return c.GetAllPages(ctx, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, n), nil, maxPages)
}

// prChecks lists the check runs of the head commit of a pull request.
func prChecks(ctx context.Context, c *api.Client, repo string, n int) ([]byte, error) {
	b, err := getPR(ctx, c, repo, n)
	if err != nil {
		return nil, err
	}
	var pr struct {
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := json.Unmarshal(b, &pr); err != nil || pr.Head.SHA == "" {
		return nil, fmt.Errorf("pull request %d has no head commit", n)
	}
	return get(ctx, c, "/repos/"+repo+"/commits/"+url.PathEscape(pr.Head.SHA)+"/check-runs", perPage100())
}

func listBranches(ctx context.Context, c *api.Client, repo, search string) ([]byte, error) {
	b, err := get(ctx, c, "/repos/"+repo+"/branches", perPage100())
	if err != nil || search == "" {
		return b, err
	}
	var all []map[string]any
	if err := json.Unmarshal(b, &all); err != nil {
		return b, nil
	}
	kept := []map[string]any{}
	for _, br := range all {
		if name, _ := br["name"].(string); strings.Contains(strings.ToLower(name), strings.ToLower(search)) {
			kept = append(kept, br)
		}
	}
	return json.MarshalIndent(kept, "", "  ")
}

func getFile(ctx context.Context, c *api.Client, repo, filePath, ref string) ([]byte, error) {
	segs := strings.Split(strings.Trim(filePath, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	q := url.Values{}
	if ref != "" {
		q.Set("ref", ref)
	}
	raw := c.WithAccept("application/vnd.github.raw+json")
	return get(ctx, raw, "/repos/"+repo+"/contents/"+strings.Join(segs, "/"), q)
}

func listRuns(ctx context.Context, c *api.Client, repo, branch string, perPage int) ([]byte, error) {
	q := url.Values{"per_page": {strconv.Itoa(cliutil.MustPositive(perPage, 30))}}
	if branch != "" {
		q.Set("branch", branch)
	}
	return get(ctx, c, "/repos/"+repo+"/actions/runs", q)
}

func getRun(ctx context.Context, c *api.Client, repo string, id int) ([]byte, error) {
	return get(ctx, c, fmt.Sprintf("/repos/%s/actions/runs/%d", repo, id), nil)
}

func runJobs(ctx context.Context, c *api.Client, repo string, id int) ([]byte, error) {
	return get(ctx, c, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs", repo, id), perPage100())
}

func addComment(ctx context.Context, c *api.Client, repo string, n int, text string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{"body": text})
	return api.BodyOnly(c.Do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, n), nil, payload, "application/json"))
}

func requestReviewers(ctx context.Context, c *api.Client, repo string, n int, reviewers []string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"reviewers": reviewers})
	return api.BodyOnly(c.Do(ctx, http.MethodPost, prPath(repo, n, "/requested_reviewers"), nil, payload, "application/json"))
}

func removeReviewers(ctx context.Context, c *api.Client, repo string, n int, reviewers []string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"reviewers": reviewers})
	return api.BodyOnly(c.Do(ctx, http.MethodDelete, prPath(repo, n, "/requested_reviewers"), nil, payload, "application/json"))
}

func editPR(ctx context.Context, c *api.Client, repo string, n int, fields map[string]any) ([]byte, error) {
	payload, _ := json.Marshal(fields)
	return api.BodyOnly(c.Do(ctx, http.MethodPatch, prPath(repo, n, ""), nil, payload, "application/json"))
}

func prEditFields(title, body, base, state string) map[string]any {
	fields := map[string]any{}
	for key, value := range map[string]string{"title": title, "body": body, "base": base, "state": state} {
		if value != "" {
			fields[key] = value
		}
	}
	return fields
}

func applyPREdit(ctx context.Context, c *api.Client, repo string, id int, fields map[string]any, add, remove []string) ([]byte, error) {
	var last []byte
	var err error
	if len(fields) > 0 {
		last, err = editPR(ctx, c, repo, id, fields)
		if err != nil {
			return nil, err
		}
	}
	for _, operation := range []struct {
		users []string
		call  func(context.Context, *api.Client, string, int, []string) ([]byte, error)
	}{{add, requestReviewers}, {remove, removeReviewers}} {
		if len(operation.users) > 0 {
			last, err = operation.call(ctx, c, repo, id, operation.users)
			if err != nil {
				return nil, err
			}
		}
	}
	return last, nil
}

func createPR(ctx context.Context, c *api.Client, repo, head, base, title, body string, draft bool, reviewers []string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"head": head, "base": base, "title": title, "body": body, "draft": draft})
	res, err := api.BodyOnly(c.Do(ctx, http.MethodPost, "/repos/"+repo+"/pulls", nil, payload, "application/json"))
	if err != nil || len(reviewers) == 0 {
		return res, err
	}
	var created struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(res, &created); err == nil && created.Number > 0 {
		if _, revErr := requestReviewers(ctx, c, repo, created.Number, reviewers); revErr != nil {
			return res, fmt.Errorf("created pull request #%d, but failed to request reviewers: %w", created.Number, revErr)
		}
	}
	return res, nil
}

func repoFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs, fs.String("repo", "", "owner/name (default from config)")
}

// Command runs `workctl github ...`.
func Command(cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("github commands: init, repo, pr, branches, runs, run, file, raw")
	}
	cfg, err := cfg.ScopedFor("", "github", cliutil.FlagValue(args[1:], "repo"))
	if err != nil {
		return err
	}
	c, err := api.NewKindClient(cfg, "github")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.CommandTimeout("github"))
	defer cancel()
	// idCommand parses `<name> <action> [--repo R] ID` and calls fn.
	idCommand := func(name string, rest []string, fn func(repo string, id int) ([]byte, error)) error {
		fs, r := repoFlagSet("github " + name)
		if err := fs.Parse(cliutil.ReorderIDToBack(rest)); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: github %s [--repo R] NUMBER", name)
		}
		id, err := strconv.Atoi(fs.Arg(0))
		if err != nil {
			return err
		}
		repo, err := repoOf(cfg, *r)
		if err != nil {
			return err
		}
		b, err := fn(repo, id)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	}
	switch args[0] {
	case "repo":
		fs, r := repoFlagSet("github repo")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		repo, err := repoOf(cfg, *r)
		if err != nil {
			return err
		}
		b, err := getRepo(ctx, c, repo)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "pr":
		return prCommand(ctx, c, cfg, args[1:], idCommand)
	case "branches":
		fs, r := repoFlagSet("github branches")
		search := fs.String("search", "", "search")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		repo, err := repoOf(cfg, *r)
		if err != nil {
			return err
		}
		b, err := listBranches(ctx, c, repo, *search)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "runs":
		fs, r := repoFlagSet("github runs")
		branch := fs.String("branch", "", "branch")
		per := fs.Int("per-page", 30, "per page")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		repo, err := repoOf(cfg, *r)
		if err != nil {
			return err
		}
		b, err := listRuns(ctx, c, repo, *branch, *per)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: github run get|jobs [--repo R] RUN_ID")
		}
		switch args[1] {
		case "get":
			return idCommand("run get", args[2:], func(r string, id int) ([]byte, error) { return getRun(ctx, c, r, id) })
		case "jobs":
			return idCommand("run jobs", args[2:], func(r string, id int) ([]byte, error) { return runJobs(ctx, c, r, id) })
		}
		return fmt.Errorf("unknown run action %q", args[1])
	case "file":
		if len(args) < 2 || args[1] != "get" {
			return fmt.Errorf("usage: github file get [--repo R] [--ref R] PATH")
		}
		fs, r := repoFlagSet("github file get")
		ref := fs.String("ref", "", "branch, tag or commit")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("file path required")
		}
		repo, err := repoOf(cfg, *r)
		if err != nil {
			return err
		}
		b, err := getFile(ctx, c, repo, fs.Arg(0), *ref)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "raw":
		return api.RawCommand(ctx, c, cfg, args[1:])
	default:
		return fmt.Errorf("unknown github command %q", args[0])
	}
}

type prRead struct {
	tool string
	read func(context.Context, *api.Client, string, int) ([]byte, error)
}

// prReads maps the CLI pr actions to their MCP tool names and read calls; the one table serves both surfaces.
var prReads = map[string]prRead{
	"get":        {"github_get_pull_request", getPR},
	"files":      {"github_get_pull_request_files", prFiles},
	"reviews":    {"github_get_pull_request_reviews", prReviews},
	"reviewers":  {"github_get_pull_request_reviewers", prRequestedReviewers},
	"comments":   {"github_get_pull_request_comments", prComments},
	"discussion": {"github_get_pull_request_discussion", prDiscussion},
	"checks":     {"github_get_pull_request_checks", prChecks},
}

func prCommand(ctx context.Context, c *api.Client, cfg *config.Config, args []string, idCommand func(string, []string, func(string, int) ([]byte, error)) error) error {
	if len(args) == 0 {
		return fmt.Errorf("pr actions: get, files, reviews, reviewers, comments, discussion, checks, list, comment, create, edit")
	}
	action, rest := args[0], args[1:]
	if entry, ok := prReads[action]; ok {
		return idCommand("pr "+action, rest, func(repo string, id int) ([]byte, error) { return entry.read(ctx, c, repo, id) })
	}
	handlers := map[string]func(context.Context, *api.Client, *config.Config, []string) error{
		"list": prListCommand, "comment": prCommentCommand,
		"create": prCreateCommand, "edit": prEditCommand,
	}
	if handler := handlers[action]; handler != nil {
		return handler(ctx, c, cfg, rest)
	}
	return fmt.Errorf("unknown pr action %q", action)
}

func prListCommand(ctx context.Context, c *api.Client, cfg *config.Config, rest []string) error {
	fs, r := repoFlagSet("github pr list")
	search := fs.String("search", "", "search text")
	state := fs.String("state", "all", "open, closed, merged or all")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	repo, err := repoOf(cfg, *r)
	if err != nil {
		return err
	}
	b, err := listPRs(ctx, c, repo, *search, *state)
	if err != nil {
		return err
	}
	return cliutil.PrintBody(b)
}

func prCommentCommand(ctx context.Context, c *api.Client, cfg *config.Config, rest []string) error {
	if len(rest) < 1 || rest[0] != "add" {
		return fmt.Errorf("usage: github pr comment add [--repo R] --body TEXT NUMBER")
	}
	if err := cfg.RequireWrite(); err != nil {
		return err
	}
	fs, r := repoFlagSet("github pr comment add")
	body := fs.String("body", "", "comment body")
	if err := fs.Parse(cliutil.ReorderIDToBack(rest[1:])); err != nil {
		return err
	}
	if fs.NArg() != 1 || *body == "" {
		return fmt.Errorf("NUMBER and --body required")
	}
	n, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return err
	}
	repo, err := repoOf(cfg, *r)
	if err != nil {
		return err
	}
	b, err := addComment(ctx, c, repo, n, *body)
	if err != nil {
		return err
	}
	return cliutil.PrintBody(b)
}

func prCreateCommand(ctx context.Context, c *api.Client, cfg *config.Config, rest []string) error {
	if err := cfg.RequireWrite(); err != nil {
		return err
	}
	fs, r := repoFlagSet("github pr create")
	head := fs.String("head", "", "source branch (or owner:branch)")
	base := fs.String("base", "main", "target branch")
	title := fs.String("title", "", "title")
	body := fs.String("body", "", "description")
	draft := fs.Bool("draft", true, "create a draft pull request")
	reviewer := fs.String("reviewer", "", "comma-separated reviewers to request")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *head == "" || *title == "" {
		return fmt.Errorf("--head and --title required")
	}
	repo, err := repoOf(cfg, *r)
	if err != nil {
		return err
	}
	b, err := createPR(ctx, c, repo, *head, *base, *title, *body, *draft, cliutil.SplitCommaList(*reviewer))
	if err != nil {
		return err
	}
	return cliutil.PrintBody(b)
}

func prEditCommand(ctx context.Context, c *api.Client, cfg *config.Config, rest []string) error {
	if err := cfg.RequireWrite(); err != nil {
		return err
	}
	fs, r := repoFlagSet("github pr edit")
	title := fs.String("title", "", "new title")
	body := fs.String("body", "", "new description")
	base := fs.String("base", "", "new target branch")
	state := fs.String("state", "", "state: open or closed")
	addReviewers := fs.String("add-reviewer", "", "comma-separated reviewers to add")
	removeReviewersFlag := fs.String("remove-reviewer", "", "comma-separated reviewers to remove")
	if err := fs.Parse(cliutil.ReorderIDToBack(rest)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: github pr edit [--repo R] [--title T] [--body B] [--base B] [--state open|closed] [--add-reviewer USERS] [--remove-reviewer USERS] NUMBER")
	}
	n, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return err
	}
	repo, err := repoOf(cfg, *r)
	if err != nil {
		return err
	}
	if *state != "" && *state != "open" && *state != "closed" {
		return fmt.Errorf("invalid state %q, expected open or closed", *state)
	}
	fields := prEditFields(*title, *body, *base, *state)
	if len(fields) == 0 && *addReviewers == "" && *removeReviewersFlag == "" {
		return fmt.Errorf("at least one edit flag is required (--title, --body, --base, --state, --add-reviewer, --remove-reviewer)")
	}
	lastResult, err := applyPREdit(ctx, c, repo, n, fields, cliutil.SplitCommaList(*addReviewers), cliutil.SplitCommaList(*removeReviewersFlag))
	if err != nil {
		return err
	}
	return cliutil.PrintBody(lastResult)
}
