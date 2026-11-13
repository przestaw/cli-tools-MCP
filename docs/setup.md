# Setup, credentials and config

Author: [przestaw](https://github.com/przestaw)

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

For an MCP agent that may write after one approval per connection:

```json
"policy": {
  "allow_write": true,
  "confirm_session_writes": true
}
```

Use `confirm_writes: true` instead to approve every write; the two confirmation modes cannot both be true. An MCP
client without elicitation support refuses writes that need either confirmation mode.

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

Native `github` and `gitlab` profiles are bound the same way: `init` and `auth set-token` store the token's account
(`GET /user`) in `username`, and the first write of a run checks the token again and is refused when it belongs to
someone else. `init` and `auth set-token` check the supplied token before replacing the keyring entry, independently of
`token_env`. A token of another account leaves the old entry untouched. When the account cannot be checked (no network),
the token is stored with a warning and any existing account binding is kept; an unbound profile stays unbound.

Top-level `jira`/`gitlab`/`github` blocks (version 1) are rejected: move them into profiles. Tokens stored by the v2
release (account `<kind>:<url>`) are not read; the installers migrate them, or run `python scripts/migrate.py` (see its
`--dry-run`), or store them again with `workctl auth set-token --profile NAME`.

Profiles of kind `api` (management consoles, artifact repositories, any JSON API), `"backend": "ssh-tunnel"` (any HTTP profile behind a bastion)
and `"backend": "ssh-exec"` (APIs that are remote commands) are described, with every authentication method, in
[services-auth.md](services-auth.md).
