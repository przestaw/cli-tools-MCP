// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package modules embeds the full cobra CLIs (azure, atlassian) into workctl and
// polices what the agent surface may run. Each module keeps its own config file and
// OS keyring entries (under the names ado and atl), so profiles and tokens made
// earlier still work.
package modules

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	atlassiancmd "workctl/internal/atlassian/cmd"
	azurecmd "workctl/internal/azure/cmd"
	"workctl/internal/config"
	"workctl/internal/mcp"
	"workctl/internal/proc"
)

// Aliases maps the short names to the module names.
var Aliases = map[string]string{"ado": "azure", "atl": "atlassian"}

// cliModule is one of the CLIs compiled into this binary (a cobra command tree under internal/<name>).
type cliModule struct {
	name    string
	desc    string
	run     func(args []string) error
	resolve func(args []string) (path []string, group bool, rest []string, err error)
	leaves  func() [][]string
	// read holds the commands that only read, as canonical words joined by a space. Every other command is a write.
	read map[string]bool
}

func setOf(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, s := range items {
		m[s] = true
	}
	return m
}

var cliModules = map[string]*cliModule{
	"azure": {
		name: "azure", desc: "Azure DevOps: repos, pull requests, pipelines, builds, releases",
		run: azurecmd.Run, resolve: azurecmd.Resolve, leaves: azurecmd.Leaves,
		read: setOf("version", "help",
			"build definitions", "build get", "build list", "build logs",
			"pipeline get", "pipeline list", "pipeline runs",
			"pr changes", "pr diff", "pr get", "pr list", "pr reviewers", "pr threads",
			"release definitions", "release get", "release list",
			"repo branches", "repo files", "repo get", "repo list", "repo show",
			"project get", "project list", "identity search"),
	},
	"atlassian": {
		name: "atlassian", desc: "Atlassian Cloud: Jira and Confluence",
		run: atlassiancmd.Run, resolve: atlassiancmd.Resolve, leaves: atlassiancmd.Leaves,
		read: setOf("version", "help", "jql", "cql", "trace",
			"attachment list",
			"board issues", "board list", "board sprints",
			"comment get", "comment list",
			"field list",
			"filter get", "filter mine", "filter run", "filter search",
			"issue get", "issue history", "issue subtasks", "issue transitions",
			"link get", "link list", "link remote", "link types",
			"page attachments", "page children", "page comment-children", "page comments", "page descendants", "page get", "page inline-comments",
			"project fields", "project get", "project list", "project statuses", "project types",
			"sprint get", "sprint issues",
			"space get", "space list", "space pages",
			"user find", "user get", "user me",
			"worklog list"),
	},
}

// Names lists the embedded module names.
func Names() []string {
	names := make([]string, 0, len(cliModules))
	for name := range cliModules {
		names = append(names, name)
	}
	return names
}

