# MCP server, skill and plugin

Author: [przestaw](https://github.com/przestaw)

## Server

```powershell
workctl --config C:\tools\workctl\workctl.json mcp
```

VS Code: `python3 scripts/install.py --vscode` (`python` on Windows) builds the binary into `~/.local/bin` and adds
it to the VS Code user profile as the MCP server `workctl` (`workctl mcp`, default config path, tokens from the keyring).
Then start it under **MCP: List Servers** and enable its tools in agent mode; restart it after changing the config.

Tools (listed only for the backends that are configured):

| Tool | What |
| --- | --- |
| `accounts` | profiles, their bindings and credential state; binds a repository/project with consent |
| `pr_list`, `pr_show` | pull/merge requests (GitHub, GitLab, Azure DevOps); `include: [files, reviews, reviewers, comments, discussion, checks]`; files, reviews, comments and discussion are read page by page; reaching the 3000-item limit reports possible truncation as an error in the part, not a complete list |
| `pr_create`, `pr_edit` | create or edit a pull/merge request (write) |
| `ci` | runs/pipelines for a `ref`, one `run` with `include: [jobs]`, the log of one `job` |
| `file` | repository file at a ref |
| `comment` | comment on a pull/merge request (`repo` + `number`) or an issue (`issue`) (write) |
| `issue_show`, `issue_search`, `issue_update` | Jira Server and Cloud issues in one shape (see below) |
| `request`, `download` | any endpoint or file of any HTTP profile (github, gitlab, jira, atlassian, api) |
| `api_exec` | allow-listed remote commands of ssh-exec profiles |
| `task_get_context` | a Jira issue with its branches and merge requests |
| `cli` | `{"module": "azure", "args": ["pr","get","123","-p","MyProject"]}`: azure, atlassian and, with a jira profile, `jira` |

Writes are refused until `allow_write`. With `"policy": {"confirm_writes": true}` every write of the agent (a build
started through `request`, `pr_create`, `comment`, `issue_update`, a `cli` write, ...) also waits for the user's approval
in the MCP client (MCP elicitation); a client that cannot ask refuses the write instead of doing it silently, and
commands typed in a terminal are not asked about. Repository tools answer in a compact shape (the same fields for
GitHub and GitLab, with the web URL); `format: "raw"` returns the provider JSON. Only the tools above exist: the provider-specific `github_*`, `gitlab_*`,
`jira_*`, `api_request`, `api_download`, `azure` and `atlassian` tools were removed in 0.4.3.
`download` follows redirects only to known storage hosts (GitHub Actions logs and artifacts, release assets, user
attachments) and never sends the credential there; an HTML answer instead of the file is flagged. In `cli`,
`atlassian attachment download` and `page attachment-download` are reads that save into the downloads directory.
Each `cli` call runs as a child process of workctl with its own timeout (`timeouts.<module>`, else `modules.timeout_seconds`).

Timeouts are resolved per request: `profiles.<name>.timeout_seconds`, else `timeouts.<kind>` (for example
`"timeouts": {"jira": 60, "api": 120}`), else the global `timeout_seconds`. Both overrides are optional, so existing
configuration files keep working unchanged.
`examples/vscode-mcp-no-tokens.json` is a `.vscode/mcp.json` that needs no tokens in the file. `examples/vscode-mcp.json` and
`examples/generic-mcp.json` pass tokens as `WORKCTL_JIRA_TOKEN`/`WORKCTL_GITLAB_TOKEN`; that works only for profiles whose
`token_env` has that name (the default written by `workctl jira|gitlab init`).

The server speaks the legacy handshake-era MCP protocol on purpose; clients that probe `server/discover` fall back to
`initialize`. Do not pin a client to modern-only mode.

## Skill and plugin for agents

`plugins/workctl` is one package for all agents: a skill (`skills/workctl/SKILL.md`: which tool to call, comment only after the
user approved the text) and the server definition (`workctl mcp`, so the binary must be on `PATH`). The tool names live in
`internal/app`; `internal/app/plugin_test.go` fails when the skill names a tool that does not exist or the versions drift apart.

One installer for Windows, macOS and Linux (needs Go and Python 3; `claude`, `codex` and `gk` are taken from PATH or from
the VS Code extensions):

```powershell
python3 scripts/install.py --all      # or any of --vscode --claude --codex --gitkraken
```

| Flag | What it registers |
| --- | --- |
| `--vscode` | server in the user `mcp.json` of VS Code, Insiders and VSCodium; skill in `~/.copilot/skills` |
| `--claude` | server in Claude Code (user scope); skill in `~/.claude/skills` |
| `--codex` | server in Codex; skill in `~/.codex/skills` |
| `--gitkraken` | GitKraken MCP (shipped with GitLens) in Claude Code and Codex, read-only while workctl writes are off; VS Code gets it from GitLens |

It also migrates keyring entries of the v2 release. A server already registered as `workctl` is left as it is. Claude Code
can also install from a clone: `claude plugin marketplace add .` then `claude plugin install workctl@workctl`. Tokens are
never part of the package: they stay in the keyring or in gh/glab.
