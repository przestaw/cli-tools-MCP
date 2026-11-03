// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package repl

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/c-bata/go-prompt"
)

// topCommands are the REPL verbs offered by autocomplete.
var topCommands = []prompt.Suggest{
	{Text: "ls", Description: "list current context (projects / repos / branches)"},
	{Text: "cd", Description: "navigate: cd <project|repo> | cd .. | cd /"},
	{Text: "use", Description: "use project <name>"},
	{Text: "pwd", Description: "print current context path"},
	{Text: "repos", Description: "list repositories in the project"},
	{Text: "prs", Description: "list pull requests (project or current repo) [status]"},
	{Text: "pr", Description: "show a pull request: pr <id>"},
	{Text: "vote", Description: "vote on a PR: vote <id> <approve|reject|...>"},
	{Text: "comment", Description: "comment on a PR: comment <id> <text>"},
	{Text: "pipelines", Description: "list pipelines"},
	{Text: "runs", Description: "list pipeline runs: runs <pipelineId>"},
	{Text: "builds", Description: "list recent builds"},
	{Text: "build", Description: "show a build: build <id>"},
	{Text: "releases", Description: "list releases"},
	{Text: "release", Description: "show a release: release <id>"},
	{Text: "branches", Description: "list branches [repo]"},
	{Text: "files", Description: "list files [repo] [path]"},
	{Text: "get", Description: "show current repo / a pull request"},
	{Text: "open", Description: "open in browser (repo, PR, or project)"},
	{Text: "expand", Description: "toggle full (untruncated) value display"},
	{Text: "help", Description: "show command help"},
	{Text: "exit", Description: "quit the shell"},
}

// complete provides command + context-aware argument completion.
func (s *Session) complete(d prompt.Document) []prompt.Suggest {
	word := d.GetWordBeforeCursor()
	if strings.Contains(strings.TrimRight(d.TextBeforeCursor(), word), " ") {
		first := strings.Fields(d.TextBeforeCursor())
		if len(first) >= 1 {
			switch first[0] {
			case "use":
				return prompt.FilterHasPrefix([]prompt.Suggest{{Text: "project"}}, word, true)
			case "cd":
				sug := []prompt.Suggest{{Text: ".."}, {Text: "/"}}
				sug = append(sug, s.navSuggestions()...)
				return prompt.FilterHasPrefix(sug, word, true)
			case "vote":
				return prompt.FilterHasPrefix([]prompt.Suggest{
					{Text: "approve"}, {Text: "approve-suggestions"},
					{Text: "reset"}, {Text: "wait"}, {Text: "reject"},
				}, word, true)
			case "pr", "get", "open", "comment":
				return prompt.FilterHasPrefix(s.prSuggestions(), word, true)
			case "runs":
				return prompt.FilterHasPrefix(s.pipelineSuggestions(), word, true)
			case "prs":
				return prompt.FilterHasPrefix([]prompt.Suggest{
					{Text: "active"}, {Text: "completed"}, {Text: "abandoned"}, {Text: "all"},
				}, word, true)
			case "branches", "files", "refs":
				return prompt.FilterHasPrefix(s.repoSuggestions(), word, true)
			}
		}
		return nil
	}
	return prompt.FilterHasPrefix(topCommands, word, true)
}

// navSuggestions returns cd targets for the current level.
func (s *Session) navSuggestions() []prompt.Suggest {
	switch s.ctx.level {
	case levelRoot:
		out := make([]prompt.Suggest, 0, len(s.projects))
		for _, p := range s.projects {
			out = append(out, prompt.Suggest{Text: p.Name})
		}
		return out
	case levelProject:
		return s.repoSuggestions()
	default:
		return nil
	}
}

func (s *Session) repoSuggestions() []prompt.Suggest {
	out := make([]prompt.Suggest, 0, len(s.repos))
	for _, r := range s.repos {
		out = append(out, prompt.Suggest{Text: r.Name, Description: r.ID})
	}
	return out
}

func (s *Session) prSuggestions() []prompt.Suggest {
	out := make([]prompt.Suggest, 0, len(s.prs))
	for _, pr := range s.prs {
		out = append(out, prompt.Suggest{Text: fmt.Sprintf("%d", pr.PullRequestID), Description: pr.Title})
	}
	return out
}

func (s *Session) pipelineSuggestions() []prompt.Suggest {
	out := make([]prompt.Suggest, 0, len(s.pipelines))
	for _, pl := range s.pipelines {
		out = append(out, prompt.Suggest{Text: fmt.Sprintf("%d", pl.ID), Description: pl.Name})
	}
	return out
}

// printHelp lists the REPL commands.
func printHelp() {
	fmt.Println(`navigate projects > repos; most commands operate on the project in context.

commands:
  ls                      list current context (root: projects; project: repos; repo: branches)
  cd <project|repo>       navigate; cd .. up, cd / root
  use project <name>      jump to a project
  pwd                     show context path
  repos                   list repositories in the project
  prs [status]            list pull requests (status: active|completed|abandoned|all)
  pr <id>                 show a pull request
  vote <id> <vote>        cast your vote (approve|approve-suggestions|reset|wait|reject)
  comment <id> <text>     add a comment to a pull request
  pipelines               list pipelines
  runs <pipelineId>       list runs of a pipeline
  builds                  list recent builds
  build <id>              show a build
  releases                list releases
  release <id>            show a release
  branches [repo]         list branches (of the current repo, or a named one)
  files [repo] [path]     list files/folders
  get [id]                show the current repo, or a pull request by id
  open [id]               open the repo / a PR / the project in the browser
  expand                  toggle full (untruncated) value display
  help                    this help
  exit | quit             leave the shell`)
}

// stop terminates the shell. go-prompt does not expose a clean stop, so exit.
func stop() {
	os.Exit(0)
}

// openBrowser opens a URL in the default browser cross-platform.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
