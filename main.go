package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	azureclient "workctl/internal/azure/client"
)

const version = "0.3.9"

func usage() {
	fmt.Fprintf(os.Stdout, `workctl %s - one CLI/MCP binary for Azure DevOps, Jira, Confluence, GitLab and GitHub

Usage:
  workctl [--config FILE] init [module...]   guided setup (alias: setup); tokens go to the OS keyring
  workctl [--config FILE] auth set-token|status|logout   profile credentials in OS keyring
  workctl [--config FILE] doctor
  workctl [--config FILE] config show|path
  workctl [--config FILE] azure ...      Azure DevOps (alias: ado)
  workctl [--config FILE] atlassian ...  Jira and Confluence Cloud (alias: atl)
  workctl [--config FILE] jira ...     Jira
  workctl [--config FILE] gitlab ...   GitLab
  workctl [--config FILE] github ...   GitHub
  workctl [--config FILE] pr|mr ...    provider-neutral pull/merge requests
  workctl [--config FILE] task ISSUE-KEY
  workctl [--config FILE] mcp
  workctl --profile NAME git-credential get   git credential helper (token from the keyring)

Global:
  --config FILE     config path (or WORKCTL_CONFIG)
  --profile NAME    use this profile instead of defaults.<kind> for its kind

Run workctl help or see README.md for command examples.
`, version)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	global := flag.NewFlagSet("workctl", flag.ContinueOnError)
	global.SetOutput(os.Stderr)
	configPath := global.String("config", "", "config file")
	profile := global.String("profile", "", "profile to use instead of the default of its kind")
	if err := global.Parse(args); err != nil {
		return err
	}
	rest := global.Args()
	if len(rest) == 0 {
		usage()
		return nil
	}
	cmd := moduleAliases[rest[0]]
	if cmd == "" {
		cmd = rest[0]
	}
	rest = rest[1:]
	switch cmd {
	case "help", "-h", "--help":
		usage()
		return nil
	case "version":
		fmt.Println(version)
		return nil
	case "init", "setup":
		return cmdInit(*configPath, rest)
	case "auth":
		return cmdAuth(*configPath, *profile, rest)
	case "jira", "gitlab", "github":
		if len(rest) > 0 && rest[0] == "init" {
			return cmdEndpointInit(*configPath, cmd, rest[1:], nil)
		}
	case "config":
		return cmdConfig(*configPath, rest)
	}
	cfg, err := loadConfigOrDefault(*configPath)
	if err != nil {
		return err
	}
	if *profile != "" {
		if _, ok := cfg.Profiles[*profile]; !ok {
			return fmt.Errorf("profile %q is not configured", *profile)
		}
		cfg.profile = *profile
	}
	return dispatchCommand(cfg, cmd, rest)
}

func commandHandlers() map[string]func(*Config, []string) error {
	handlers := map[string]func(*Config, []string) error{
		"doctor": func(cfg *Config, _ []string) error { return cmdDoctor(cfg) },
		"jira":   cmdJira, "gitlab": cmdGitLab, "github": cmdGitHub,
		"pr": cmdPullRequest, "mr": cmdPullRequest, "task": cmdTask,
		"mcp":            func(cfg *Config, _ []string) error { return runMCP(context.Background(), cfg) },
		"git-credential": func(cfg *Config, args []string) error { return cmdGitCredential(cfg, args, os.Stdin, os.Stdout) },
	}
	for name := range cliModules {
		handlers[name] = func(cfg *Config, args []string) error { return cmdModule(cfg, name, args) }
	}
	for name := range plannedModules {
		handlers[name] = func(_ *Config, _ []string) error { return cmdPlanned(name) }
	}
	return handlers
}

func dispatchCommand(cfg *Config, name string, args []string) error {
	if handler := commandHandlers()[name]; handler != nil {
		return handler(cfg, args)
	}
	return fmt.Errorf("unknown command %q", name)
}

func cmdTask(cfg *Config, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: workctl task ISSUE-KEY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds*3)*time.Second)
	defer cancel()
	body, err := getTaskContext(ctx, cfg, args[0])
	if err != nil {
		return err
	}
	return printBody(body)
}

func cmdConfig(path string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: workctl config show|path")
	}
	switch args[0] {
	case "path":
		if path == "" {
			p, err := defaultConfigPath()
			if err != nil {
				return err
			}
			path = p
		}
		fmt.Println(path)
		return nil
	case "show":
		cfg, err := loadConfig(path)
		if err != nil {
			return err
		}
		cp := *cfg
		cp.path = ""
		b, _ := json.MarshalIndent(cp, "", "  ")
		return printBody(b)
	default:
		return fmt.Errorf("usage: workctl config show|path")
	}
}

