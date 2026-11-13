# workctl

[![CI](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml/badge.svg)](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-BSD--4--Clause-lightgrey)](LICENSE)

**GitHub, GitLab, Jira, Confluence and Azure DevOps for your AI agent — one binary, read-only by default.**

workctl is a CLI and an MCP server for local agents (VS Code Copilot, Claude Code, Codex, Cline, Cursor). The agent reads
pull requests, tickets, CI runs and attachments in one compact shape; it writes only when you allow it, and with
`confirm_writes` it asks you before every write.

## Features

- **One shape for every provider:** `pr_show` returns the same fields for GitHub and GitLab, with reviews, checks and
  comments on request; `issue_*` does the same for Jira Server and Cloud. Fewer tokens, no per-provider parsing.
- **Safe by default:** read-only until `allow_write`; `confirm_writes` makes every agent write wait for your approval in
  the MCP client; tokens live in the OS keyring, never in the config.
- **One account per repository:** profiles match repositories and projects by pattern, so work and personal accounts
  never mix.
- **Agent package included:** a skill and a plugin for VS Code, Claude Code and Codex, installed with one command.

## Install

Needs Go 1.26 and Python 3.

```powershell
python3 scripts/install.py --all      # or any of --vscode --claude --codex --gitkraken
workctl init                          # guided setup of every service; tokens go to the keyring
```

The installer builds `workctl` into `~/.local/bin`, registers the MCP server and copies the skill:

| Host | Server | Skill |
| --- | --- | --- |
| VS Code (also Insiders, VSCodium) | user `mcp.json` | `~/.copilot/skills` |
| Claude Code | user scope (or `claude plugin install workctl@workctl` from a clone) | `~/.claude/skills` |
| Codex | Codex config | `~/.codex/skills` |

Manual registration: any MCP client that starts `workctl mcp` (the binary on `PATH`) works.

## Tools

| Tool | What |
| --- | --- |
| `pr_list`, `pr_show` | pull/merge requests; `include: [files, reviews, reviewers, comments, discussion, checks]` |
| `pr_create`, `pr_edit`, `comment` | create, edit and comment (write) |
| `upload` | an image or video for a description or comment: GitHub attachment, GitLab upload, Jira attachment (write) |
| `batch` | several writes (for example Jenkins builds) after one approval |
| `ci` | CI runs and pipelines, jobs, job logs |
| `file` | a repository file at a ref |
| `issue_show`, `issue_search`, `issue_update` | Jira issues; `issue_update` supports `dry_run` |
| `task_get_context` | a Jira issue with its branches and merge requests |
| `request`, `download` | any endpoint or file of a configured HTTP profile |
| `api_exec` | allow-listed remote commands over ssh |
| `accounts` | profiles and which one serves a repository |
| `cli` | the embedded `azure`, `atlassian` and `jira` command trees |

Tools are listed only for configured backends. `format: "raw"` returns the provider JSON instead of the compact shape.

## Safety

| Switch | Effect |
| --- | --- |
| `policy.allow_write: false` (default) | every write is refused |
| `policy.confirm_writes: true` (optional, default off) | every agent write waits for your approval; `batch` asks once for its list; a client that cannot ask refuses it |
| keyring | tokens never touch the config file, the command line or the agent |

Use `confirm: true` for one high-risk write or batch without changing global policy. Refusal or cancellation is not
permission to retry through CLI, another account, or changed settings.

Commands you type in a terminal are yours and are not asked about. Details: [docs/cli.md](docs/cli.md#policy-read-only-unless-allowed).

## Documentation

| Topic | Where |
| --- | --- |
| CLI modules and everyday commands | [docs/cli.md](docs/cli.md) |
| MCP server, skill and plugin | [docs/mcp.md](docs/mcp.md) |
| Setup, credentials and config | [docs/setup.md](docs/setup.md) |
| Other services, ssh and form login | [docs/services-auth.md](docs/services-auth.md) |
| Build, test and versioning | [docs/development.md](docs/development.md) |
| Code layout | [ARCHITECTURE.md](ARCHITECTURE.md) |
| Plans and design records | [docs/plans/](docs/plans/README.md) |

## License

[BSD 4-Clause "Original" License](LICENSE) - [przestaw](https://github.com/przestaw). See [NOTICE](NOTICE).
