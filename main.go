package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const version = "0.3.4"

func usage() {
	fmt.Fprintf(os.Stderr, `workctl %s - one CLI/MCP binary for Azure DevOps, Jira, Confluence and GitLab

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
		return cmdAuth(*configPath, rest)
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
	switch cmd {
	case "doctor":
		return cmdDoctor(cfg)
	case "azure", "atlassian":
		return cmdModule(cfg, cmd, rest)
	case "jira":
		return cmdJira(cfg, rest)
	case "gitlab":
		return cmdGitLab(cfg, rest)
	case "github":
		return cmdGitHub(cfg, rest)
	case "pr", "mr":
		return cmdPullRequest(cfg, rest)
	case "bitbucket", "asana":
		return cmdPlanned(cmd)
	case "task":
		if len(rest) != 1 {
			return fmt.Errorf("usage: workctl task ISSUE-KEY")
		}
		jc, err := newKindClient(cfg, "jira")
		if err != nil {
			return err
		}
		gc, err := newKindClient(cfg, "gitlab")
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds*3)*time.Second)
		defer cancel()
		b, err := getTaskContext(ctx, cfg, jc, gc, rest[0])
		if err != nil {
			return err
		}
		return printBody(b)
	case "mcp":
		return runMCP(context.Background(), cfg)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
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

func cmdDoctor(cfg *Config) error {
	type check struct {
		OK     bool   `json:"ok"`
		Detail any    `json:"detail,omitempty"`
		Error  string `json:"error,omitempty"`
	}
	result := map[string]check{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	if !cfg.has("jira") {
		result["jira"] = check{OK: true, Detail: "not configured; skipped"}
	} else if jc, err := newKindClient(cfg, "jira"); err != nil {
		result["jira"] = check{Error: err.Error()}
	} else {
		info, e1 := jiraServerInfo(ctx, jc)
		me, e2 := jiraMyself(ctx, jc)
		if e1 != nil {
			result["jira"] = check{Error: e1.Error()}
		} else if e2 != nil {
			result["jira"] = check{Error: e2.Error()}
		} else {
			result["jira"] = check{OK: true, Detail: map[string]json.RawMessage{"server": info, "user": me}}
		}
	}
	if !cfg.has("gitlab") {
		result["gitlab"] = check{OK: true, Detail: "not configured; skipped"}
	} else if gc, err := newKindClient(cfg, "gitlab"); err != nil {
		result["gitlab"] = check{Error: err.Error()}
	} else {
		ver, e := gitlabVersion(ctx, gc)
		if e != nil {
			result["gitlab"] = check{Error: e.Error()}
		} else {
			result["gitlab"] = check{OK: true, Detail: json.RawMessage(ver)}
		}
	}
	if !cfg.has("github") {
		result["github"] = check{OK: true, Detail: "not configured; skipped"}
	} else if hc, err := newKindClient(cfg, "github"); err != nil {
		result["github"] = check{Error: err.Error()}
	} else if u, e := githubUser(ctx, hc); e != nil {
		result["github"] = check{Error: e.Error()}
	} else {
		result["github"] = check{OK: true, Detail: json.RawMessage(u)}
	}
	// The embedded CLIs report their own connectivity; this is informational and does not change the exit code.
	for _, name := range []string{"azure", "atlassian"} {
		out, err := runModuleProcess(ctx, cfg, name, []string{"config", "test"})
		if err != nil {
			result[name] = check{Error: err.Error()}
		} else {
			result[name] = check{OK: true, Detail: strings.TrimSpace(string(out))}
		}
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	_ = printBody(b)
	if !result["jira"].OK || !result["gitlab"].OK || !result["github"].OK {
		return fmt.Errorf("one or more connectivity checks failed")
	}
	return nil
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