type doctorCheck struct {
	OK     bool   `json:"ok"`
	Detail any    `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func cmdDoctor(cfg *Config) error {
	result := map[string]doctorCheck{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	allOK := true

	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if cfg.Profiles[name].Kind == "atlassian" {
			continue // Atlassian Cloud is verified via the embedded module below
		}
		detail, err := doctorCheckProfile(ctx, cfg, name, cfg.Profiles[name])
		if err != nil {
			result[name] = doctorCheck{Error: err.Error()}
			allOK = false
		} else {
			result[name] = doctorCheck{OK: true, Detail: detail}
		}
	}

	// The embedded CLIs report their own connectivity; informational, does not change the exit code.
	for _, name := range []string{"azure", "atlassian"} {
		out, err := runModuleProcess(ctx, cfg, name, []string{"config", "test"})
		key := name + " (module)"
		if err != nil {
			result[key] = doctorCheck{Error: err.Error()}
		} else {
			result[key] = doctorCheck{OK: true, Detail: strings.TrimSpace(string(out))}
		}
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	_ = printBody(b)
	if !allOK {
		return fmt.Errorf("one or more connectivity checks failed")
	}
	return nil
}

// doctorCheckProfile makes one lightweight authenticated call and returns a compact identity detail.
func doctorCheckProfile(ctx context.Context, cfg *Config, name string, profile Profile) (any, error) {
	if profile.Kind == "azure" {
		cred, err := resolveProfileSecret(name, profile)
		if err != nil {
			return nil, err
		}
		c := azureclient.New(azureclient.Options{OrgURL: profile.URL, Token: cred, Timeout: profileTimeout(cfg, profile), VerifyTLS: profile.VerifyTLS})
		cd, err := c.ConnectionData(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "azure", "user": cd.AuthenticatedUser.ProviderDisplayName}, nil
	}
	c, err := newProfileClient(cfg, name, profile)
	if err != nil {
		return nil, err
	}
	switch profile.Kind {
	case "github":
		u, err := githubUser(ctx, c)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "github", "login": jsonField(u, "login")}, nil
	case "gitlab":
		ver, err := gitlabVersion(ctx, c)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "gitlab", "version": jsonField(ver, "version")}, nil
	case "jira":
		me, err := jiraMyself(ctx, c)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "jira", "user": firstNonEmpty(jsonField(me, "name"), jsonField(me, "displayName"))}, nil
	default:
		return nil, fmt.Errorf("unsupported profile kind %q", profile.Kind)
	}
}

// jsonField returns one top-level string field of a JSON object, or "".
func jsonField(b []byte, key string) string {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

func cmdJira(cfg *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("jira commands: issue, search, comments, transitions, transition, comment, myself, server-info, raw")
	}
	c, err := newKindClient(cfg, "jira")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	switch args[0] {
	case "issue":
		fs := flag.NewFlagSet("jira issue", flag.ContinueOnError)
		fields := fs.String("fields", "", "comma-separated fields")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: jira issue [--fields a,b] KEY")
		}
		b, err := jiraGetIssue(ctx, c, fs.Arg(0), parseCommaList(*fields))
		if err != nil {
			return err
		}
		return printBody(b)
	case "search":
		fs := flag.NewFlagSet("jira search", flag.ContinueOnError)
		jql := fs.String("jql", "", "JQL")
		start := fs.Int("start", 0, "startAt")
		max := fs.Int("max", 50, "maxResults")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *jql == "" {
			return fmt.Errorf("--jql is required")
		}
		b, err := jiraSearch(ctx, c, *jql, *start, *max)
		if err != nil {
			return err
		}
		return printBody(b)
	case "comments":
		if len(args) != 2 {
			return fmt.Errorf("usage: jira comments KEY")
		}
		b, err := jiraComments(ctx, c, args[1])
		if err != nil {
			return err
		}
		return printBody(b)
	case "transitions":
		if len(args) != 2 {
			return fmt.Errorf("usage: jira transitions KEY")
		}
		b, err := jiraTransitions(ctx, c, args[1])
		if err != nil {
			return err
		}
		return printBody(b)
	case "transition":
		if err := requireWrite(cfg); err != nil {
			return err
		}
		fs := flag.NewFlagSet("jira transition", flag.ContinueOnError)
		id := fs.String("id", "", "transition id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *id == "" {
			return fmt.Errorf("usage: jira transition --id ID KEY")
		}
		b, err := jiraDoTransition(ctx, c, fs.Arg(0), *id)
		if err != nil {
			return err
		}
		return printBody(b)
	case "comment":
		if len(args) < 2 || args[1] != "add" {
			return fmt.Errorf("usage: jira comment add --body TEXT KEY")
		}
		if err := requireWrite(cfg); err != nil {
			return err
		}
		fs := flag.NewFlagSet("jira comment add", flag.ContinueOnError)
		body := fs.String("body", "", "comment body")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *body == "" {
			return fmt.Errorf("usage: jira comment add --body TEXT KEY")
		}
		b, err := jiraAddComment(ctx, c, fs.Arg(0), *body)
		if err != nil {
			return err
		}
		return printBody(b)
	case "myself":
		b, err := jiraMyself(ctx, c)
		if err != nil {
			return err
		}
		return printBody(b)
	case "server-info":
		b, err := jiraServerInfo(ctx, c)
		if err != nil {
			return err
		}
		return printBody(b)
	case "raw":
		return cmdRaw(ctx, c, cfg, args[1:])
	default:
		return fmt.Errorf("unknown jira command %q", args[0])
	}
}

func projectFlagSet(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	p := fs.String("project", "", "group/project (default from config)")
	return fs, p
}

func cmdGitLab(cfg *Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("gitlab commands: project, mr, pipelines, pipeline, job, branches, file, raw")
	}
	c, err := newKindClient(cfg, "gitlab")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	switch args[0] {
	case "project":
		fs, p := projectFlagSet("gitlab project")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabGetProject(ctx, c, project)
		if e != nil {
			return e
		}
		return printBody(b)
	case "mr":
		return cmdGitLabMR(ctx, c, cfg, args[1:])
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
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabListPipelines(ctx, c, project, *ref, *per)
		if e != nil {
			return e
		}
		return printBody(b)
	case "pipeline":
		if len(args) < 2 {
			return fmt.Errorf("usage: gitlab pipeline get|jobs ...")
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
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		var b []byte
		if action == "get" {
			b, e = gitlabGetPipeline(ctx, c, project, id)
		} else if action == "jobs" {
			b, e = gitlabListPipelineJobs(ctx, c, project, id)
		} else {
			return fmt.Errorf("unknown pipeline action %q", action)
		}
		if e != nil {
			return e
		}
		return printBody(b)
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
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabJobTrace(ctx, c, project, id)
		if e != nil {
			return e
		}
		return printBody(b)
	case "branches":
		fs, p := projectFlagSet("gitlab branches")
		search := fs.String("search", "", "search")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabListBranches(ctx, c, project, *search)
		if e != nil {
			return e
		}
		return printBody(b)
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
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabGetFile(ctx, c, project, fs.Arg(0), *ref)
		if e != nil {
			return e
		}
		return printBody(b)
	case "raw":
		return cmdRaw(ctx, c, cfg, args[1:])
	default:
		return fmt.Errorf("unknown gitlab command %q", args[0])
	}
}

func cmdGitLabMR(ctx context.Context, c *APIClient, cfg *Config, args []string) error {
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
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		var b []byte
		if action == "get" {
			b, e = gitlabGetMR(ctx, c, project, iid)
		} else if action == "changes" {
			b, e = gitlabGetMRChanges(ctx, c, project, iid)
		} else {
			b, e = gitlabGetMRNotes(ctx, c, project, iid)
		}
		if e != nil {
			return e
		}
		return printBody(b)
	case "list":
		fs, p := projectFlagSet("gitlab mr list")
		search := fs.String("search", "", "search")
		state := fs.String("state", "all", "state")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabListMRs(ctx, c, project, *search, *state)
		if e != nil {
			return e
		}
		return printBody(b)
	case "note":
		if len(args) < 2 || args[1] != "add" {
			return fmt.Errorf("usage: gitlab mr note add [--project P] --body TEXT IID")
		}
		if err := requireWrite(cfg); err != nil {
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
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabAddMRNote(ctx, c, project, iid, *body)
		if e != nil {
			return e
		}
		return printBody(b)
	case "create":
		if err := requireWrite(cfg); err != nil {
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
		project, e := glProject(cfg, *p)
		if e != nil {
			return e
		}
		b, e := gitlabCreateMR(ctx, c, project, *source, *target, *title, *desc, *draft)
		if e != nil {
			return e
		}
		return printBody(b)
	default:
		return fmt.Errorf("unknown mr action %q", action)
	}
}

func cmdRaw(ctx context.Context, c *APIClient, cfg *Config, args []string) error {
	fs := flag.NewFlagSet("raw", flag.ContinueOnError)
	bodyArg := fs.String("body", "", "JSON body or @file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: raw [--body JSON|@file] METHOD PATH")
	}
	method := strings.ToUpper(fs.Arg(0))
	if method != "GET" && method != "HEAD" {
		if err := requireWrite(cfg); err != nil {
			return err
		}
	}
	body, err := readBodyArg(*bodyArg)
	if err != nil {
		return err
	}
	b, _, err := c.do(ctx, method, fs.Arg(1), nil, body, "application/json")
	if err != nil {
		return err
	}
	return printBody(b)
}

// Silence otherwise unused imports when command code is compiled on old Go versions.
var _ = http.MethodGet
var _ = url.Values{}
