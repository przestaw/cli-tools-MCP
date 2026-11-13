# CLI

Author: [przestaw](https://github.com/przestaw)

The command line of workctl: modules, everyday commands and the write policy. MCP: [mcp.md](mcp.md). Setup: [setup.md](setup.md).

## Modules

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

MCP write confirmation is configured separately: `confirm_writes` asks for every write, while
`confirm_session_writes` asks for the first write in each MCP connection and covers its remaining writes. These modes
are mutually exclusive. `confirm: true` still requests detailed approval for that operation.

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
