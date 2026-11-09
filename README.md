# workctl

Author: [przestaw](https://github.com/przestaw)

[![CI](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml/badge.svg)](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml)

One binary for the work-tracking and code-hosting services a developer uses every day, as a CLI and as an MCP server
for local AI agents (VS Code, Claude Code, Cline, Cursor). Read-only by default; writes are opt-in.

| Module | Service | Source |
| --- | --- | --- |
| `azure` (`ado`) | Azure DevOps: repos, pull requests, pipelines, builds, releases | `internal/azure` |
| `atlassian` (`atl`) | Jira Cloud and Confluence Cloud | `internal/atlassian` |
| `jira` | Jira (REST v2) | `internal/jira` |
| `gitlab` | GitLab (REST v4) | `internal/gitlab` |
| `github` | GitHub (REST) | `internal/github` |
| `api` | any other HTTP service, or commands over ssh (generic profiles) | `internal/generic` |
| `bitbucket` | Bitbucket | reserved, not implemented yet |
| `asana` | Asana | reserved, not implemented yet |

`workctl bitbucket ...` and `workctl asana ...` only say that the module does not exist yet (`internal/app`); the names are
reserved so the command line stays stable. Macros are planned too (see the end).

Every module is optional. With no config file at all, `workctl azure ...` and `workctl atlassian ...` work as long as those
CLIs are set up (`workctl azure config init`); `jira` and `gitlab` need their URL in the config.

## Quick start

```powershell
go build -trimpath -ldflags "-s -w" -o workctl.exe .\cmd\workctl      # or scripts\build.ps1

workctl azure pr list -p "MyProject" --mine --status active
workctl azure pr threads 123 -p "MyProject" -e
workctl atlassian jql "project = ABC AND sprint in openSprints()"
workctl atlassian issue get ABC-123
workctl doctor
```

`azure` and `atlassian` are full command trees compiled into the binary; `ado` and `atl` stay as short aliases. They keep
their config files (`%APPDATA%\ado\config.yaml`, `%APPDATA%\atl\config.yaml`) and OS keyring entries, so profiles and
tokens made earlier keep working.

## Policy: read-only unless allowed

Writes are blocked until the config contains `"policy": {"allow_write": true}`. For `azure` and `atlassian` a command counts as a
read only if it is on the read list in `internal/modules` (`get`, `list`, `threads`, `diff`, `jql`, ...); every other command,
including any command a future version adds, counts as a write. Matching uses the command the CLI itself resolves, so
aliases (`azure pullrequest ...`) and flags before the command (`azure -p X pr list`) are handled. `raw` is a read only for
GET/HEAD without a body. `shell` counts as a write, because it can run anything.

The MCP surface is stricter, because it is what an agent can reach. It also refuses `config`, `setup`, `shell`,
`completion`, every `open` (it starts a browser), the `--config` and `--insecure` flags, and `raw` with a full URL (so
the credential cannot be sent to another host).

## MCP

```powershell
workctl --config C:\tools\workctl\workctl.json mcp
```

VS Code: `python3 scripts/install.py --vscode` (`python` on Windows) builds the binary into `~/.local/bin` and adds
it to the VS Code user profile as the MCP server `workctl` (`workctl mcp`, default config path, tokens from the keyring).
Then start it under **MCP: List Servers** and enable its tools in agent mode; restart it after changing the config.

Tools: `azure` and `atlassian` (one each; the argument is the command line as an array, for example
`["pr","get","123","-p","MyProject"]`), plus `jira_*`, `gitlab_*`, `github_*`, `api_request`/`api_download`/`api_exec` and `task_get_context` for the backends that are
configured. Write tools of `jira`/`gitlab`/`github` appear only with `allow_write`; the `azure`/`atlassian` tools refuse writes until then.
Each `azure`/`atlassian` call runs as a child process of workctl with its own timeout (`timeouts.azure`/`timeouts.atlassian`, else `modules.timeout_seconds`).

Timeouts are resolved per request: `profiles.<name>.timeout_seconds`, else `timeouts.<kind>` (for example
`"timeouts": {"jira": 60, "api": 120}`), else the global `timeout_seconds`. Both overrides are optional, so existing
configuration files keep working unchanged.
`examples/vscode-mcp-no-tokens.json` is a `.vscode/mcp.json` that needs no tokens in the file. `examples/vscode-mcp.json` and
`examples/generic-mcp.json` pass tokens as `WORKCTL_JIRA_TOKEN`/`WORKCTL_GITLAB_TOKEN`; that works only for profiles whose
`token_env` has that name (the default written by `workctl jira|gitlab init`).

The server speaks the legacy handshake-era MCP protocol on purpose; clients that probe `server/discover` fall back to
`initialize`. Do not pin a client to modern-only mode.

### Skill and plugin for agents

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

## Setup and credentials

Every module has its own guided setup, and all of them keep tokens in the OS keyring (Windows Credential Manager,
macOS Keychain, Secret Service), never in the config file:

```powershell
workctl init                 # walks through all modules, then asks about write access
workctl jira init            # URL, auth, TLS, token (hidden prompt) -> keyring, then a connection test
workctl gitlab init          # --profile NAME for a second instance
workctl github init
workctl azure init           # same as `workctl azure config init`
workctl atlassian init
workctl auth set-token --profile NAME   # replace a token later; `auth status`, `auth logout`; `auth login` for cli profiles
```

For scripts: `workctl jira init --non-interactive --url https://jira.example.com --token-stdin < token.txt`. A token is
never accepted as an argument (it would end up in shell history and process lists). Where there is no keyring (a
headless Linux box), set the profile's `token_env` variable instead (`init` defaults it to `WORKCTL_JIRA_TOKEN`,
`WORKCTL_GITLAB_TOKEN`, `WORKCTL_GITHUB_TOKEN`); azure and atlassian use `ADO_PAT` and `ATL_API_TOKEN`. The variable
wins over the keyring.

