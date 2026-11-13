# workctl

**English** · [Polski](https://github.com/przestaw/cli-tools-MCP/blob/main/README.pl.md)

[![CI](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml/badge.svg)](https://github.com/przestaw/cli-tools-MCP/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8)](https://go.dev/dl/)
[![License](https://img.shields.io/badge/license-BSD--4--Clause-blue)](https://github.com/przestaw/cli-tools-MCP/blob/main/LICENSE)

**Connect your services to coding agents through APIs — one CLI and MCP server.**

workctl gives local coding assistants controlled access to services through configurable HTTP APIs and
allow-listed SSH commands. Use it wherever your workflow needs service data or actions: development,
monitoring, CI/CD, packages, deployment, administration, or your own internal tools.

Named integrations provide convenient commands, not a closed list of compatible services.
Writes are disabled by default; profiles keep destinations and credentials explicit, and optional approvals
let you review changes individually or as a batch.

[Quick start](#quick-start) · [MCP configuration](#connect-an-mcp-client) · [Write approvals](#write-approvals) · [Documentation](#documentation)

## What it provides

- **One Go executable:** a command-line client and a stdio [Model Context Protocol](https://modelcontextprotocol.io/) server.
- **Compact answers:** shared PR/issue shapes; reviews, files, discussions and checks fetched when requested. Raw output remains available.
- **Explicit write policy:** writes are disabled until enabled; optional per-operation, batch or connection-scoped approvals.
- **Account routing:** profiles selected by repository/project, explicit name or default. Native GitHub/GitLab account binding checks tokens before writes.
- **Credentials outside source control:** OS keyring storage; explicit environment-variable and provider-CLI backends for other setups.
- **Agent integrations:** an installer and reusable skill for VS Code, Claude Code and Codex.
- **Operations beyond source control:** query monitoring APIs, inspect deployment status, and call management endpoints through configured service profiles.

## Two ways to connect a service

| Route | When to use it |
|---|---|
| Configured HTTP API or allow-listed SSH commands | Connect an existing service without adding a product-specific connector. Supply its address, supported authentication and the operations your workflow needs. |
| Built-in provider commands | Use convenient commands and common response formats where available, including GitHub, GitLab, Jira, Confluence and Azure DevOps. |

Compatibility depends on the service's protocol, authentication and endpoint semantics — not its brand being
listed here. Generic access does not imply a turnkey integration or a local client for every product.
Tools are advertised according to configured backends.

**Examples, not a support ceiling:** Jenkins and Conan services have been used successfully by the maintainer
through configured API profiles. The same approach applies to compatible monitoring, deployment, management
and custom service APIs; each requires its own configuration.
Conan server access is not a replacement for the Conan CLI's local package resolution or build workflow.

### Arbitrary APIs, monitoring and operations

The `request` tool is not limited to built-in integrations. Configure a `kind: "api"` profile with the service URL
and authentication, then supply the endpoint, HTTP method, query parameters and body required by that service.
Absolute request URLs are refused: the profile fixes the destination and its credential.

For example, after configuring a `monitoring` profile for a Prometheus-compatible API, call `request` with:

```json
{
  "profile": "monitoring",
  "method": "GET",
  "path": "/api/v1/query",
  "query": {"query": "up"}
}
```

The same mechanism can inspect deployment services, trigger a Jenkins build, query an artifact repository, or
operate a management console. These are configurable API workflows, not claims of a dedicated connector for
every product. You supply the endpoint semantics and permissions; non-read requests require `allow_write` and
the configured approval mode. Use `read_only: true` for monitoring profiles that should never write.

See [service profiles and authentication](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/services-auth.md)
for bearer/basic/custom-header authentication, form login and SSH-backed access.

## Quick start

Requires **Go 1.26+**, **Python 3** for the installer, Git, and access to the services you configure.
Use `python3` instead of `python` where required by your environment.

```sh
git clone https://github.com/przestaw/cli-tools-MCP.git
cd cli-tools-MCP
python scripts/install.py --vscode
workctl github init --profile personal
workctl auth status
```

Choose `--claude`, `--codex`, or `--all` instead of `--vscode` for other integrations.
Without an integration flag, the installer builds the executable without registering an MCP client.
It installs into `~/.local/bin`, may update the user PATH, and attempts migration of older keyring entries.
Restart the MCP server through the client that owns it after installation or upgrades.

Configure other services with `workctl init`, or only the provider you need.
Tokens are entered in the setup prompt, not supplied on the command line.
Keep writes disabled while checking the account and repository selection:

```sh
workctl --profile personal github pr list --repo owner/repository
workctl --profile personal github pr get 42 --repo owner/repository
```

## Connect an MCP client

Any compatible stdio MCP client can start `workctl mcp`. A typical configuration is:

```json
{
  "mcpServers": {
    "workctl": {
      "command": "workctl",
      "args": ["mcp"]
    }
  }
}
```

Use the executable's absolute path if it is not on the client's PATH. VS Code uses a `servers` object and
`type: "stdio"` instead; the installer handles that format. Host configuration and approval UI vary:
see the [MCP guide](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/mcp.md).
The MCP configuration above contains no credentials.

An agent can call `pr_show` with these arguments to inspect a PR without changing it:

```json
{
  "profile": "personal",
  "repo": "owner/repository",
  "number": 42,
  "include": ["reviews", "checks"]
}
```

## Write approvals

In the `policy` section of your workctl configuration, choose the scope deliberately:

| Setting | Behavior |
|---|---|
| `allow_write: false` | Default: refuse writes. |
| `allow_write: true` | Enable writes; by itself this does **not** request confirmation. |
| `confirm_writes: true` | Ask for each write, or once for the exact operations in a batch. |
| `confirm_session_writes: true` | Ask on the first write; approval covers subsequent writes across configured profiles in that MCP connection. |

Both confirmation modes default to off and cannot be enabled together. For a cautious setup, merge this fragment
into your existing configuration; retain its profiles and other settings:

```json
{
  "policy": {
    "allow_write": true,
    "confirm_writes": true
  }
}
```

For a known group of operations, use `batch` with a short explanation for each item:

```json
{
  "calls": [
    {
      "tool": "pr_edit",
      "description": "Rename PR #42 to describe the pagination fix",
      "args": {
        "profile": "personal",
        "repo": "owner/repository",
        "number": 42,
        "title": "Handle exact pagination boundaries"
      }
    }
  ],
  "confirm": true,
  "stop_on_error": true
}
```

A batch accepts 1–20 writes. All items are checked locally before execution; remote failures can still leave
a partially completed batch. Earlier writes are not rolled back. Descriptions help explain intent, but the
displayed request details remain the basis for approval; long values are shortened.

`confirm: true` requests detailed approval even after a session approval. When confirmation is required but
the MCP client cannot display it, workctl refuses the write. A cancelled call must not execute. Session approval
lasts until the MCP connection ends, which need not coincide with a chat turn or conversation.

**Boundary:** these prompts govern MCP calls. Direct terminal commands are not prompted this way, and workctl
cannot stop an agent from using an unrelated shell, `gh` or `curl`. Restrict those tools in the host if necessary.
After a refusal, the agent must stop rather than switch transport, account or policy.

## Tools at a glance

| Purpose | MCP tools |
|---|---|
| Accounts and routing | `accounts` |
| Pull/merge requests | `pr_list`, `pr_show`, `pr_create`, `pr_edit` |
| Tickets | `issue_show`, `issue_search`, `issue_update`, `task_get_context` |
| CI and source files | `ci`, `file` |
| Comments and media | `comment`, `upload`, `download` |
| Grouped writes | `batch` |
| Other provider operations | `request`, `api_exec`, `cli` |

Use least-privilege provider credentials. Keep tokens out of prompts and repository files.
The [setup guide](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/setup.md) documents profile selection,
keyring storage and explicit environment overrides.

## Documentation

| Topic | Guide |
|---|---|
| CLI usage and policy | [CLI reference](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/cli.md) |
| Client registration, tools and approvals | [MCP guide](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/mcp.md) |
| Profiles and credentials | [Setup](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/setup.md) |
| Generic APIs, SSH and form login | [Other services](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/services-auth.md) |
| Configuration fields | [Example config](https://github.com/przestaw/cli-tools-MCP/blob/main/config.example.json) |
| Code organization | [Architecture](https://github.com/przestaw/cli-tools-MCP/blob/main/ARCHITECTURE.md) |

## Development and contributions

Report reproducible problems or propose a focused change through
[GitHub issues](https://github.com/przestaw/cli-tools-MCP/issues).
Include the workctl/client version, selected provider and sanitized reproduction steps.
Never attach tokens, cookies, account dumps or private customer payloads.

```sh
go build ./...
go vet ./...
go test ./...
golangci-lint run
```

CI checks Windows, Linux (including race tests) and lint.
See [development notes](https://github.com/przestaw/cli-tools-MCP/blob/main/docs/development.md) for builds and versioning.

## License

[BSD 4-Clause](https://github.com/przestaw/cli-tools-MCP/blob/main/LICENSE).
Copyright © 2026 [przestaw](https://github.com/przestaw).
Redistribution and advertising acknowledgements are described in the license and
[NOTICE](https://github.com/przestaw/cli-tools-MCP/blob/main/NOTICE).
