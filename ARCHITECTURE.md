# Architecture

Author: [przestaw](https://github.com/przestaw)

`workctl` is a single Go binary that talks to Azure DevOps, Jira, Confluence, GitLab and GitHub.
It has two front ends that share one code base:

- **CLI** – `workctl github pr get 42`, `workctl jira issue ABC-1`, ...
- **MCP server** – `workctl mcp` exposes the same operations as tools for AI agents (JSON-RPC over stdio).

Everything is **read-only** unless the config sets `policy.allow_write = true`.

## Layout

```
cmd/workctl/        entry point, one line: calls app.Main
internal/
  app/              wires everything together: command dispatch, doctor, task, MCP tool list
  config/           config file, profiles, write policy, credential lookup (env var, then OS keyring)
  api/              HTTP client for one profile, plus the gh/glab transport ("cli" backend) and the ssh dialer ("ssh-tunnel")
  mcp/              MCP protocol: JSON-RPC loop, Tool type, schema and argument helpers
  github/ gitlab/ jira/   REST providers: API calls, CLI commands, MCP tools
  generic/          kind "api" profiles: `api raw` / `api_request` over HTTP, `api exec` / `api_exec` over ssh
  pr/               provider-neutral `pr` / `mr` command and pr_list / pr_show tools
  vcs/              provider-neutral pull request model and JSON normalisation
  modules/          embeds the azure and atlassian CLIs and decides which of their commands may run
  azure/ atlassian/ the two embedded CLIs (cobra command trees, each with its own config)
  auth/ setup/      `auth ...` commands, git credential helper, `init` wizards
  cliutil/ proc/ httpx/ secret/ render/ output/ prompt/ clikit/   small shared helpers
  testutil/         fake gh/glab and child processes for tests
```

## Dependency rule

Dependencies point one way only. Only `app` knows all providers; nothing imports `app`.

```
cmd/workctl -> app -> github, gitlab, jira, generic, pr, modules, auth, setup
                      -> api, mcp, vcs -> config, cliutil, httpx, secret, proc
```

Providers depend on `api`, `config` and `mcp` (for the `Tool` type). `mcp` and `config` depend on no provider.

## Request flow

**CLI:** `app.run` parses `--config` / `--profile`, loads the config and calls the provider's `Command`.
The provider builds an `api.Client` from the profile, sends the request and prints the JSON.

**MCP:** `mcp.Serve` reads JSON-RPC from stdin and calls `app.callMCPTool`, which checks that the backend is
configured and hands over to the provider's `CallTool`. Write tools call `cfg.RequireWrite()` first. Output is
truncated to `mcp.max_output_bytes`.

## Two kinds of provider

| | GitHub, GitLab, Jira | Azure DevOps, Atlassian |
|---|---|---|
| Code | small packages (`flag` + `switch`) | full cobra CLIs, embedded |
| Config | shared `config.json` profiles | own config files and keyring entries |
| From MCP | called in-process | run as a child process (`modules.RunProcess`) |
| MCP tools | one per operation | one per module, arguments as an array |

The embedded CLIs started as separate tools. They keep their own configs so existing setups still work. Running
them as child processes keeps cobra's global state out of the long-lived MCP server and gives each call its own
timeout.

## Design decisions

- **One table, two front ends.** `github.prReads` maps a CLI action to its MCP tool and read function, so the
  CLI and MCP cannot drift apart. A test checks that both send identical requests.
- **Provider-neutral pull requests.** `vcs.PullRequestReader` is a small interface with one adapter per
  provider. Adding a provider means writing an adapter and one `case` in `pr.newReader`.
- **`mcp` is generic.** `mcp.Server` receives `Tools` and `Call` functions instead of importing providers.
- **Clients are created per call.** A missing login breaks only that provider's tools.
- **Reserved names.** `bitbucket` and `asana` print "not implemented yet" so the command line stays stable.

## Safety rules (covered by tests)

- No write reaches a server, `gh`/`glab` or a child process while `allow_write` is off.
- For embedded CLIs a command is a write unless it is on the read list in `modules`. A test compares that list
  with the real command trees.
- The MCP surface additionally refuses credential and profile commands, the interactive shell, `open` and the
  `--config` / `--insecure` flags.
- `api.Client` rejects absolute URLs and cross-host redirects, so a token cannot be sent to another host.
- With the `cli` backend a write is refused when `gh`/`glab` is logged in as a different account than the profile.
- One identity per layer: an SSH key authenticates the ssh transport, a workctl token the HTTP request. A token never
  reaches the ssh command line, and profiles owned by gh/glab or an SSH key have no workctl token (`config.NoTokenError`).
- `ssh-exec` runs only allow-listed command prefixes, quotes every word for the remote shell, and refuses
  `ssh_destination` values that ssh would read as an option.
- Tokens are never written to the config file.

## Tests and CI

```
go build ./... && go vet ./... && go test ./...
golangci-lint run ./...
```

Tests use `httptest` servers and fake child processes; they need no network and no real `gh`/`glab`.
CI (`.github/workflows/ci.yml`) runs build, vet, tests (with `-race` on Linux) and golangci-lint. The lint config
also checks `gofmt` formatting and requires a doc comment on every exported symbol.
`internal/app/plugin_test.go` keeps the agent skill and plugin manifests in sync with the code.
