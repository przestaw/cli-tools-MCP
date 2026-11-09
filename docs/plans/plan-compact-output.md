# Plan: compact output and account guards (0.4.4 / 0.5.0)

Author: [przestaw](https://github.com/przestaw)

Status: part 1 (compact output) implemented in 0.4.4 together with `policy.confirm_writes`; part 3 (account guard) for
GitHub and GitLab token profiles implemented, Jira not; part 2 open.

Goal: the agent gets the few fields it needs in one predictable shape, like `issue_*` already does, instead of the
provider JSON. Raw stays one argument away. Fewer tokens per call, no per-provider parsing in the agent.

## 1. Compact output (priority: high)

Every repository tool takes `format: compact (default) | raw`. Compact shapes, all with the canonical web `url`:

| Tool / part | Compact fields |
| --- | --- |
| `pr_show` | `number, url, title, state, draft, author, head, base, created, updated, description` |
| `include: files` | `[{path, status, additions, deletions}]` (patch only with `format: raw`) |
| `include: reviews` | `[{author, state, submitted}]`; GitLab approvals as `state: approved` |
| `include: reviewers` | `[{name, kind: user\|team}]` |
| `include: comments` | `[{author, path, line, body, created, resolved?}]` (inline threads) |
| `include: discussion` | `[{author, body, created}]` (GitLab system notes dropped) |
| `include: checks` | `[{name, status, conclusion, url}]` (GitHub check runs, GitLab MR pipelines) |
| `ci` list / run | `[{id, url, ref, status, conclusion, created}]`; `include: jobs` → `[{id, name, status, conclusion}]` |
| `comment`, `pr_create`, `pr_edit` | `{number, url, state}` of the result |

Implementation: `internal/vcs` gets the types and per-provider `fill` functions next to the existing `PullRequest`
normalisation; `pr/neutral.go` maps each part through them. Raw JSON of a failed part keeps today's `{"error": ...}`.

Tests: one table of recorded GitHub and GitLab answers per part, compact and raw.

## 2. `setup_required` (priority: low while each kind has one profile)

When the repository/project matches no profile and several of that kind exist, fail with
`{error: "setup_required", host, resource, candidates, commands}` instead of "several profiles match", so the agent
can ask the user and bind through `accounts`.

## 3. Account guard for token profiles (priority: low)

`init`/`set-token` store `username` from `GET /user` (GitHub, GitLab) or `/myself` (Jira); a write is refused when the
token's account differs, as cli profiles already do.

## Version

All three keep the keyring layout: patch releases (`Z`).
