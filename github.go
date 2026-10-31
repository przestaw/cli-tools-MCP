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

// ghRepo picks the repository (owner/name) of a call: the argument, then the github profile defaults.
func ghRepo(cfg *Config, repo string) (string, error) {
	_, profile, _ := cfg.profileOf("github")
	repo, err := profileRepository(profile, repo)
	if err != nil {
		return "", err
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

func githubPRFiles(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, githubPRPath(repo, n, "/files"), perPage100())
}

func githubPRReviews(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, githubPRPath(repo, n, "/reviews"), perPage100())
}

// githubPRRequestedReviewers lists the requested reviewers and teams of a pull request.
func githubPRRequestedReviewers(ctx context.Context, c *APIClient, repo string, n int) ([]byte, error) {
	return githubGet(ctx, c, githubPRPath(repo, n, "/requested_reviewers"), perPage100())
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

func githubRequestReviewers(ctx context.Context, c *APIClient, repo string, n int, reviewers []string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"reviewers": reviewers})
	return bodyOnly(c.do(ctx, http.MethodPost, githubPRPath(repo, n, "/requested_reviewers"), nil, payload, "application/json"))
}

func githubRemoveReviewers(ctx context.Context, c *APIClient, repo string, n int, reviewers []string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"reviewers": reviewers})
	return bodyOnly(c.do(ctx, http.MethodDelete, githubPRPath(repo, n, "/requested_reviewers"), nil, payload, "application/json"))
}

func githubEditPR(ctx context.Context, c *APIClient, repo string, n int, fields map[string]any) ([]byte, error) {
	payload, _ := json.Marshal(fields)
	return bodyOnly(c.do(ctx, http.MethodPatch, githubPRPath(repo, n, ""), nil, payload, "application/json"))
}

func githubPREditFields(title, body, base, state string) map[string]any {
	fields := map[string]any{}
	for key, value := range map[string]string{"title": title, "body": body, "base": base, "state": state} {
		if value != "" {
			fields[key] = value
		}
	}
	return fields
}

func githubApplyPREdit(ctx context.Context, c *APIClient, repo string, id int, fields map[string]any, add, remove []string) ([]byte, error) {
	var last []byte
	var err error
	if len(fields) > 0 {
		last, err = githubEditPR(ctx, c, repo, id, fields)
		if err != nil {
			return nil, err
		}
	}
	for _, operation := range []struct {
		users []string
		call  func(context.Context, *APIClient, string, int, []string) ([]byte, error)
	}{{add, githubRequestReviewers}, {remove, githubRemoveReviewers}} {
		if len(operation.users) > 0 {
			last, err = operation.call(ctx, c, repo, id, operation.users)
			if err != nil {
				return nil, err
			}
		}
	}
	return last, nil
}

func githubCreatePR(ctx context.Context, c *APIClient, repo, head, base, title, body string, draft bool, reviewers []string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"head": head, "base": base, "title": title, "body": body, "draft": draft})
	res, err := bodyOnly(c.do(ctx, http.MethodPost, "/repos/"+repo+"/pulls", nil, payload, "application/json"))
	if err != nil || len(reviewers) == 0 {
		return res, err
	}
	var created struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(res, &created); err == nil && created.Number > 0 {
		if _, revErr := githubRequestReviewers(ctx, c, repo, created.Number, reviewers); revErr != nil {
			return res, fmt.Errorf("created pull request #%d, but failed to request reviewers: %w", created.Number, revErr)
		}
	}
	return res, nil
}

func splitCommaList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var res []string
	for _, item := range strings.Split(s, ",") {
		if t := strings.TrimSpace(item); t != "" {
			res = append(res, t)
		}
	}
	return res
}

// reorderIDToBack moves a leading integer argument to the back so flag.FlagSet can parse flags that follow it.
func reorderIDToBack(args []string) []string {
	if len(args) > 1 && !strings.HasPrefix(args[0], "-") {
		if _, err := strconv.Atoi(args[0]); err == nil {
			res := make([]string, 0, len(args))
			res = append(res, args[1:]...)
			res = append(res, args[0])
			return res
		}
	}
	return args
}

func repoFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	return fs, fs.String("repo", "", "owner/name (default from config)")
}

func cmdGitHub(cfg *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("github commands: init, repo, pr, branches, runs, run, file, raw")
	}
	c, err := newKindClient(cfg, "github")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	// idCommand parses `<name> <action> [--repo R] ID` and calls fn.
	idCommand := func(name string, rest []string, fn func(repo string, id int) ([]byte, error)) error {
		fs, r := repoFlagSet("github " + name)
		if err := fs.Parse(reorderIDToBack(rest)); err != nil {
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

type githubPRRead struct {
	tool string
	read func(context.Context, *APIClient, string, int) ([]byte, error)
}

var githubPRReads = map[string]githubPRRead{
	"get":        {"github_get_pull_request", githubGetPR},
	"files":      {"github_get_pull_request_files", githubPRFiles},
	"reviews":    {"github_get_pull_request_reviews", githubPRReviews},
	"reviewers":  {"github_get_pull_request_reviewers", githubPRRequestedReviewers},
	"comments":   {"github_get_pull_request_comments", githubPRComments},
	"discussion": {"github_get_pull_request_discussion", githubPRDiscussion},
	"checks":     {"github_get_pull_request_checks", githubPRChecks},
}

func cmdGitHubPR(ctx context.Context, c *APIClient, cfg *Config, args []string, idCommand func(string, []string, func(string, int) ([]byte, error)) error) error {
	if len(args) == 0 {
		return fmt.Errorf("pr actions: get, files, reviews, reviewers, comments, discussion, checks, list, comment, create, edit")
	}
	action, rest := args[0], args[1:]
	if entry, ok := githubPRReads[action]; ok {
		return idCommand("pr "+action, rest, func(repo string, id int) ([]byte, error) { return entry.read(ctx, c, repo, id) })
	}
	handlers := map[string]func(context.Context, *APIClient, *Config, []string) error{
		"list": cmdGitHubPRList, "comment": cmdGitHubPRComment,
		"create": cmdGitHubPRCreate, "edit": cmdGitHubPREdit,
	}
	if handler := handlers[action]; handler != nil {
		return handler(ctx, c, cfg, rest)
	}
	return fmt.Errorf("unknown pr action %q", action)
}

func cmdGitHubPRList(ctx context.Context, c *APIClient, cfg *Config, rest []string) error {
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
}

func cmdGitHubPRComment(ctx context.Context, c *APIClient, cfg *Config, rest []string) error {
	if len(rest) < 1 || rest[0] != "add" {
		return fmt.Errorf("usage: github pr comment add [--repo R] --body TEXT NUMBER")
	}
	if err := requireWrite(cfg); err != nil {
		return err
	}
	fs, r := repoFlagSet("github pr comment add")
	body := fs.String("body", "", "comment body")
	if err := fs.Parse(reorderIDToBack(rest[1:])); err != nil {
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
}

func cmdGitHubPRCreate(ctx context.Context, c *APIClient, cfg *Config, rest []string) error {
	if err := requireWrite(cfg); err != nil {
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
	repo, err := ghRepo(cfg, *r)
	if err != nil {
		return err
	}
	b, err := githubCreatePR(ctx, c, repo, *head, *base, *title, *body, *draft, splitCommaList(*reviewer))
	if err != nil {
		return err
	}
	return printBody(b)
}

func cmdGitHubPREdit(ctx context.Context, c *APIClient, cfg *Config, rest []string) error {
	if err := requireWrite(cfg); err != nil {
		return err
	}
	fs, r := repoFlagSet("github pr edit")
	title := fs.String("title", "", "new title")
	body := fs.String("body", "", "new description")
	base := fs.String("base", "", "new target branch")
	state := fs.String("state", "", "state: open or closed")
	addReviewers := fs.String("add-reviewer", "", "comma-separated reviewers to add")
	removeReviewers := fs.String("remove-reviewer", "", "comma-separated reviewers to remove")
	if err := fs.Parse(reorderIDToBack(rest)); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: github pr edit [--repo R] [--title T] [--body B] [--base B] [--state open|closed] [--add-reviewer USERS] [--remove-reviewer USERS] NUMBER")
	}
	n, err := strconv.Atoi(fs.Arg(0))
	if err != nil {
		return err
	}
	repo, err := ghRepo(cfg, *r)
	if err != nil {
		return err
	}
	fields := githubPREditFields(*title, *body, *base, *state)
	if *state != "" {
		if *state != "open" && *state != "closed" {
			return fmt.Errorf("invalid state %q, expected open or closed", *state)
		}
	}
	if len(fields) == 0 && *addReviewers == "" && *removeReviewers == "" {
		return fmt.Errorf("at least one edit flag is required (--title, --body, --base, --state, --add-reviewer, --remove-reviewer)")
	}
	lastResult, err := githubApplyPREdit(ctx, c, repo, n, fields, splitCommaList(*addReviewers), splitCommaList(*removeReviewers))
	if err != nil {
		return err
	}
	return printBody(lastResult)
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
		{"github_get_pull_request_reviewers", "Get requested reviewers (users and teams) of a pull request.", byNum},
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
			mcpTool{"github_create_pull_request", "Create a pull request. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"repo": repo, "head": strProp("Source branch"), "base": strProp("Target branch"), "title": strProp("Title"), "body": strProp("Description"), "draft": boolProp("Create as draft"), "reviewers": strProp("Comma-separated reviewer usernames")}, "head", "base", "title")},
			mcpTool{"github_request_reviewers", "Request reviewers for a pull request. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"repo": repo, "number": num, "reviewers": strProp("Comma-separated reviewer usernames")}, "number", "reviewers")},
			mcpTool{"github_edit_pull_request", "Edit a pull request title, body, state, base or reviewers. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"repo": repo, "number": num, "title": strProp("New title"), "body": strProp("New description"), "base": strProp("New base branch"), "state": strProp("open or closed"), "add_reviewers": strProp("Comma-separated reviewer usernames to add"), "remove_reviewers": strProp("Comma-separated reviewer usernames to remove")}, "number")},
		)
	}
	withProfileArg(tools, "github_", "GitHub profile (account); omit to use defaults.github")
	return tools
}

func callGitHubTool(ctx context.Context, cfg *Config, name string, a map[string]any) ([]byte, error) {
	cfg, err := scopedConfig(cfg, a, "github")
	if err != nil {
		return nil, err
	}
	c, err := newKindClient(cfg, "github")
	if err != nil {
		return nil, err
	}
	repo, err := ghRepo(cfg, argString(a, "repo"))
	if err != nil {
		return nil, err
	}
	n, id := argInt(a, "number", 0), argInt(a, "id", 0)
	for _, entry := range githubPRReads {
		if entry.tool == name {
			return entry.read(ctx, c, repo, n)
		}
	}
	switch name {
	case "github_get_repo":
		return githubGetRepo(ctx, c, repo)
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
		return githubCreatePR(ctx, c, repo, argString(a, "head"), argString(a, "base"), argString(a, "title"), argString(a, "body"), argBool(a, "draft"), splitCommaList(argString(a, "reviewers")))
	case "github_request_reviewers":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		return githubRequestReviewers(ctx, c, repo, n, splitCommaList(argString(a, "reviewers")))
	case "github_edit_pull_request":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		fields := githubPREditFields(argString(a, "title"), argString(a, "body"), argString(a, "base"), argString(a, "state"))
		last, err := githubApplyPREdit(ctx, c, repo, n, fields, splitCommaList(argString(a, "add_reviewers")), splitCommaList(argString(a, "remove_reviewers")))
		if err != nil {
			return nil, err
		}
		if last == nil {
			return nil, fmt.Errorf("at least one of title, body, base, state, add_reviewers or remove_reviewers must be provided")
		}
		return last, nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
