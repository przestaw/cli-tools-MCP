// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strings"

	"workctl/internal/config"
	"workctl/internal/issue"
	"workctl/internal/mcp"
	"workctl/internal/modules"
)

// commandHelp is the text of `workctl help COMMAND` and `workctl COMMAND --help`. Flags of the leaf commands
// (`api raw -h`) come from their flag sets; this text names the commands and says what they are for.
var commandHelp = map[string]string{
	"api": `workctl [--profile NAME] api COMMAND

Any HTTP profile (kinds api, github, gitlab, jira, atlassian, google) or an ssh-exec profile. --profile must name a
profile that exists and has a usable kind; it is never replaced by a default.

  raw [--body JSON|@file] [--query KEY=VALUE]... [--output original|compact] METHOD PATH
        one request. Every response is written unchanged, without indentation or an added newline.
        Optional --output compact minifies JSON (numbers untouched) and turns HTML into its visible text (lossy);
        XML, other text and binary data are unchanged.
        Anything but GET and HEAD needs policy.allow_write. MCP: request (output).
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
--kind limits the list to profiles of that kind. --resource (owner/name, group/project or a Jira key) shows which
profile resolves for it (within the kind, if given); it does not filter the list. Use it to pick
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
        PARTS is a comma list of files, diff, reviews, reviewers, comments, discussion, checks (GitHub and GitLab); the output
        is then JSON with the same keys as the pr_show tool.
        diff is the unified diff as a string; over the cap named in 'workctl tools pr_show' it is an object with
        truncated, bytes_total, bytes_returned, hint.
  common: --provider github|gitlab|azure  --format text|json|compact|raw  --json
          (default text; compact = the JSON of the pr_list/pr_show tools, details compacted; raw: keep the provider details, no projection)

MCP: pr_list, pr_show.`,
	"tools": `workctl tools [--all] [NAME]

List the MCP tools (the agent's view) with the CLI command that does the same. With NAME: description and
parameters. Without --all only the tools of configured backends are listed.`,
	"config": `workctl config show|path

show  print the configuration; identifiers and credential-related values (OAuth client id, token_env, auth_header,
      form fields) are replaced with "***". Secrets are in the OS keyring, never in the file.
path  print the path of the configuration file.`,
	"skills": `workctl skills status|install [--target vscode|claude|codex|all] [--with compact-output] [--dry-run] [--check]

The agent skills are embedded in the binary. Targets: vscode (~/.copilot/skills), claude (~/.claude/skills),
codex ($CODEX_HOME or ~/.codex, then skills); default all.
  status   one line per target and skill: target, skill, state (missing, current, differs), path. Exit code 0;
           with --check 1 when a selected skill is missing or differs.
  install  copy the workctl skill (plus the ones named in --with); only files of these skills are written, nothing is
           deleted, and files that are already current are left alone. --dry-run writes nothing.
Optional skills: compact-output. CLI only, there is no MCP tool.`,
	"doctor": `workctl doctor

Check every profile (and the azure and atlassian modules). A failed profile carries a "code": dns, timeout, tls,
refused, unauthorized, forbidden or other, next to the error text. Exit code 1 when a profile failed.
The "skills" entry compares installed workctl skills with the embedded ones (code "outdated", with the fix command);
it is informational and never changes the exit code.`,
	"init": `workctl [--config FILE] init [module...]   (alias: setup)

Guided setup of the configuration file. Without arguments it asks for each service in turn (jira, gitlab, github,
azure, atlassian) and for policy.allow_write; with module names it sets up only those. Tokens go to the OS keyring,
never to the config file. Non-interactive form per endpoint: workctl github|gitlab|jira init [--profile NAME] [--url U]
[--auth SCHEME] [--user U] [--backend native|cli] [--token-stdin] [--non-interactive].`,
	"auth": `workctl [--config FILE] auth login|set-token|status|logout|setup-git [flags]

  login      [--profile NAME] [--no-browser]   OAuth 2.0 loopback login, or the gh/glab login of a cli profile
  set-token  [--profile NAME] [--stdin]        store the token of a profile in the OS keyring
  status     [--profile NAME]                  credential state of the profiles (never the secret)
  logout     [--profile NAME]                  remove the stored credential
  setup-git  [--dry-run]                       point git at workctl as the credential helper for the profile hosts`,
	"jira": `workctl [--profile NAME] jira COMMAND [flags]

Jira Server/Data Center REST: issue, search, comments, transitions, transition, comment, myself, server-info, raw,
and init (profile setup: workctl jira init). Output is the JSON of the server. Writes need policy.allow_write.
For provider-neutral issue work use: workctl help issue.`,
	"gitlab": `workctl [--profile NAME] gitlab COMMAND [flags]

GitLab REST: project, mr, pipelines, pipeline, job, branches, file, raw, and init (profile setup: workctl gitlab init).
Output is the JSON of the server. Writes need policy.allow_write. For merge requests see also: workctl help pr.`,
	"github": `workctl [--profile NAME] github COMMAND [flags]

GitHub REST: repo, pr, branches, runs, run, file, raw, and init (profile setup: workctl github init). Most commands
take --repo owner/name (else the profile's default repo). Output is the JSON of the server. Writes need
policy.allow_write. For pull requests see also: workctl help pr.`,
	"issue": issue.Usage,
	"task": `workctl task ISSUE-KEY

One JSON document: the Jira issue plus the branches and merge requests that mention its key in the configured
GitLab projects. MCP: task_get_context.`,
	"mcp": `workctl [--config FILE] mcp

Run the MCP server on stdin/stdout (JSON-RPC). Started by an agent, not by hand; see workctl tools for what it offers.`,
	"git-credential": `workctl [--profile NAME] git-credential get

Git credential helper: reads git's key=value request on stdin and answers with the token of the profile for the host
(and repository path). It stays silent when no profile matches. Set it up with: workctl auth setup-git.`,
	"version": `workctl version

Print the version of the binary.`,
	"help": `workctl help [COMMAND]

Without COMMAND the list of commands; with COMMAND the help of that command. Also: workctl COMMAND --help.`,
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

// canonicalCommand maps the aliases (mr, ado, atl, i, setup) to the command they name.
func canonicalCommand(name string) string {
	if full := modules.Aliases[name]; full != "" {
		return full
	}
	switch name {
	case "mr":
		return "pr"
	case "i":
		return "issue"
	case "setup":
		return "init"
	}
	return name
}

// printCommandHelp writes the help of a command; it reports false when the name is not a command. The cobra
// module trees print their own help (to stdout, side effect free).
func printCommandHelp(w io.Writer, command string) bool {
	command = canonicalCommand(command)
	if text, ok := commandHelp[command]; ok {
		fmt.Fprintln(w, text)
		return true
	}
	if desc, ok := plannedModules[command]; ok {
		fmt.Fprintf(w, "workctl %s\n\nReserved for %s. Not implemented yet.\n", command, desc)
		return true
	}
	return slices.Contains(modules.Names(), command) && modules.Command(config.Default(), command, []string{"--help"}) == nil
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
	names := slices.Sorted(maps.Keys(props))
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