## Config

`%APPDATA%\workctl\config.json` (Linux: `~/.config/workctl/config.json`), or `--config FILE`, or `WORKCTL_CONFIG`.
Only `"version": 2` is accepted; see `config.example.json`.

Every connection is a named entry in `profiles` with `kind` (`jira`, `gitlab`, `github`, `azure`), `url`, `auth`,
`username`, `token_env`, `verify_tls`, `ca_file` and, where it applies, `default_repo`/`repos` or
`default_project`/`projects`. `defaults.<kind>` names the profile used when there is more than one of a kind. The token
is read from `token_env`, then from the keyring (service `workctl`, account `<kind>:<profile>`).

All of those selectors are optional. With several profiles of one kind, a call picks its profile in this order: an explicit
`--profile`/`profile` argument; the best match of the repository, project or Jira key against `repos`/`projects` (exact name
beats a prefix such as `"owner/*"`, longer prefix beats shorter; Jira keys match `projects` entries like `"IR"`); then
`defaults.<kind>` or the only profile of the kind. Two equally good matches are an error, never a guess. The MCP tool
`accounts` lists the profiles (credential state, never the secret), reports which profile resolves for a given
repository/project/key, and with `bind_profile` plus `bind_repo`/`bind_project` adds a pattern to a profile (refused when
another profile of the same host already holds it).

A `github` or `gitlab` profile may set `"backend": "cli"`: requests then go through `gh api` / `glab api`, which keep the login
(no token in workctl). The URL must be `https://HOST` without a path, `verify_tls` must be `true`, and `token_env` and `ca_file` are
refused. gh/glab environment overrides (`GH_TOKEN`, `GITLAB_TOKEN`, `GH_HOST`, ...) are not passed on. Source: `internal/api/cli.go`.

```powershell
workctl github init --profile personal --backend cli   # gh logs in on the terminal; no token is asked for
workctl --profile personal auth login                  # log in again (and after switching accounts)
workctl auth status                                    # "logged in as X via gh"
```

`init` and `auth login` store the logged-in account in the profile's `username`. A write (comment, create, edit) is refused
when gh/glab is logged in as someone else; to move a profile to another account clear its `username` and log in again.
`auth set-token` and `auth logout` do not apply to such a profile (use `gh auth logout`, which ends the session for every gh user).
The login is one per host: two accounts on the same host need the native backend.

