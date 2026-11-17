// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"workctl/internal/config"
	"workctl/internal/mcp"
)

// commandHelp is the text of `workctl help COMMAND` and `workctl COMMAND --help`. Flags of the leaf commands
// (`api raw -h`) come from their flag sets; this text names the commands and says what they are for.
var commandHelp = map[string]string{
	"api": `workctl [--profile NAME] api COMMAND

Any HTTP profile (kinds api, github, gitlab, jira, atlassian, google) or an ssh-exec profile. --profile must name a
profile that exists and has a usable kind; it is never replaced by a default.

  raw [--body JSON|@file] [--query KEY=VALUE]... METHOD PATH
        one request. The response is written exactly as received: no indentation, no added newline.
        Anything but GET and HEAD needs policy.allow_write. MCP: request.
  download [--query KEY=VALUE]... [--output FILE] [--name NAME] [--export-mime MIME] PATH
        stream a file to disk without changing a byte; prints path, size, SHA-256, content type and a warning when an
        HTML page (login, error) came instead. Drive: ID or file URL, metadata and checksum verification;
        native documents require --export-mime. MCP: download.
  upload [--folder ID] [--name NAME] [--target PATH] [--overwrite] FILE
        upload a file (Google Drive, or an issue attachment). Refuses to overwrite without --overwrite. MCP: upload.
  exec [--] CMD [ARG...]
        one allowed remote command of an ssh-exec profile. MCP: api_exec.
  session-check
        verify the form login and the server-side logout.

PATH is relative to the profile's API prefix, or starts with / for the site root. A full URL is accepted only for the
profile's own server (and the Google hosts of a google profile). Put values that need encoding in --query.
Per-command flags: workctl api raw -h`,
	"accounts": `workctl accounts [--kind KIND] [--resource RESOURCE] [--profile NAME] [--check]

List the configured profiles: kind, host, account, bound repos/projects, credential state (never a secret).
With --resource (owner/name, group/project or a Jira key) it shows which profile resolves for it. Use it to pick
--profile when a command says that several profiles match. --check fetches Drive account/quota and reports
errors per profile; without it no Drive network check is made. MCP: accounts.`,
	"file": `workctl file [--profile NAME] [--repo REPO] [--ref REF] [PATH]

Repository: read PATH at REF. Drive profile: PATH is a file ID (metadata) or folder ID (listing).
Omit PATH to list --folder ID, profile default folder or root.
Drive flags: --folder ID (not with PATH), --query TEXT (name contains), --limit 1..1000 (default 30), --page-token TOKEN.
For bytes or native document exports use api download. MCP: file.`,
	"pr": `workctl [--profile NAME] pr|mr list|show [flags]

  list  [--repo R] [--project P] [--state open|closed|merged|all] [--search TEXT]
  show  [--repo R] [--project P] [--include PARTS] NUMBER
        PARTS is a comma list of files, reviews, reviewers, comments, discussion, checks (GitHub and GitLab); the output
        is then JSON with the same keys as the pr_show tool.
  common: --provider github|gitlab|azure  --format text|json  --json  (--format raw: provider JSON, show only)

MCP: pr_list, pr_show.`,
	"tools": `workctl tools [--all] [NAME]

List the MCP tools (the agent's view) with the CLI command that does the same. With NAME: description and
parameters. Without --all only the tools of configured backends are listed.`,
	"config": `workctl config show|path

show  print the configuration; identifiers and credential-related values (OAuth client id, token_env, auth_header,
      form fields) are replaced with "***". Secrets are in the OS keyring, never in the file.
path  print the path of the configuration file.`,
	"doctor": `workctl doctor

Check every profile (and the azure and atlassian modules). A failed profile carries a "code": dns, timeout, tls,
refused, unauthorized, forbidden or other, next to the error text. Exit code 1 when a profile failed.`,
}

// cliEquivalent is the CLI command that does what an MCP tool does; tools without one are for agents only.
var cliEquivalent = map[string]string{
	"file":     "file",
	"accounts": "accounts", "request": "api raw", "download": "api download", "upload": "api upload", "api_exec": "api exec",
	"pr_list": "pr list", "pr_show": "pr show", "task_get_context": "task KEY",
	"issue_show": "issue show", "issue_update": "issue update|move|close|dup|link", "issue_search": "issue search",
	"comment": "issue comment (issues only)",
}

// helpWanted reports whether the first argument after a command asks for its help.
func helpWanted(args []string) bool {
	return len(args) > 0 && (args[0] == "-h" || args[0] == "--help" || args[0] == "help")
}

// printCommandHelp writes the help of a command; it reports false when the command has none of its own.
func printCommandHelp(w io.Writer, command string) bool {
	if command == "mr" {
		command = "pr"
	}
	text, ok := commandHelp[command]
	if ok {
		fmt.Fprintln(w, text)
	}
	return ok
}

// toolsCommand is `workctl tools [--all] [NAME]`.
func toolsCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("tools", flag.ContinueOnError)
	all := fs.Bool("all", false, "include the tools of backends that are not configured")
	if err := fs.Parse(args); err != nil {
		return err
	}
	tools := toolList(cfg, !*all)
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	if fs.NArg() == 1 {
		for _, t := range toolList(cfg, false) {
			if t.Name == fs.Arg(0) {
				printTool(os.Stdout, t)
				return nil
			}
		}
		return fmt.Errorf("unknown tool %q (see: workctl tools --all)", fs.Arg(0))
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: workctl tools [--all] [NAME]")
	}
	for _, t := range tools {
		cli := cliEquivalent[t.Name]
		if cli == "" {
			cli = "-"
		}
		fmt.Fprintf(os.Stdout, "%-18s %-30s %s\n", t.Name, cli, firstSentence(t.Description))
	}
	return nil
}

func printTool(w io.Writer, t mcp.Tool) {
	fmt.Fprintf(w, "%s\n%s\n", t.Name, t.Description)
	if cli := cliEquivalent[t.Name]; cli != "" {
		fmt.Fprintf(w, "CLI: workctl %s\n", cli)
	}
	props, _ := t.InputSchema["properties"].(map[string]any)
	required := map[string]bool{}
	if list, ok := t.InputSchema["required"].([]string); ok {
		for _, name := range list {
			required[name] = true
		}
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	fmt.Fprintln(w, "\nParameters:")
	for _, name := range names {
		prop, _ := props[name].(map[string]any)
		kind, _ := prop["type"].(string)
		desc, _ := prop["description"].(string)
		mark := ""
		if required[name] {
			mark = " (required)"
		}
		fmt.Fprintf(w, "  %s  %s%s  %s\n", name, kind, mark, desc)
	}
}

// firstSentence shortens a tool description to its first sentence for the list.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	if len(s) > 110 {
		s = s[:107] + "..."
	}
	return s
}
