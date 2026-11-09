# Plan: one account per owner, a consolidated toolset, GitKraken/GitLens routing

Author: [przestaw](https://github.com/przestaw)

Status: implemented (2026-11-09) with these differences: accounts bind repos/projects patterns instead of `owners`;
`pr_show` keeps `id` next to `number`; GitHub `user-attachments` go through `request` with `accept`
`application/vnd.github.full+json` and `download` of the signed link from `body_html`. The hidden aliases were removed
in 0.4.3. See also plan-issue-layer.md.

Goal: workctl is the agent's **provider data plane** (PRs, issues, CI, attachments, comments) with one account per
owner. It is deliberately not the agent's only tool: local git, GitKraken MCP and GitLens handle the working tree and
the editor, and the skill routes between them. An agent never needs `gh`, `glab`, `curl`, a browser or a Git
Credential Manager prompt for provider data, and always acts as the right account.

## Identity rule (extends plan-generic-api-ssh.md)

**One account per (host, owner).** A GitHub owner / GitLab group is bound to exactly one profile. The binding drives
tool-call profile selection, the git credential hint, and the account a write must be made as. An unbound owner is a
setup step (`setup_required`), never a guess, never a silent `defaults.<kind>` fallback.

## Target toolset

Fewer tools than today (today: 18 read + 4 write + 2 wrappers with github, azure and atlassian profiles; 44 with every kind
configured). Target: **10 + wrappers**. One description per task, so the agent cannot pick between near-duplicates;
everything rarer goes through `request` with a skill cheat sheet.

| Tool | Arguments (new schema) | Replaces |
| --- | --- | --- |
| `accounts` | optional `bind {profile, owner, replace?, git_hint_dir?}`; without it: read-only list | — (new) |
| `pr_list` | `repo`, `state`, `search`, `profile?` | `github_list_pull_requests`, `gitlab_list_merge_requests` |
| `pr_show` | `number`, `repo`, `include?: [files, reviews, comments, discussion, checks]`, `profile?` | `github_get_pull_request{,_files,_reviews,_reviewers,_comments,_discussion,_checks}`, `gitlab_get_merge_request{,_changes,_notes}` |
| `ci` | `repo`, `ref?` or `run?`, `include?: [jobs, log]`, `profile?` | `github_{list,get}_workflow_run{s,_jobs}`, `gitlab_{list_pipelines,get_pipeline,get_pipeline_jobs,get_job_trace}` |
| `file` | `repo`, `path`, `ref?`, `profile?` | `github_get_file`, `gitlab_get_file` |
| `request` | `method?`, `path`, `query?`, `body?`, `profile?` — **any HTTP profile**; GET/HEAD free, else `allow_write` | `api_request`; repo/branch metadata, issues, diffs, approvals, anything provider-specific |
| `download` | `path` or provider attachment URL, `query?`, `filename?`, `profile?` | `api_download`; attachment/log/artifact download for every provider |
| `comment` | `repo`, `number` or `issue`, `body`, `profile?` (write) | `github_add_pull_request_comment`, `gitlab_add_merge_request_note`, `jira_add_comment` |
| `pr_create` | `repo`, `head`, `base`, `title`, `body?`, `draft?`, `reviewers?`, `profile?` (write) | `github_create_pull_request`, `gitlab_create_merge_request` |
| `pr_edit` | `repo`, `number`, `title?`, `body?`, `state?`, `base?`, `add_reviewers?`, `remove_reviewers?`, `profile?` (write) | `github_edit_pull_request`, `github_request_reviewers` |
| `azure`, `atlassian`, `task_get_context`, `api_exec` | unchanged | — |

Jira read tools (`jira_get_issue`, `jira_search`, `jira_get_comments`, `jira_get_transitions`) stay: Jira Server has no
other surface and they do not overlap with `pr_*`.

Consolidation rules:

- Provider picked by the resolved profile's kind; vocabulary translated per provider: `number` everywhere (`iid` dies
  with the old tools), `state` = `open|closed|merged|all`.
