# Services and authentication manual

Author: [przestaw](https://github.com/przestaw)

What workctl can talk to, how each connection authenticates, and who owns which credential.
Configuration lives in `config.json` (`workctl config path` shows where); secrets live in the OS
keyring (Windows Credential Manager, macOS Keychain, Secret Service) under service `workctl`,
account `<kind>:<profile-name>`. Design background: [plan-generic-api-ssh.md](plans/plan-generic-api-ssh.md).

## The identity rule

Every connection has up to two identity layers, each with exactly one owner:

- **Transport identity** — SSH key or gh/glab login. Owned by `~/.ssh/config` + ssh-agent or by
  the provider CLI. workctl never stores, reads or forwards it.
- **Request identity** — token or basic credentials on the HTTP request. Owned by workctl
  (keyring or `token_env`). Never placed into argv or a remote environment.

If a backend owns the request identity itself (gh, glab, ssh-exec) or the profile uses
`auth: "none"`, workctl holds no token for it: `auth set-token` and `auth logout` say who owns the
login instead, and `auth status` shows the same reason.

## Supported services

| Service | kind | Backend | Request auth | Token owner |
| --- | --- | --- | --- | --- |
| GitHub / GitHub Enterprise | `github` | native | `Bearer` | workctl keyring / `token_env` |
| GitHub via gh | `github` | `cli` | gh adds it | gh (`gh auth login`) |
| GitLab / self-managed | `gitlab` | native | `PRIVATE-TOKEN` (default) or `Bearer` | workctl |
| GitLab via glab | `gitlab` | `cli` | glab adds it | glab (`glab auth login`) |
| Jira Server/DC (REST v2) | `jira` | native | `Bearer` (PAT, default) or `Basic` (user + token) | workctl |
| Azure DevOps | module `azure` | own CLI tree | PAT | module's own config + keyring |
| Jira/Confluence Cloud | module `atlassian` | own CLI tree | API token | module's own config + keyring |
| Management consoles with a web GUI and a JSON API (CI, fleet, ops tools) | `api` | native | `Basic` (user + API token or password) | workctl |
| Artifactory, Nexus, SonarQube, any JSON API | `api` | native | `none` / `basic` / `bearer` / `header` | workctl |
| Any `github`, `gitlab`, `jira` or `api` profile behind a bastion | same kind | `ssh-tunnel` | unchanged (see below) | workctl |
| SSH-command APIs (Gerrit style) | `api` | `ssh-exec` | none — SSH key is the identity | ssh-agent |
| Bitbucket, Asana | reserved | — | — | — |

## Authentication methods

### `auth: "bearer"` — `Authorization: Bearer <token>`

Default for `github`, `jira` and `api`. Store the token once:

```sh
workctl auth set-token --profile myjira --stdin
```

### `auth: "basic"` — user + secret

Jira with user/token pairs, management consoles. `username` comes from the profile, the secret from the
keyring; workctl builds the `Authorization: Basic` header itself.

```json
{ "kind": "api", "url": "https://fleet.example.com", "auth": "basic", "username": "stawczyk", "verify_tls": true }
```

### `auth: "header"` — custom header

For APIs that want `X-Api-Key` or similar. One extra field, no templates:

```json
{ "kind": "api", "url": "https://tool.example.com", "auth": "header", "auth_header": "X-Api-Key", "verify_tls": true }
```

### `auth: "form"` — login form of a web application

For web applications that have a GUI but no API token: workctl posts the login form once, like a
browser, and the session cookie it gets back authenticates the later requests. `username` comes
from the profile, the password from the keyring (`workctl auth set-token`); the password is sent
only in the body of the login POST. Pages come back as the server renders them, usually HTML,
and reading them is up to the consumer. Combine it with `"read_only": true`:

```json
{
  "kind": "api",
  "url": "https://fleet.example.com/console",
  "auth": "form",
  "username": "stawczyk",
  "verify_tls": true,
  "read_only": true,
  "form_login": {
    "path": "/login",
    "fields": { "action": "login" },
    "user_field": "user",
    "password_field": "password",
    "session_cookie": "fleet_session"
  }
}
```

`fields` are extra form fields the login form sends (hidden inputs). The login counts as done
only when the answer sets `session_cookie`: a wrong password usually comes back as the login page
with HTTP 200. Without `logout_path` the session cookies are kept between runs in the user cache directory
(`workctl/session-*.json`, mode 0600), so a profile logs in once, not once per command (servers cap the sessions per
user); when the server answers a saved session with its login page, workctl logs in again and repeats the request
once. `workctl auth logout` deletes that file. With `logout_path` nothing is kept: every run logs out (below).
SSO or two-factor logins cannot be automated this way.

Set `logout_path` to close each CLI/MCP session, including after a failed request. Cleanup has its
own timeout, independent of the request context, and failures are returned to the caller rather
than silently ignored. Without `logout_verify_path`, cleanup only checks that the session cookie
was removed; that alone does not prove server-side invalidation.

For OTRS, add these fields inside `form_login`:

```json
{
  "logout_path": "/index.pl?Action=Logout",
  "logout_token_field": "ChallengeToken",
  "logout_verify_path": "/index.pl?Action=AgentPreferences"
}
```

`logout_token_field` is optional: workctl reads its value from a hidden input or a link/form query
in an authenticated page and sends it only to the configured logout endpoint. It understands OTRS
semicolon-separated queries and HTML entities. Tokens and cookies remain in memory.
`logout_verify_path` must identify a protected, non-mutating page, not a public login page. After
logout, workctl replays the original cookies against that page without logging in again and requires
the login form's password input. It does not merely trust HTTP 200 or a deleted browser cookie.

Verify a profile without printing HTML, cookies, passwords or tokens:

```sh
workctl --profile otrs api session-check
```

This checks that the verification page is authenticated before logout and rejects the original
session afterwards. It never requests ticket details. All `api raw`, `api download`, `api_request`
and `api_download` calls use the same automatic cleanup. Check the command's exit status; stop on
a cleanup error rather than repeatedly opening sessions. `workctl auth logout` removes the stored
credential; it is not the server-session logout command. Restart existing MCP servers after
installing a new binary; already-running processes still use the old code.

### `read_only` — reads only

Any HTTP profile may set `"read_only": true`: workctl then refuses every method but GET and HEAD
for it, even with `policy.allow_write`. Note that some web pages change state on a GET (for
example marking an item as seen), exactly as in a browser.

### `auth: "none"`

Unauthenticated endpoints (kind `api` only), and required for `ssh-exec` profiles, whose SSH key
is the identity.

### `token_env` — secret from the environment

Any native profile may set `"token_env": "MY_TOKEN"`; the variable wins over the keyring entry.
For CI jobs where no keyring exists.

### CLI-owned login (`backend: "cli"`)

gh or glab performs and refreshes the login; `workctl auth login --profile NAME` binds the profile
to the account the CLI is logged in as, and writes are refused if the accounts diverge.

## Using an `api` profile

```sh
workctl --profile fleet api raw GET /api/v1/devices
workctl --profile fleet api raw --body '{}' POST /api/v1/devices/42/reboot   # needs policy.allow_write
workctl doctor                                                        # GET / on every api profile
```

Paths are relative to the profile `url` (including its path); absolute URLs are refused. MCP tool:
`api_request` with `method`, `path`, `query`, `body` and optional `profile`; anything but GET/HEAD needs
`allow_write`. `defaults.api` picks the profile when there are several.

Query parameters are encoded by workctl, so values with spaces or several values of one key (a
multi-select) need no hand-made escaping: `--query KEY=VALUE` (repeatable) on the CLI, and on MCP
`"query": {"region": ["eu-west", "eu-central"], "channel": "stable", "dry_run": false}`.

### Downloads (binary files, archives, logs)

`api raw` prints text for reading: JSON is indented and a final newline is added. For files use
`api download`, which streams the answer to disk unchanged and prints where it went:

```sh
workctl --profile fleet api download --query id=42 /api/v1/logs/export
# {"path":"~/.cache/workctl/downloads/download-123-export.log.gz","bytes":812345,
#  "sha256":"...","content_type":"application/gzip","server_file_name":"export.log.gz"}
```

Without `--output FILE` the file lands in `<user cache dir>/workctl/downloads` under a unique
prefix and the server's file name (directories and odd characters dropped), so nothing is
overwritten; a failed download leaves no file. MCP tool: `api_download` with `path`, `query` and an
optional `filename`; it never takes a target directory, so an agent cannot write outside the
downloads directory. Downloads are GETs and work on `read_only` profiles. The profile's
`timeout_seconds` limits the whole transfer.

### Anti-CSRF tokens

Some servers refuse a write signed with a password unless it carries a token that a GET handed out
in the same session (API tokens are often exempt). Describe where the token comes from in the
profile's `csrf` block; before the first write workctl fetches it, keeps the session cookie in
memory and sends the token header. Neither reaches the output. A 404 from `path` means the
server has CSRF protection off.

| Field | Meaning |
| --- | --- |
| `path` | GET endpoint that answers JSON with the token |
| `token_field` | JSON field holding the token |
| `header` | header that carries the token, or |
| `header_field` | JSON field naming that header (when the server says which header it wants) |

A server that also says which header it wants (`{"header": "X-Fleet-CSRF", "token": "..."}`):

```json
"csrf": { "path": "/api/v1/session/csrf", "header_field": "header", "token_field": "token" }
```

A server with a fixed header:

```json
"csrf": { "path": "/api/csrf", "header": "X-CSRF-Token", "token_field": "token" }
```

```sh
workctl --profile fleet api raw \
  --query channel=stable --query region=eu-west --query region=eu-central \
  POST /api/v1/deployments
```

## SSH

### Tunnel by hand, with no workctl feature

A service reachable only through a bastion also works with a tunnel you start yourself:

```sh
ssh -L 8443:internal-service:443 -p 2222 -J jumphost bastion   # or put all of it in ~/.ssh/config
```

point the profile at `https://localhost:8443` and set `ca_file`/`verify_tls` if the certificate
does not match `localhost`.

### `backend: "ssh-tunnel"`

workctl dials every connection through `ssh -o BatchMode=yes -W host:port -- <ssh_destination>`:
no local port is opened, TLS is verified against the *real* hostname, and the ssh process ends
with its connection.

```json
{
  "kind": "jira",
  "url": "https://jira.internal.example.com",
  "auth": "bearer",
  "verify_tls": true,
  "backend": "ssh-tunnel",
  "ssh_destination": "bastion-prod"
}
```

`ssh_destination` is handed to your system `ssh` (a host, `user@host`, `ssh://user@host:2222` or a
`~/.ssh/config` alias; it may not start with `-`). Odd ports, `ProxyJump`, key selection and
connection reuse are **ssh config features, not workctl ones**:

```ssh-config
Host bastion-prod
    HostName bastion.example.com
    Port 2222
    ProxyJump edge.example.com
    User stawczyk
    IdentityFile ~/.ssh/id_work
    # one ssh login reused by every connection:
    ControlMaster auto
    ControlPath ~/.ssh/cm-%C
    ControlPersist 5m
```

`BatchMode=yes` means ssh never prompts: load the key into ssh-agent (or use a key without a
passphrase) and accept the host key once with a manual `ssh bastion-prod`. When ssh fails, the
workctl error shows its message, for example `Permission denied (publickey)`.

The SSH key authenticates the tunnel; the profile token authenticates the request. Two layers,
two owners — this is the supported way to combine a key with a token.

### `backend: "ssh-exec"`

For APIs that are a remote command. The SSH key is the only identity, so `auth` must be `"none"`,
the profile has no `url`, and `auth set-token` refuses it. Only commands on the profile's allowlist
run:

```json
{
  "kind": "api",
  "backend": "ssh-exec",
  "ssh_destination": "gerrit",
  "auth": "none",
  "commands": [
    { "prefix": ["gerrit", "query"], "read_only": true },
    { "prefix": ["gerrit", "review"] }
  ]
}
```

```ssh-config
Host gerrit
    HostName review.example.com
    # the Gerrit ssh port:
    Port 29418
    User stawczyk
```

```sh
workctl --profile gerrit api exec -- gerrit query --format JSON status:open
```

A command runs only if its first words equal one `prefix`. ssh hands the remote side one command
line, so workctl single-quotes every word: `'x; rm -rf ~'` arrives as one argument, not a second
command. Commands under a prefix without `read_only` are writes and need `allow_write`. MCP tool:
`api_exec` with `args` (one word per item); the allowlist is what makes it safe to expose.
`workctl doctor` does not run anything for these profiles.

## What is deliberately not supported

- workctl tokens in ssh argv or the remote environment — visible in `ps` on the remote host.
- Custom auth on `cli`/`ssh-exec` backends — the transport owns that identity.
- Running a command over ssh that is not on the profile's allowlist.
- Absolute URLs in `raw`/`api_request` — a credential never leaves its profile's host.
- Secret placeholders in paths/query strings — rethink the profile, or ask for the feature with a
  concrete API that needs it.
