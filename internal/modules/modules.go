// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package modules embeds the full cobra CLIs (azure, atlassian) into workctl and
// polices what the agent surface may run. Each module keeps its own config file and
// OS keyring entries (under the names ado and atl), so profiles and tokens made
// earlier still work. The agent reaches them, and the `workctl jira` commands, through
// one `cli` tool.
package modules

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	atlassiancmd "workctl/internal/atlassian/cmd"
	azurecmd "workctl/internal/azure/cmd"
	"workctl/internal/config"
	"workctl/internal/generic"
	"workctl/internal/jira"
	"workctl/internal/mcp"
	"workctl/internal/proc"
)

// Aliases maps the short names to the module names.
var Aliases = map[string]string{"ado": "azure", "atl": "atlassian"}

// cliModule is a CLI the agent may run through the cli tool: a cobra command tree compiled into this binary
// (internal/<name>, with run set), or a native workctl command over workctl profiles (native set, run nil).
type cliModule struct {
	name    string
	desc    string
	run     func(args []string) error
	resolve func(args []string) (path []string, group bool, rest []string, err error)
	leaves  func() [][]string
	// read holds the commands that only read, as canonical words joined by a space. Every other command is a write.
	read map[string]bool
	// raw classifies the module's raw command; nil means the cobra form (rawIsWrite).
	raw func(rest []string, strict bool) (bool, error)
	// native: a `workctl <name>` command using workctl profiles; listed only when a profile of that kind exists,
	// accepts the profile argument and takes no --no-color.
	native bool
	// downloads are commands that only fetch a file. On the agent surface they count as reads, run in the workctl
	// downloads directory and refuse an output path of their own.
	downloads map[string]bool
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
		downloads: setOf("attachment download", "page attachment-download"),
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
	"jira": {
		name: "jira", desc: "Jira Server / Data Center (workctl jira)", native: true,
		resolve: jira.Resolve, leaves: jira.Leaves, read: setOf(jira.ReadCommands...), raw: jira.RawIsWrite,
	},
}

// Names lists the embedded module names (the cobra trees; native commands have their own handlers).
func Names() []string {
	names := make([]string, 0, len(cliModules))
	for name, m := range cliModules {
		if m.run != nil {
			names = append(names, name)
		}
	}
	return names
}

