package main

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
	"time"
)

// The owner cannot start with a dot, so ".." never gets into a request path; a repository may be named ".github".
var ghRepoRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_.-]+$`)

// ghRepo picks the repository (owner/name) of a call: the argument, then github.default_repo, then the only listed one.
func ghRepo(cfg *Config, repo string) (string, error) {
	if repo == "" {
		repo = cfg.GitHub.DefaultRepo
	}
	if repo == "" && len(cfg.GitHub.Repos) == 1 {
		repo = cfg.GitHub.Repos[0]
	}
	if repo == "" {
		return "", fmt.Errorf("GitHub repository not specified (owner/name) and github.default_repo is empty")
	}
	if !ghRepoRe.MatchString(repo) || strings.HasSuffix(repo, "/.") || strings.HasSuffix(repo, "/..") {
		return "", fmt.Errorf("invalid repository %q, expected owner/name", repo)
	}
	return repo, nil
}

func githubGet(ctx context.Context, c *APIClient, path string, q url.Values) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, path, q, nil, ""))
}

func perPage100() url.Values { return url.Values{"per_page": {"100"}} }

func githubUser(ctx context.Context, c *APIClient) ([]byte, error) {
	return githubGet(ctx, c, "/user", nil)
}

func githubGetRepo(ctx context.Context, c *APIClient, repo string) ([]byte, error) {
	return githubGet(ctx, c, "/repos/"+repo, nil)
}

func githubPRPath(repo string, n int, tail string) string {
	return fmt.Sprintf("/repos/%s/pulls/%d%s", repo, n, tail)
}

func githubGetPR(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, githubPRPath(repo, n, ""), nil)
}

func githubPRFiles(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, githubPRPath(repo, n, "/files"), perPage100())
}

func githubPRReviews(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, githubPRPath(repo, n, "/reviews"), perPage100())
}

// githubPRComments are the inline review comments (the threads on the diff).
func githubPRComments(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, githubPRPath(repo, n, "/comments"), perPage100())
}

// githubPRDiscussion is the conversation tab: the plain comments of the pull request.
func githubPRDiscussion(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, n), perPage100())
}

// githubPRChecks lists the check runs of the head commit of a pull request.
func githubPRChecks(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	b, err := githubGetPR(ctx, c, repo, n)
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
	return githubGet(ctx, c, "/repos/"+repo+"/commits/"+url.PathEscape(pr.Head.SHA)+"/check-runs", perPage100())
}

func githubListPRs(ctx context.Context, c *APIClient, repo, search, state string) ([]byte, error) {
	if state == "" {
		state = "all"
	}
	if search == "" {
		q := perPage100()
		q.Set("state", state)
		return githubGet(ctx, c, "/repos/"+repo+"/pulls", q)
	}
	query := "repo:" + repo + " is:pr " + search
	switch state {
	case "open", "closed":
		query += " is:" + state
	case "merged":
		query += " is:merged"
	}
	q := perPage100()
	q.Set("q", query)
	return githubGet(ctx, c, "/search/issues", q)
}

func githubListBranches(ctx context.Context, c *APIClient, repo, search string) ([]byte, error) {
	b, err := githubGet(ctx, c, "/repos/"+repo+"/branches", perPage100())
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

func githubGetFile(ctx context.Context, c *APIClient, repo, filePath, ref string) ([]byte, error) {
	segs := strings.Split(strings.Trim(filePath, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	q := url.Values{}
	if ref != "" {
		q.Set("ref", ref)
	}
	raw := *c
	raw.headers = c.headers.Clone()
	raw.headers.Set("Accept", "application/vnd.github.raw+json")
	return githubGet(ctx, &raw, "/repos/"+repo+"/contents/"+strings.Join(segs, "/"), q)
}

func githubListRuns(ctx context.Context, c *APIClient, repo, branch string, perPage int) ([]byte, error) {
	q := url.Values{"per_page": {strconv.Itoa(mustPositive(perPage, 30))}}
	if branch != "" {
		q.Set("branch", branch)
	}
	return githubGet(ctx, c, "/repos/"+repo+"/actions/runs", q)
}

func githubGetRun(ctx context.Context, c *APIClient, repo string, id int) ([]byte, error) {
	return githubGet(ctx, c, fmt.Sprintf("/repos/%s/actions/runs/%d", repo, id), nil)
}

func githubRunJobs(ctx context.Context, c *APIClient, repo string, id int) ([]byte, error) {
	return githubGet(ctx, c, fmt.Sprintf("/repos/%s/actions/runs/%d/jobs", repo, id), perPage100())
}

func githubAddComment(ctx context.Context, c *APIClient, repo string, n int, text string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{"body": text})
	return bodyOnly(c.do(ctx, http.MethodPost, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, n), nil, payload, "application/json"))
}

func githubCreatePR(ctx context.Context, c *APIClient, repo, head, base, title, body string, draft bool) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"head": head, "base": base, "title": title, "body": body, "draft": draft})
	return bodyOnly(c.do(ctx, http.MethodPost, "/repos/"+repo+"/pulls", nil, payload, "application/json"))
}

func repoFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs, fs.String("repo", "", "owner/name (default from config)")
}

func cmdGitHub(cfg *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("github commands: init, set-token, repo, pr, branches, runs, run, file, raw")
	}
	c, err := newGitHubClient(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	// idCommand parses `<name> <action> [--repo R] ID` and calls fn.
	idCommand := func(name string, rest []string, fn func(repo string, id int) ([]byte, error)) error {
		fs, r := repoFlagSet("github " + name)
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: github %s [--repo R] NUMBER", name)
		}
		id, err := strconv.Atoi(fs.Arg(0))
		if err != nil {
			return err
		}
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := fn(repo, id)
		if err != nil {
			return err
		}
		return printBody(b)
	}
	switch args[0] {
	case "repo":
		fs, r := repoFlagSet("github repo")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := githubGetRepo(ctx, c, repo)
		if err != nil {
			return err
		}
		return printBody(b)
	case "pr":
		return cmdGitHubPR(ctx, c, cfg, args[1:], idCommand)
	case "branches":
		fs, r := repoFlagSet("github branches")
		search := fs.String("search", "", "search")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := githubListBranches(ctx, c, repo, *search)
		if err != nil {
			return err
		}
		return printBody(b)
	case "runs":
		fs, r := repoFlagSet("github runs")
		branch := fs.String("branch", "", "branch")
		per := fs.Int("per-page", 30, "per page")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := githubListRuns(ctx, c, repo, *branch, *per)
		if err != nil {
			return err
		}
		return printBody(b)
	case "run":
		if len(args) < 2 {
			return fmt.Errorf("usage: github run get|jobs [--repo R] RUN_ID")
		}
		switch args[1] {
		case "get":
			return idCommand("run get", args[2:], func(r string, id int) ([]byte, error) { return githubGetRun(ctx, c, r, id) })
		case "jobs":
			return idCommand("run jobs", args[2:], func(r string, id int) ([]byte, error) { return githubRunJobs(ctx, c, r, id) })
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
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := githubGetFile(ctx, c, repo, fs.Arg(0), *ref)
		if err != nil {
			return err
		}
		return printBody(b)
	case "raw":
		return cmdRaw(ctx, c, cfg, args[1:])
	default:
		return fmt.Errorf("unknown github command %q", args[0])
	}
}

func cmdGitHubPR(ctx context.Context, c *APIClient, cfg *Config, args []string, idCommand func(string, []string, func(string, int) ([]byte, error)) error) error {
	if len(args) == 0 {
		return fmt.Errorf("pr actions: get, files, reviews, comments, discussion, checks, list, comment, create")
	}
	action, rest := args[0], args[1:]
	reads := map[string]func(string, int) ([]byte, error){
		"get":        func(r string, n int) ([]byte, error) { return githubGetPR(ctx, c, r, n) },
		"files":      func(r string, n int) ([]byte, error) { return githubPRFiles(ctx, c, r, n) },
		"reviews":    func(r string, n int) ([]byte, error) { return githubPRReviews(ctx, c, r, n) },
		"comments":   func(r string, n int) ([]byte, error) { return githubPRComments(ctx, c, r, n) },
		"discussion": func(r string, n int) ([]byte, error) { return githubPRDiscussion(ctx, c, r, n) },
		"checks":     func(r string, n int) ([]byte, error) { return githubPRChecks(ctx, c, r, n) },
	}
	if fn, ok := reads[action]; ok {
		return idCommand("pr "+action, rest, fn)
	}
	switch action {
	case "list":
		fs, r := repoFlagSet("github pr list")
		search := fs.String("search", "", "search text")
		state := fs.String("state", "all", "open, closed, merged or all")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := githubListPRs(ctx, c, repo, *search, *state)
		if err != nil {
			return err
		}
		return printBody(b)
	case "comment":
		if len(rest) < 1 || rest[0] != "add" {
			return fmt.Errorf("usage: github pr comment add [--repo R] --body TEXT NUMBER")
		}
		if err := requireWrite(cfg); err != nil {
			return err
		}
		fs, r := repoFlagSet("github pr comment add")
		body := fs.String("body", "", "comment body")
		if err := fs.Parse(rest[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *body == "" {
			return fmt.Errorf("NUMBER and --body required")
		}
		n, err := strconv.Atoi(fs.Arg(0))
		if err != nil {
			return err
		}
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := githubAddComment(ctx, c, repo, n, *body)
		if err != nil {
			return err
		}
		return printBody(b)
	case "create":
		if err := requireWrite(cfg); err != nil {
			return err
		}
		fs, r := repoFlagSet("github pr create")
		head := fs.String("head", "", "source branch (or owner:branch)")
		base := fs.String("base", "main", "target branch")
		title := fs.String("title", "", "title")
		body := fs.String("body", "", "description")
		draft := fs.Bool("draft", true, "create a draft pull request")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		if *head == "" || *title == "" {
			return fmt.Errorf("--head and --title required")
		}
		repo, err := ghRepo(cfg, *r)
		if err != nil {
			return err
		}
		b, err := githubCreatePR(ctx, c, repo, *head, *base, *title, *body, *draft)
		if err != nil {
			return err
		}
		return printBody(b)
	}
	return fmt.Errorf("unknown pr action %q", action)
}

// githubTools are the MCP tools of the GitHub module; write tools exist only with policy.allow_write.
func githubTools(cfg *Config) []mcpTool {
	repo := strProp("Repository owner/name; omit to use the default")
	num := intProp("Pull request number")
	byNum := objSchema(map[string]any{"repo": repo, "number": num}, "number")
	tools := []mcpTool{
		{"github_get_repo", "Get GitHub repository metadata.", objSchema(map[string]any{"repo": repo})},
		{"github_get_pull_request", "Get a GitHub pull request.", byNum},
		{"github_get_pull_request_files", "Get the changed files and patches of a pull request.", byNum},
		{"github_get_pull_request_reviews", "Get the reviews (approve, request changes, comment) of a pull request.", byNum},
		{"github_get_pull_request_comments", "Get the inline review comments (threads on the diff) of a pull request.", byNum},
		{"github_get_pull_request_discussion", "Get the conversation comments of a pull request.", byNum},
		{"github_get_pull_request_checks", "Get the check runs of the head commit of a pull request.", byNum},
		{"github_list_pull_requests", "List or search pull requests of a repository.", objSchema(map[string]any{"repo": repo, "search": strProp("Search text"), "state": strProp("open, closed, merged, all")})},
		{"github_list_workflow_runs", "List GitHub Actions runs, optionally for a branch.", objSchema(map[string]any{"repo": repo, "branch": strProp("Branch"), "per_page": intProp("Items per page")})},
		{"github_get_workflow_run", "Get one GitHub Actions run.", objSchema(map[string]any{"repo": repo, "id": intProp("Run ID")}, "id")},
		{"github_get_workflow_run_jobs", "Get the jobs of a GitHub Actions run.", objSchema(map[string]any{"repo": repo, "id": intProp("Run ID")}, "id")},
		{"github_list_branches", "List or search branches of a repository.", objSchema(map[string]any{"repo": repo, "search": strProp("Branch search text")})},
		{"github_get_file", "Get raw repository file contents at a ref.", objSchema(map[string]any{"repo": repo, "path": strProp("Repository-relative file path"), "ref": strProp("Branch, tag or commit; default branch if omitted")}, "path")},
	}
	if cfg.Policy.AllowWrite {
		tools = append(tools,
			mcpTool{"github_add_pull_request_comment", "Add a comment to a pull request. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"repo": repo, "number": num, "body": strProp("Comment body")}, "number", "body")},
			mcpTool{"github_create_pull_request", "Create a pull request. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"repo": repo, "head": strProp("Source branch"), "base": strProp("Target branch"), "title": strProp("Title"), "body": strProp("Description"), "draft": boolProp("Create as draft")}, "head", "base", "title")},
		)
	}
	return tools
}

func callGitHubTool(ctx context.Context, cfg *Config, name string, a map[string]any) ([]byte, error) {
	if !toolAvailable(cfg, name) {
		return nil, fmt.Errorf("tool %q is not available: its backend is not configured", name)
	}
	c, err := newGitHubClient(cfg)
	if err != nil {
		return nil, err
	}
	repo, err := ghRepo(cfg, argString(a, "repo"))
	if err != nil {
		return nil, err
	}
	n, id := argInt(a, "number", 0), argInt(a, "id", 0)
	switch name {
	case "github_get_repo":
		return githubGetRepo(ctx, c, repo)
	case "github_get_pull_request":
		return githubGetPR(ctx, c, repo, n)
	case "github_get_pull_request_files":
		return githubPRFiles(ctx, c, repo, n)
	case "github_get_pull_request_reviews":
		return githubPRReviews(ctx, c, repo, n)
	case "github_get_pull_request_comments":
		return githubPRComments(ctx, c, repo, n)
	case "github_get_pull_request_discussion":
		return githubPRDiscussion(ctx, c, repo, n)
	case "github_get_pull_request_checks":
		return githubPRChecks(ctx, c, repo, n)
	case "github_list_pull_requests":
		return githubListPRs(ctx, c, repo, argString(a, "search"), argString(a, "state"))
	case "github_list_workflow_runs":
		return githubListRuns(ctx, c, repo, argString(a, "branch"), argInt(a, "per_page", 30))
	case "github_get_workflow_run":
		return githubGetRun(ctx, c, repo, id)
	case "github_get_workflow_run_jobs":
		return githubRunJobs(ctx, c, repo, id)
	case "github_list_branches":
		return githubListBranches(ctx, c, repo, argString(a, "search"))
	case "github_get_file":
		return githubGetFile(ctx, c, repo, argString(a, "path"), argString(a, "ref"))
	case "github_add_pull_request_comment":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		return githubAddComment(ctx, c, repo, n, argString(a, "body"))
	case "github_create_pull_request":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		return githubCreatePR(ctx, c, repo, argString(a, "head"), argString(a, "base"), argString(a, "title"), argString(a, "body"), argBool(a, "draft"))
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