- Write tools are always listed; without `policy.allow_write` they fail with the policy message, so the agent sees
  "disabled", not "missing".
- Compact default output including the canonical web URL (GitLens hand-offs need it); `format: "raw"` returns the
  provider JSON.
- Old `github_*`/`gitlab_*` names stay for one release as hidden aliases (callable, not listed), then go.

## Stage 1 — accounts and profile resolution (code)

1. `config.Profile`: add `Owners []string` (GitHub owners / GitLab groups, case-insensitive; `Validate` enforces one
   profile per (host, owner)).
2. Resolution for every repo-taking tool, replacing the kind-only `Scoped`/`ProfileOf` checks: explicit `profile` →
   exact `repos`/`default_repo`/`projects` match → `owners` match → `defaults.<kind>` only when the owner is unbound
   and no repo was given → error `setup_required` with `{host, owner, candidates, commands}` in the body.
3. `accounts` tool. Read-only call: profiles with name, kind, host, `username`, `owners`, repos, backend, `read_only`,
   credential state (`present`/`missing`/`cli: logged in as X`); never a secret. With `bind`: changes only `owners`
   (and with `git_hint_dir` writes `credential.https://HOST.username` into that one repo's local git config); refuses
   to steal an owner without `replace: true`; cannot create profiles or touch auth, URL, backend, policy or defaults.
4. Native token profiles get `username` bound at `init` via `GET /user`; a write under another login is refused (same
   rule as the cli backend today).
5. Example: a personal and a work account on the same host:

```jsonc
"profiles": {
  "personal": { "kind": "github", "url": "https://api.github.com", "auth": "bearer",
                "username": "przestaw", "owners": ["przestaw"], "verify_tls": true },
  "work":     { "kind": "github", "url": "https://api.github.com", "auth": "bearer",
                "username": "alice-work", "owners": ["acme-corp"], "verify_tls": true }
},
"defaults": { "github": "work" }
```

Usage: first call for an unbound owner fails with `setup_required` → the agent calls `accounts`, asks the user which
account owns that owner (candidates + "new account") → existing profile: `accounts {bind}` and retry; new account: the
user runs `workctl github init --profile NAME` in their own terminal (the token never passes through the agent), then
the agent binds. With consent, `git_hint_dir` also silences the GCM account picker for that clone.

## Stage 2 — tool consolidation (code)

- `pr_show {include}`, `ci`, `file`, `comment`, `pr_create`, `pr_edit` over the existing provider functions: the
  internals in `internal/github` and `internal/gitlab` stay, only the MCP surface shrinks.
- `request` = `api_request` widened to every HTTP profile; `api_exec` and ssh-exec profiles unchanged.
- Old names kept as hidden aliases for one release.
- Server `Instructions` rewritten: name `accounts`, the setup flow, "prefer workctl for provider data".
- Atlassian `attachment download`/`page attachment-download` reclassified read-only when writing to the downloads dir.

## Stage 3 — downloads (code)

- `download` for every profile: streaming, SHA-256, unique name in the downloads dir (all already in
  `internal/generic/download.go`).
- Signed redirects followed to an allow-list (`*.githubusercontent.com`, Actions blob storage); the credential is never
  forwarded cross-host; HTML-instead-of-binary is flagged in the result.
- GitHub `user-attachments` resolved via `body_html` (Accept `application/vnd.github.full+json`) when a PAT cannot
  fetch them directly (open question); GitLab uploads via `/projects/:id/uploads/...` (17.4+).
- cli profiles: `gh api`/`glab api` stdout written straight to the file (drop the blanket refusal in `Stream`).

## Stage 4 — skill: routing between workctl, git, GitKraken and GitLens

workctl is one of several git tools the agent holds; the skill's routing table says which does what. GitKraken MCP is
bundled with GitLens 19.5; its provider tools use the single connected integration account (no account argument), its
git tools run local git and therefore hit GCM.

| Task | Tool |
| --- | --- |
| Working tree: status, diff, log, blame, branch, stash, worktree | GitKraken `git_*` or plain git — not workctl |
| fetch / pull / push | GitKraken `git_*` or git; needs the stage-1 git hint, else the GCM picker appears |
| PRs, MRs, issues, reviews, checks, CI logs, attachments, comments | **workctl** (account per owner, policy, downloads) |
| Open a PR for review in the editor | `gitlens_start_review {pr_url}` — URL from workctl `pr_show` |
| Start work on an issue (branch + link) | `gitlens_start_work {issue_url}` — URL from workctl |
| "What needs attention" overview | `gitlens_launchpad`, details via workctl |
| GitKraken provider tools (`pull_request_*`, `issues_*`) | only when workctl has no profile for that host, with the user's consent (their account is unknown to workctl) |

Skill additions: this table, the stage-1 first-call account flow, a `request`/`download` cheat sheet (issues, diff,
approvals, job logs per provider), and the existing download-verification rules unchanged.

## Stage 5 — install and host configs

When the `claude`/`codex` CLIs are not on PATH (VS Code extensions only), the installer (`scripts/install.py`, one
script for every OS) uses the copies bundled with the extensions and stays idempotent:

| Host | MCP registration | Skill |
| --- | --- | --- |
| VS Code Copilot | `%APPDATA%\Code\User\mcp.json` + every `profiles\*\mcp.json`, Insiders when present | `~/.copilot/skills/workctl` |
| Claude Code | `~/.claude.json` → `mcpServers.workctl = {command, args: ["mcp"]}` (user scope) | `~/.claude/skills/workctl` |
| Codex | `~/.codex/config.toml` → `[mcp_servers.workctl] command=... args=["mcp"]` | `~/.codex/skills/workctl` |

Never overwrite a foreign server named `workctl` without `-Force`. GCM hints, once per machine and per bound owner:
`git config --global url."https://USER@github.com/OWNER/".insteadOf "https://github.com/OWNER/"`.

## Dropped from revision 1 (and why)

- `directory`/`url` tool arguments — the agent knows the repo from `git remote` and can parse a PR URL itself.
- MCP elicitation — a large `internal/mcp` change with unverified client support; `setup_required` covers the flow.
- `doctor --fix-git-hints` and new doctor checks — two lines of git config, set once.
- Dedicated issue/diff/log tools — `request` plus the cheat sheet.
- Argument aliases (`iid`) — pointless once the old tools are gone.
- workctl wrapping `gk` — a third identity owner, against the identity rule.

## Commit order and verification

1. Copyright headers (ready, uncommitted). Review and commit the SKILL.md rewrite separately.
2. Stage 1 (+ tests: resolution order, ambiguity, `setup_required`, bind/replace/git_hint).
3. Stage 2 (+ tests: include fan-out, state mapping, write-disabled errors, alias dispatch).
4. Stage 3 (+ tests: redirect allow-list, no credential cross-host, cli streaming; live: one private screenshot and one
   Actions log via profile `przestaw` — the user creates the profile first).
5. Stages 4-5 (skill, installer, host configs); install on this PC for Copilot, Claude Code and Codex.
6. One release later: delete the hidden aliases.

Each step: `gofmt -l . ; go vet ./... ; go test ./...`.

## Open questions

- Does a PAT fetch `github.com/user-attachments/assets/...` directly, or only the signed URL from `body_html`?
- Which account is connected to the GitKraken GitHub integration (manual check in GitLens).
- `pr_list`/`pr_show` keep serving Azure DevOps PRs through `internal/vcs` as today — confirm nothing in `ci`/`file`
  needs an Azure arm, or route those through the `azure` wrapper.