// toolModules lists the modules the cli tool offers with this configuration, sorted.
func toolModules(cfg *config.Config) []string {
	var names []string
	for name, m := range cliModules {
		if !m.native || cfg.Has(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

func hasOutputFlag(args []string) bool {
	for _, a := range args {
		flag, _, _ := strings.Cut(a, "=")
		if flag == "-o" || flag == "--output" || strings.HasPrefix(a, "-o") && len(a) > 2 && !strings.HasPrefix(a, "--") {
			return true
		}
	}
	return false
}

// downloadDir is the working directory of a download command on the agent surface; empty for other commands.
func (m *cliModule) downloadDir(args []string) (string, error) {
	path, group, _, err := m.resolve(args)
	if err != nil || group || !m.downloads[strings.Join(path, " ")] {
		return "", nil
	}
	return generic.DownloadsDir()
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
	if m.downloads[key] {
		if strict && hasOutputFlag(args) {
			return false, fmt.Errorf("%s %s: the output path is chosen by workctl (the downloads directory); drop -o/--output", m.name, key)
		}
		return !strict, nil
	}
	if key == "raw" {
		if m.raw != nil {
			return m.raw(rest, strict)
		}
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
	if m == nil || m.run == nil {
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
	if m == nil || m.run == nil {
		return fmt.Errorf("unknown module %q", name)
	}
	return m.run(args)
}

// moduleTimeout is timeouts.<module>, else modules.timeout_seconds, else 120 s.
func moduleTimeout(cfg *config.Config, name string) time.Duration {
	if s := cfg.Timeouts[name]; s > 0 {
		return time.Duration(s) * time.Second
	}
	if s := cfg.Modules.TimeoutSeconds; s > 0 {
		return time.Duration(s) * time.Second
	}
	return 120 * time.Second
}

// RunProcess runs `workctl [--config F] <name> args... --no-color` as a child process and returns its stdout.
// A child keeps the command trees' global flag state away from the long-running MCP server, gives each call its own
// timeout, and has no terminal to prompt on. It does not check the policy: callers do.
func RunProcess(ctx context.Context, cfg *config.Config, name string, args []string) ([]byte, error) {
	return runProcess(ctx, cfg, name, "", args)
}

func runProcess(ctx context.Context, cfg *config.Config, name, profile string, args []string) ([]byte, error) {
	return runProcessIn(ctx, cfg, name, profile, "", args)
}

func runProcessIn(ctx context.Context, cfg *config.Config, name, profile, dir string, args []string) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	var argv []string
	if cfg.Path() != "" {
		argv = append(argv, "--config", cfg.Path())
	}
	if profile != "" {
		argv = append(argv, "--profile", profile)
	}
	argv = append(argv, name)
	argv = append(argv, args...)
	if m := cliModules[name]; m == nil || !m.native {
		argv = append(argv, "--no-color")
	}
	timeout := moduleTimeout(cfg, name)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := proc.Command(ctx, exe, argv...)
	cmd.Env = append(cmd.Environ(), "NO_COLOR=1")
	cmd.Dir = dir
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

// CallCLITool runs the cli tool: {module, args, profile?}.
func CallCLITool(ctx context.Context, cfg *config.Config, a map[string]any) ([]byte, error) {
	args, err := mcp.ArgStrings(a, "args")
	if err != nil {
		return nil, err
	}
	name := mcp.ArgString(a, "module")
	if alias := Aliases[name]; alias != "" {
		name = alias
	}
	return callModule(ctx, cfg, name, mcp.ArgString(a, "profile"), args)
}

func callModule(ctx context.Context, cfg *config.Config, name, profile string, args []string) ([]byte, error) {
	m := cliModules[name]
	if m == nil {
		return nil, fmt.Errorf("unknown module %q; available: %s", name, strings.Join(toolModules(cfg), ", "))
	}
	if m.native && !cfg.Has(name) {
		return nil, fmt.Errorf("module %q is not available: no %s profile is configured", name, name)
	}
	if profile != "" {
		if !m.native {
			return nil, fmt.Errorf("%s keeps its own profiles: pass its own flag in args instead of profile", name)
		}
		if p, ok := cfg.Profiles[profile]; !ok || p.Kind != name {
			return nil, fmt.Errorf("profile %q is not a configured %s profile", profile, name)
		}
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
	dir, err := m.downloadDir(args)
	if err != nil {
		return nil, err
	}
	out, err := runProcessIn(ctx, cfg, name, profile, dir, args)
	if err == nil && dir != "" {
		out = append(out, []byte("\n(saved in "+dir+")\n")...)
	}
	return out, err
}

// Tools is the one MCP tool of the module CLIs: the command line as an array, the module as an argument.
func Tools(cfg *config.Config) []mcp.Tool {
	mode := "Commands that change something are refused until policy.allow_write=true."
	if cfg.Policy.AllowWrite {
		mode = "Commands that change something are allowed (policy.allow_write=true)."
	}
	names := toolModules(cfg)
	desc := "Run a provider CLI embedded in workctl; put the command first and flags after it. Modules: " +
		strings.Join(names, ", ") + ". " +
		"azure (Azure DevOps): [\"pr\",\"get\",\"123\",\"-p\",\"MyProject\",\"-e\"], no default project, pass -p. " +
		"atlassian (Jira/Confluence Cloud): [\"issue\",\"get\",\"ABC-123\",\"-e\"] or [\"jql\",\"project = ABC\"]. "
	if cfg.Has("jira") {
		desc += "jira (Jira Server): [\"transitions\",\"KEY\"], [\"transition\",\"--id\",\"5\",\"KEY\"], " +
			"[\"raw\",\"--body\",\"{...}\",\"PUT\",\"/rest/api/2/issue/KEY\"]. "
	}
	desc += "Use [\"--help\"] or [\"<command>\",\"--help\"] to explore. " + mode
	module := mcp.StrProp("Module: " + strings.Join(names, ", "))
	module["enum"] = names
	return []mcp.Tool{{Name: "cli", Description: desc, InputSchema: mcp.ObjSchema(map[string]any{
		"module":  module,
		"args":    mcp.ArrProp("Arguments of the module command, one array element per argument"),
		"profile": mcp.StrProp("workctl profile for native modules (jira); omit to use defaults.<module>"),
	}, "module", "args")}}
}

// IsWrite reports whether a cli tool call changes something. A call that will be refused anyway counts as a read:
// the tool reports the refusal itself.
func IsWrite(a map[string]any) bool {
	args, err := mcp.ArgStrings(a, "args")
	name := mcp.ArgString(a, "module")
	if alias := Aliases[name]; alias != "" {
		name = alias
	}
	m := cliModules[name]
	if err != nil || m == nil || len(args) == 0 {
		return false
	}
	write, err := m.classify(args, true)
	return err == nil && write
}
