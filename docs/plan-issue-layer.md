# Plan: provider-neutral issues, one module wrapper, per-kind timeouts

Author: [przestaw](https://github.com/przestaw)

Status: proposal (2026-11-09). Extends [plan-agent-accounts.md](plan-agent-accounts.md) (which consolidates `pr_*`, `ci`,
`file`, `comment`, `request`, `download`). Stages 1 and 2 implemented; stage 3 open.

Goal: the same rule for issues as for pull requests — **one operation, one tool, one output shape; the provider is a
detail of the resolved profile**. Jira Server, Jira Cloud and later Asana or Azure Boards answer the same questions
(get, search, comments, transitions, transition, edit, link); only the path differs. Provider-specific leftovers go
through one wrapper instead of one wrapper per module.

Revises plan-agent-accounts.md:

- "Jira read tools stay" is dropped: `jira_*` duplicates `workctl jira ...` (same `GetIssue`, same client) and would
  duplicate `issue_*`.
- "`azure`, `atlassian` unchanged" is dropped: they become one `cli` tool.

## Target toolset (delta)

| Tool | Arguments | Replaces |
| --- | --- | --- |
| `issue_show` | `key`, `include?: [comments, transitions, links]`, `profile?` | `jira_get_issue`, `jira_get_comments`, `jira_get_transitions`, `atlassian issue get/transitions` |
| `issue_search` | `query` (JQL for Jira), `start_at?`, `max_results?`, `profile?` | `jira_search`, `atlassian jql` |
| `issue_update` (write) | `key`, `transition?` (id or name), `fields?` (resolution, fix_versions, assignee, …), `link?: {type, key}`, `profile?` | none today (Jira Server had no write besides comments) |
| `comment` | unchanged from plan-agent-accounts.md (`issue` covers Jira) | `jira_add_comment` |
| `cli` | `module: azure \| atlassian \| jira \| gitlab \| github`, `args: []` | `azure`, `atlassian` |

`task_get_context` and `api_*` / `request` / `download` stay. Net effect on top of plan-agent-accounts.md: −5 Jira
tools, −2 wrappers, +3 issue tools, +1 `cli`.

Rules (same as for `pr_*`):

- Provider picked by the resolved profile's kind (`jira` = Server/DC `/rest/api/2`, `atlassian` = Cloud `/rest/api/3`).
- Compact common output: `key, url, title, status, resolution, type, assignee, reporter, fix_versions, labels, links,
  updated`; `include` adds `comments` / `transitions`; `format: "raw"` returns the provider JSON.
- `issue_update` applies transition + fields in one call when the provider allows it (Jira: `fields` in the transition
  body); a transition not available to the account fails with the list of available ones.
- Write tools always listed; without `policy.allow_write` they fail with the policy message.
- Old `jira_*`, `azure`, `atlassian` names: hidden aliases for one release, then removed.

CLI mirrors MCP: `workctl issue show|search|update ...` next to `workctl pr ...`; `workctl <module> ...` stays the escape
hatch and is what `cli` runs.

## Stage 1 — per-kind timeouts (done)

`timeouts.<kind>` between `profiles.<name>.timeout_seconds` and the global `timeout_seconds`; CLI commands use the
selected profile's timeout instead of the global one; `doctor` the longest, `task` 3× the longer of jira/gitlab;
modules: `timeouts.<module>` → `modules.timeout_seconds` → 120 s. Old configs unchanged.

## Stage 2 — `cli` wrapper (done)

1. `modules.Tools`: one tool `cli {module, args}`; module list from `cliModules`.
2. Add `jira` to `cliModules` (no cobra tree: a small `resolve` over its subcommands). Read set: `issue`, `search`,
   `comments`, `transitions`, `myself`, `server-info`, `raw GET|HEAD`; everything else is a write.
3. `gitlab` / `github` CLIs join the same way when needed.
4. Hidden aliases `azure`, `atlassian`; skill and README updated.

## Stage 3 — `issue_*`

1. `internal/issue`: `Reader`/`Writer` interface + normalised `Issue` type (like `vcs` for PRs).
2. Backends: `jira` (existing `internal/jira` functions plus `PUT /issue/{key}`, `POST /issue/{key}/transitions` with
   `fields`, `POST /issueLink`), `atlassian` (existing `internal/atlassian/client`: `UpdateIssue`, `DoTransition`, links).
3. `workctl issue ...` CLI and the three MCP tools; `jira_*` become hidden aliases.
4. Tests: one shared table of cases run against both backends with recorded HTTP.

## Order

0. Rebase `dev` on `origin/dev` (local 5b60ee9 + uncommitted work), split commits: form-login logout, stage 1.
1. Stage 1 commit.
2. Stage 2 commit.
3. Stage 3 commit(s); then remove aliases in the following release.
