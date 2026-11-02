# Plan: generic API profiles and SSH transport

Author: [przestaw](https://github.com/przestaw)

Status: implemented in four commits (phases 1–4 below). Notes marked *As built* record where the code
differs from the first draft and why. User manual: [services-auth.md](services-auth.md).

## Identity rule (the one invariant)

**One identity per layer, one owner per identity. Never mixed.**

| Layer | Identity | Owner | workctl's role |
| --- | --- | --- | --- |
| Connection (transport) | SSH key, gh/glab login | `~/.ssh/config` + ssh-agent, or the provider CLI | starts the process, never injects a secret |
| HTTP request | token / basic credentials | workctl keyring (`secret.Account(kind, name)`) or `token_env` | resolves and attaches via `api.NewProfileClient` |

Allowed combinations:

1. **native HTTP + workctl token** — existing (`github`, `gitlab`, `jira`), extended to `kind: "api"`.
2. **cli backend** (`gh`/`glab`) — the CLI owns both layers; workctl holds no secret. Existing.
3. **ssh-tunnel + workctl token** — SSH key authenticates the tunnel (layer 1), the profile token
   authenticates the request (layer 2). Two identities on two layers is layering, not mixing.
4. **ssh-exec, key only** — the remote command is the API; SSH is the only identity; the profile
   must have `auth: "none"` and no keyring entry (`config.NoTokenError`, which replaced `CLIManagedError`).

Refused, with explicit errors (never silent fallback):

- a workctl token in ssh argv or remote environment (visible in `ps` on the remote host),
- `ssh-exec` with any `auth` other than `"none"`,
- custom auth headers on the `cli` backend (gh/glab own that identity),
- absolute URLs in `raw` (existing `relPath` rule: a credential never leaves the profile's host).

## Phase 1 — generic HTTP profile (`kind: "api"`)

Smallest useful step; one new package (`internal/generic`) for the command and the MCP tool.

- `config.Profile`: add `AuthHeader string` (`auth_header`, used only with `auth: "header"`).
- `config.ValidateProfile`: accept kind `api`; require `auth` ∈ `none|basic|bearer|header`;
  `header` requires `auth_header`, `basic` requires `username`.
- `api.NewProfileClient`: new `case "api"` in the existing kind switch — no prefix, auth from the
  profile fields. Roughly 15 lines.
- Expose the existing `api.RawCommand` for the new kind (CLI `workctl api raw ...`) and as the MCP
  tool `api_request`, under the same write policy and host pinning as today.
- `doctor`: an `api` profile check = credential resolves + `GET /` on the profile URL answers 2xx.
  *As built:* no `health_path` field; point `url` at a path that answers if `/` does not.
- Covers Jenkins (`auth: "basic"`, username + API token), Artifactory/Nexus (`header`/`bearer`),
  SonarQube (`bearer`), any JSON-over-HTTP service.

Out of scope, deliberately: placeholder/template mapping of secrets into paths or query strings.
One secret per profile, placed by scheme, is enough until a concrete API proves otherwise.

## Phase 2 — SSH tunnel (`backend: "ssh-tunnel"`)

A tunnel is only a dialer, so it composes with **every** HTTP kind (`api`, `gitlab`, `jira`,
`github` native) without touching their auth code.

- Profile field: `ssh_destination` (string), passed verbatim to the system `ssh` binary.
  Odd ports and jump hosts need no workctl syntax: use `ssh://user@host:2222` or a
  `~/.ssh/config` alias with `Port`, `ProxyJump`, `IdentityFile`, `ControlMaster`.
- *As built:* no local port at all. Every TCP connection of the HTTP client is one
  `ssh -o BatchMode=yes -W host:port -- <destination>` child process (`internal/api/ssh.go`):
  ssh forwards its stdin/stdout to the service, `useSSHTunnel` only replaces
  `Transport.DialContext`. No free-port race, no readiness polling, and the process ends with its
  connection (idle connections close after 30 s). The `-L` draft below was dropped:
  - picks a free local port (`net.Listen("tcp", "127.0.0.1:0")`, close, reuse),
  - spawns `ssh -N -L 127.0.0.1:<port>:<service-host>:<service-port> <destination>`,
  - polls the local port until it accepts TCP (bounded by the profile timeout),
  - terminates the child on client close / process exit.
- The HTTP client keeps the real service URL; only `Transport.DialContext` is overridden to dial
  the local tunnel port. **TLS is still verified against the real hostname** and the Host header
  stays correct — no `verify_tls: false` workaround needed.
- Use the system `ssh` via `os/exec` (pattern: `internal/proc`), not `golang.org/x/crypto/ssh`:
  config parsing, known_hosts, agent and ProxyJump come for free and the Go code stays ~100 lines.
- Errors: `ssh_destination` on a native or `cli` profile is a config error, and so is a destination
  starting with `-` (ssh would read it as an option such as `-oProxyCommand=`). When ssh fails, the
  request error carries its stderr (for example `Permission denied (publickey)`).

## Phase 3 — SSH exec (`backend: "ssh-exec"`)

For services whose API *is* an SSH command (Gitea/Gerrit-style `ssh host cmd`, internal admin
endpoints reachable only as remote commands).

- Profile: `kind: "api"`, `backend: "ssh-exec"`, `auth: "none"`, `ssh_destination`, plus
  `commands` — an allowlist of permitted argv prefixes. An empty allowlist permits nothing.
- Guard: `config.NoTokenError` (the former `CLIManagedError`, now covering cli, ssh-exec and
  `auth: "none"`) — `auth set-token`, `auth logout` and `ResolveProfileSecret` refuse these
  profiles; the SSH key is the only identity.
- CLI: `workctl --profile NAME api exec -- CMD ARGS...`. Anything not matching a `commands`
  prefix is refused; every exec counts as a write under the policy unless the matched prefix is
  marked `read_only` in the profile.
- MCP: tool `api_exec`. *As built:* no extra MCP switch — the per-profile allowlist already is the
  opt-in, and non-`read_only` commands need `allow_write` exactly like other writes.
- *As built:* ssh joins its arguments into one command line that the remote side parses again, so
  "args as a vector" alone is not enough. Every word is single-quoted (`'x; rm -rf ~'` stays one
  argument). The doctor does not run any command for these profiles.

## Phase 4 — documentation and doc comments

- `docs/services-auth.md` (shipped with this plan) is the user-facing manual: supported services,
  auth methods, backends, examples. Update it in the same PR as any phase above.
- **Godoc discipline** (the Go equivalent of doxygen): every exported symbol gets a doc comment
  starting with the symbol name, as the existing code already does (`// NewProfileClient connects
  to one named profile, ...`). New rules for this plan:
  - every new exported type, function, method and field: doc comment in the canonical form,
  - every new package or file with a distinct role: a package/file comment like `api/cli.go`'s,
  - non-obvious invariants (identity rule, argv-leak avoidance, TLS-through-tunnel) are documented
    on the type that enforces them, not in commit messages,
  - verification: `go vet ./...` plus golangci-lint's `revive` `exported` rule, which CI already
    runs (`.golangci.yml`); browse with `go doc workctl/internal/api`.

## Order and risk

| Phase | Risk | Why this order |
| --- | --- | --- |
| 1 generic `api` kind | low — one switch case | immediate value, no new concepts |
| 2 ssh-tunnel | medium — child process lifecycle | composes with phase 1 and existing kinds |
| 3 ssh-exec | highest — remote exec surface | needs the guards proven in 1–2 |
| 4 docs | none | continuous, enforced per PR |

Azure and Atlassian modules stay untouched; they keep their own config and keyring entries.
Adapters for them are out of scope until duplication hurts in practice.