Top-level `jira`/`gitlab`/`github` blocks (version 1) are rejected: move them into profiles. Tokens stored by the v2
release (account `<kind>:<url>`) are not read; the installers migrate them, or run `python scripts/migrate.py` (see its
`--dry-run`), or store them again with `workctl auth set-token --profile NAME`.

Profiles of kind `api` (management consoles, artifact repositories, any JSON API), `"backend": "ssh-tunnel"` (any HTTP profile behind a bastion)
and `"backend": "ssh-exec"` (APIs that are remote commands) are described, with every authentication method, in
[docs/services-auth.md](docs/services-auth.md).

## Build and test

For the code layout and design decisions see [ARCHITECTURE.md](ARCHITECTURE.md).

```powershell
go vet ./... ; go test ./... ; go build -trimpath -ldflags "-s -w" -o workctl.exe .\cmd\workctl
```

`scripts/release.sh` cross-compiles the binaries into `dist/` with a `SHA256SUMS` file; publishing them is a manual step.

Go 1.26. Third-party modules come in only through the two built-in modules (cobra, go-keyring, go-prompt, survey, yaml).
The azure and atlassian packages' own tests run too; run them with `APPDATA`/`USERPROFILE` pointing at a scratch directory if you
want to be sure nothing touches your real config.

## GitHub

```powershell
workctl github pr get 42 --repo owner/name
workctl github pr reviewers 42       # requested reviewers (users and teams)
workctl github pr edit 42 --add-reviewer user1,user2 --title "New title"
workctl github pr create --head feat --base main --title "Feature" --reviewer user1,user2
workctl github pr comments 42        # inline review threads; `discussion` is the conversation, `reviews` the verdicts
workctl github pr checks 42
workctl github pr list --state open --search "TASK-1234"
workctl github runs --branch main    # GitHub Actions; `run get|jobs ID` for one run
workctl github file get --ref main docs/README.md
```

`--repo` defaults to the github profile's `default_repo`. Lists return the first 100 items. Job logs are not offered: GitHub serves them
from another host, which workctl refuses to follow with the token. Writes (`pr comment add`, `pr create`, `pr edit`) need `allow_write`.

Two accounts: `workctl github init --profile NAME` once per account (the first becomes `defaults.github`), then
`workctl --profile NAME github ...` on the CLI, or `"profile": "NAME"` in any `github_*`, `gitlab_*` or `pr_*` MCP call.

## Git with a profile token

`workctl git-credential get` is a git credential helper: git gets the token of a github, gitlab or azure profile from
the keyring (or, for a github `cli` profile, from `gh auth token` for the bound account), only for https and the profile's
host (`api.github.com` serves `github.com`). With `--profile NAME` it answers for that profile. Without it the profile is
chosen from the host and, when git sends it, the repository path, by the same `repos`/`projects` matching as every other
call; an unbound repository on a host with several profiles gets `defaults.<kind>`, or nothing so another helper can answer.

One command wires every pull request profile host (`credential.useHttpPath` plus the helper, replacing the helpers
inherited for that host); plain git, GitKraken and GitLens then authenticate as the profile that owns the repository:

```powershell
workctl auth setup-git --dry-run   # print the git config commands
workctl auth setup-git
```

Per repository with a fixed profile instead:

```powershell
git config --add credential.https://github.com.helper '""'    # drop helpers configured globally
git config --add credential.https://github.com.helper "!workctl --profile gmail git-credential"
```

It is a CLI command only; the MCP server does not offer it.

## Planned

- **Implement `bitbucket` and `asana`:** replace the entry in `internal/app` (plannedModules) with a real module and its MCP tools.
- **Macros as MCP tools:** a named, parameterised sequence of module calls defined in a JSON file, for example
  "everything about ticket X" (Jira issue, matching pull requests, their open threads) in one call. A macro is one MCP
  tool (`macro_run`, with `macro_list` to discover them); it counts as a read only if every step is a read, and its steps
  go through the same policy and child-process path as the single `azure`/`atlassian` tools. `task ISSUE-KEY` is the
  hand-written ancestor of this.

## License

[BSD 4-Clause "Original" License](LICENSE) - [przestaw](https://github.com/przestaw). See [NOTICE](NOTICE).
