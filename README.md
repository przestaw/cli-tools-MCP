# workctl

Author: [przestaw](https://github.com/przestaw)

One binary for the work-tracking and code-hosting services a developer uses every day, as a CLI and as an MCP server
for local AI agents (VS Code, Claude Code, Cline, Cursor). Read-only by default; writes are opt-in.

| Module | Service | Source |
| --- | --- | --- |
| `azure` (`ado`) | Azure DevOps: repos, pull requests, pipelines, builds, releases | `internal/azure` |
| `atlassian` (`atl`) | Jira Cloud and Confluence Cloud | `internal/atlassian` |
| `jira` | Jira (REST v2) | `jira.go` |
| `gitlab` | GitLab (REST v4) | `gitlab.go` |
| `github` | GitHub (REST) | `github.go` |
| `bitbucket` | Bitbucket | reserved, not implemented yet |
| `asana` | Asana | reserved, not implemented yet |

`workctl bitbucket ...` and `workctl asana ...` only say that the module does not exist yet (`planned.go`); the names are
reserved so the command line stays stable. Macros are planned too (see the end).

Every module is optional. With no config file at all, `workctl azure ...` and `workctl atlassian ...` work as long as those
CLIs are set up (`workctl azure config init`); `jira` and `gitlab` need their URL in the config.

## Quick start

```powershell
go build -trimpath -ldflags "-s -w" -o workctl.exe .      # or scripts\build.ps1

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
read only if it is on the read list in `modules.go` (`get`, `list`, `threads`, `diff`, `jql`, ...); every other command,
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

VS Code: `scripts\install.ps1 -VSCode` (or `scripts/install.sh --vscode`) builds the binary into `~/.local/bin` and adds
it to the VS Code user profile as the MCP server `workctl` (`workctl mcp`, default config path, tokens from the keyring).
Then start it under **MCP: List Servers** and enable its tools in agent mode; restart it after changing the config.

Tools: `azure` and `atlassian` (one each; the argument is the command line as an array, for example
`["pr","get","123","-p","MyProject"]`), plus `jira_*`, `gitlab_*`, `github_*` and `task_get_context` for the backends that are
configured. Write tools of `jira`/`gitlab`/`github` appear only with `allow_write`; the `azure`/`atlassian` tools refuse writes until then.
Each `azure`/`atlassian` call runs as a child process of workctl with its own timeout (`modules.timeout_seconds`).
`examples/vscode-mcp-no-tokens.json` is a `.vscode/mcp.json` that needs no tokens in the file. `examples/vscode-mcp.json` and
`examples/generic-mcp.json` pass tokens as `WORKCTL_JIRA_TOKEN`/`WORKCTL_GITLAB_TOKEN`; that works only for profiles whose
`token_env` has that name (the default written by `workctl jira|gitlab init`).

The server speaks the legacy handshake-era MCP protocol on purpose; clients that probe `server/discover` fall back to
`initialize`. Do not pin a client to modern-only mode.

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
workctl auth set-token --profile NAME   # replace a token later; `auth status`, `auth logout`
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

Top-level `jira`/`gitlab`/`github` blocks (version 1) are rejected: move them into profiles. Tokens stored by version 1
(account `<kind>:<url>`) are not read; store them again with `workctl auth set-token --profile NAME`.

## Build and test

```powershell
go vet ./... ; go test ./... ; go build -trimpath -ldflags "-s -w" -o workctl.exe .
```

Go 1.26. Third-party modules come in only through the two built-in modules (cobra, go-keyring, go-prompt, survey, yaml).
The azure and atlassian packages' own tests run too; run them with `APPDATA`/`USERPROFILE` pointing at a scratch directory if you
want to be sure nothing touches your real config.

## GitHub

```powershell
workctl github pr get 42 --repo owner/name
workctl github pr comments 42        # inline review threads; `discussion` is the conversation, `reviews` the verdicts
workctl github pr checks 42
workctl github pr list --state open --search "TASK-1234"
workctl github runs --branch main    # GitHub Actions; `run get|jobs ID` for one run
workctl github file get --ref main docs/README.md
```

`--repo` defaults to the github profile's `default_repo`. Lists return the first 100 items. Job logs are not offered: GitHub serves them
from another host, which workctl refuses to follow with the token. Writes (`pr comment add`, `pr create`) need `allow_write`.

Two accounts: `workctl github init --profile NAME` once per account (the first becomes `defaults.github`), then
`workctl --profile NAME github ...` on the CLI, or `"profile": "NAME"` in any `github_*` / `pr_*` MCP call.

## Planned

- **Implement `bitbucket` and `asana`:** replace the entry in `planned.go` with a real module and its MCP tools.
- **Macros as MCP tools:** a named, parameterised sequence of module calls defined in a JSON file, for example
  "everything about ticket X" (Jira issue, matching pull requests, their open threads) in one call. A macro is one MCP
  tool (`macro_run`, with `macro_list` to discover them); it counts as a read only if every step is a read, and its steps
  go through the same policy and child-process path as the single `azure`/`atlassian` tools. `task ISSUE-KEY` is the
  hand-written ancestor of this.

## License

[Creative Commons Attribution 4.0 International (CC BY 4.0)](LICENSE) - [przestaw](https://github.com/przestaw)
