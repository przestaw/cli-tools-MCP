// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package app wires workctl together: the command-line dispatch, the doctor and
// task commands that span several providers, and the composition of all MCP tools
// into one server. It is the only package that knows every provider.
package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"workctl/internal/auth"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/generic"
	"workctl/internal/github"
	"workctl/internal/gitlab"
	"workctl/internal/jira"
	"workctl/internal/modules"
	"workctl/internal/pr"
	"workctl/internal/setup"
)

// Version is the one version string of the binary, the MCP server and the plugin package.
const Version = "0.4.1"

func usage() {
	fmt.Fprintf(os.Stdout, `workctl %s - one CLI/MCP binary for Azure DevOps, Jira, Confluence, GitLab and GitHub

Usage:
  workctl [--config FILE] init [module...]   guided setup (alias: setup); tokens go to the OS keyring
  workctl [--config FILE] auth login|set-token|status|logout|setup-git   profile credentials; setup-git points git at workctl
  workctl [--config FILE] doctor
  workctl [--config FILE] config show|path
  workctl [--config FILE] azure ...      Azure DevOps (alias: ado)
  workctl [--config FILE] atlassian ...  Jira and Confluence Cloud (alias: atl)
  workctl [--config FILE] jira ...     Jira
  workctl [--config FILE] gitlab ...   GitLab
  workctl [--config FILE] github ...   GitHub
  workctl [--config FILE] api raw METHOD PATH   any HTTP service of a kind "api" profile
  workctl [--config FILE] pr|mr ...    provider-neutral pull/merge requests
  workctl [--config FILE] task ISSUE-KEY
  workctl [--config FILE] mcp
  workctl [--profile NAME] git-credential get   git credential helper (profile from --profile, else from host and repository path)

Global:
  --config FILE     config path (or WORKCTL_CONFIG)
  --profile NAME    use this profile instead of defaults.<kind> for its kind

Run workctl help or see README.md for command examples.
`, Version)
}

// Main is the whole program; it returns the process exit code.
func Main(args []string) int {
	if err := run(args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
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
	cmd := modules.Aliases[rest[0]]
	if cmd == "" {
		cmd = rest[0]
	}
	rest = rest[1:]
	switch cmd {
	case "help", "-h", "--help":
		usage()
		return nil
	case "version":
		fmt.Println(Version)
		return nil
	case "init", "setup":
		return setup.Init(*configPath, rest)
	case "auth":
		return auth.Command(*configPath, *profile, rest)
	case "jira", "gitlab", "github":
		if len(rest) > 0 && rest[0] == "init" {
			return setup.EndpointInit(*configPath, cmd, rest[1:], nil)
		}
	case "config":
		return configCommand(*configPath, rest)
	}
	cfg, err := config.LoadOrDefault(*configPath)
	if err != nil {
		return err
	}
	if *profile != "" {
		if _, ok := cfg.Profiles[*profile]; !ok {
			return fmt.Errorf("profile %q is not configured", *profile)
		}
		cfg.SetProfileName(*profile)
	}
	return dispatchCommand(cfg, cmd, rest)
}

func commandHandlers() map[string]func(*config.Config, []string) error {
	handlers := map[string]func(*config.Config, []string) error{
		"doctor": func(cfg *config.Config, _ []string) error { return doctorCommand(cfg) },
		"jira":   jira.Command, "gitlab": gitlab.Command, "github": github.Command, "api": generic.Command,
		"pr": pr.Command, "mr": pr.Command, "task": taskCommand,
		"mcp": func(cfg *config.Config, _ []string) error { return runMCP(context.Background(), cfg) },
		"git-credential": func(cfg *config.Config, args []string) error {
			return auth.GitCredential(cfg, args, os.Stdin, os.Stdout)
		},
	}
	for _, name := range modules.Names() {
		handlers[name] = func(cfg *config.Config, args []string) error { return modules.Command(cfg, name, args) }
	}
	for name := range plannedModules {
		handlers[name] = func(_ *config.Config, _ []string) error { return plannedCommand(name) }
	}
	return handlers
}

func dispatchCommand(cfg *config.Config, name string, args []string) error {
	if handler := commandHandlers()[name]; handler != nil {
		return handler(cfg, args)
	}
	return fmt.Errorf("unknown command %q", name)
}

func taskCommand(cfg *config.Config, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: workctl task ISSUE-KEY")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds*3)*time.Second)
	defer cancel()
	body, err := taskContextJSON(ctx, cfg, args[0])
	if err != nil {
		return err
	}
	return cliutil.PrintBody(body)
}

func configCommand(path string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: workctl config show|path")
	}
	switch args[0] {
	case "path":
		if path == "" {
			p, err := config.DefaultPath()
			if err != nil {
				return err
			}
			path = p
		}
		fmt.Println(path)
		return nil
	case "show":
		cfg, err := config.Load(path)
		if err != nil {
			return err
		}
		b, _ := json.MarshalIndent(cfg, "", "  ")
		return cliutil.PrintBody(b)
	default:
		return fmt.Errorf("usage: workctl config show|path")
	}
}

// plannedModules are names reserved for modules that do not exist yet. `workctl <name>` says so instead of
// "unknown command", which keeps the command line stable. To implement one, add a package under internal/,
// dispatch to it in commandHandlers, and add its MCP tools.
var plannedModules = map[string]string{
	"bitbucket": "Bitbucket: repositories and pull requests",
	"asana":     "Asana: tasks and projects",
}

func plannedCommand(name string) error {
	return fmt.Errorf("the %s module is not implemented yet (reserved for: %s)", name, plannedModules[name])
}