func hasHelpFlag(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// classify says what running args on the module means. write is true when the command changes something, which
// needs policy.allow_write. strict is the agent surface (MCP): it also refuses the credential, profile and shell
// commands, `open` (it starts a browser) and the flags that point the CLI at another config file or switch TLS
// verification off. Commands are matched by the canonical command the CLI itself resolves, so aliases and flags
// before the command (`-p X pr list`) are handled. Anything not on the read list counts as a write.
func (m *cliModule) classify(args []string, strict bool) (write bool, err error) {
	if hasHelpFlag(args) {
		return false, nil // prints help and nothing else
	}
	if strict {
		for _, a := range args {
			flag, _, _ := strings.Cut(a, "=")
			if flag == "--config" || flag == "--insecure" {
				return false, fmt.Errorf("%s: the %s flag is not allowed here", m.name, flag)
			}
		}
	}
	path, group, rest, err := m.resolve(args)
	if err != nil {
		return false, fmt.Errorf("%s: %w", m.name, err)
	}
	if len(path) == 0 || group {
		return false, nil // a group of commands only prints its help
	}
	key := strings.Join(path, " ")
	switch path[0] {
	case "config", "setup", "completion", "shell":
		if strict {
			return false, fmt.Errorf("%s %s is not available here: credentials, profiles and the interactive shell stay with their owner", m.name, key)
		}
		return path[0] == "shell", nil // the shell can run any command, so it counts as a write
	}
	if path[len(path)-1] == "open" {
		if strict {
			return false, fmt.Errorf("%s %s starts a browser and is not available here", m.name, key)
		}
		return false, nil
	}
	if key == "raw" {
		return rawIsWrite(m.name, args, rest, strict)
	}
	return !m.read[key], nil
}

// rawIsWrite classifies the `raw` command (an arbitrary HTTP request): GET and HEAD read, everything else writes.
// On the strict surface the path must come first and start with "/", so a full URL can never carry the credential
// to another host.
func rawIsWrite(name string, args, rest []string, strict bool) (bool, error) {
	if strict && (len(rest) == 0 || !strings.HasPrefix(rest[0], "/")) {
		return false, fmt.Errorf("%s raw: give the path first, starting with / (full URLs are not allowed here)", name)
	}
	method, data := "GET", false
	for i, a := range args {
		switch {
		case a == "-X" || a == "--method":
			if i+1 < len(args) {
				method = args[i+1]
			}
		case strings.HasPrefix(a, "--method="):
			method = strings.TrimPrefix(a, "--method=")
		case strings.HasPrefix(a, "-X"):
			method = strings.TrimPrefix(strings.TrimPrefix(a, "-X"), "=")
		case a == "--data" || strings.HasPrefix(a, "--data="), strings.HasPrefix(a, "-d") && !strings.HasPrefix(a, "--"):
			data = true
		}
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	return data || (method != "GET" && method != "HEAD"), nil
}

// Command runs an embedded CLI in this process: `workctl azure ...`, `workctl atlassian ...`.
func Command(cfg *config.Config, name string, args []string) error {
	m := cliModules[name]
	if m == nil {
		return fmt.Errorf("unknown module %q", name)
	}
	if len(args) > 0 && (args[0] == "init" || args[0] == "set-token") {
		args = append([]string{"config"}, args...) // same words as jira and gitlab
	}
	write, err := m.classify(args, false)
	if err != nil {
		return err
	}
	if write {
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
	}
	return m.run(args)
}

// RunDirect runs a module CLI in-process without the policy check; the init wizard uses it for `config init`.
func RunDirect(name string, args []string) error {
	m := cliModules[name]
	if m == nil {
		return fmt.Errorf("unknown module %q", name)
	}
	return m.run(args)
}

func moduleTimeout(cfg *config.Config) time.Duration {
	if s := cfg.Modules.TimeoutSeconds; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 120 * time.Second
}

// RunProcess runs `workctl [--config F] <name> args... --no-color` as a child process and returns its stdout.
// A child keeps the command trees' global flag state away from the long-running MCP server, gives each call its own
// timeout, and has no terminal to prompt on. It does not check the policy: callers do.
func RunProcess(ctx context.Context, cfg *config.Config, name string, args []string) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	var argv []string
	if cfg.Path() != "" {
		argv = append(argv, "--config", cfg.Path())
	}
	argv = append(argv, name)
	argv = append(argv, args...)
	argv = append(argv, "--no-color")
	timeout := moduleTimeout(cfg)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := proc.Command(ctx, exe, argv...)
	cmd.Env = append(cmd.Environ(), "NO_COLOR=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("%s timed out after %s", name, timeout)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > 2000 {
			msg = msg[:2000] + "..."
		}
		return nil, fmt.Errorf("%s: %s", name, msg)
	}
	return stdout.Bytes(), nil
}

// CallTool is the MCP entry for the embedded CLIs: strict policy first, then a child process.
func CallTool(ctx context.Context, cfg *config.Config, name string, args []string) ([]byte, error) {
	m := cliModules[name]
	if m == nil {
		return nil, fmt.Errorf("unknown module %q", name)
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("%s: args is empty, for example [\"--help\"]", name)
	}
	write, err := m.classify(args, true)
	if err != nil {
		return nil, err
	}
	if write {
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	return RunProcess(ctx, cfg, name, args)
}

// Tools are the MCP tools of the embedded CLIs: one per module, taking the command line as an array.
func Tools(cfg *config.Config) []mcp.Tool {
	mode := "Commands that change something are refused until policy.allow_write=true."
	if cfg.Policy.AllowWrite {
		mode = "Commands that change something are allowed (policy.allow_write=true)."
	}
	argsSchema := func(what string) map[string]any {
		return mcp.ObjSchema(map[string]any{"args": mcp.ArrProp("Arguments of the " + what + " command, one array element per argument")}, "args")
	}
	return []mcp.Tool{
		{Name: "azure", Description: "Azure DevOps CLI (embedded in workctl). Put the command first and flags after it, for example [\"pr\",\"get\",\"123\",\"-p\",\"MyProject\",\"-e\"] or [\"pr\",\"threads\",\"123\",\"-p\",\"MyProject\",\"--json\"]. There is no default project: pass -p. Use [\"pr\",\"--help\"] to explore. " + mode, InputSchema: argsSchema("azure")},
		{Name: "atlassian", Description: "Atlassian Cloud CLI for Jira and Confluence (embedded in workctl). Put the command first and flags after it, for example [\"issue\",\"get\",\"ABC-123\",\"-e\"] or [\"jql\",\"project = ABC AND assignee = currentUser()\"]. Use [\"issue\",\"--help\"] to explore. " + mode, InputSchema: argsSchema("atlassian")},
	}
}
