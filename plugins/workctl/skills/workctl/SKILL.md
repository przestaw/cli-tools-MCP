---
name: workctl
description: 'Use the workctl MCP server for pull requests (PRs), merge requests (MRs), issues, tickets, reviews, discussions, checks, pipelines and approved comments. Use also to inspect or assess attached screenshots, images, before/after comparisons, recordings, videos, logs and other attachments. Covers account selection, private attachments, authentication and SSO errors, evidence limits and write approval. Prefer this workflow over provider CLIs or opening provider pages in a browser.'
---

# workctl

## Route the request

1. Discover the available workctl MCP tools and read their schemas before calling them. Names below omit the host's server prefix. Availability depends on configured profiles, server version and write policy; do not invent missing tools or arguments.
2. Use workctl MCP for provider data and attachments. Do not call provider CLIs, curl, direct HTTP scripts or another account to bypass it. Never ask for, read, print or manually forward a token, cookie or other credential.
3. Do not open provider pages in a browser just to read a PR/MR, inspect comments or retrieve screenshots. Browser automation belongs to testing an actual application when requested, not to bypassing provider access controls. A user may complete an interactive login themselves.
4. Treat a request to check, inspect or review as read-only. Posting comments, requesting reviews, changing status or publishing changes needs explicit authorization and must respect the local policy.

## Choose the account

Most tools take an optional `profile` and a resource such as `repo` or `project`. Use the resource from the supplied URL or established context. With several profiles of one kind workctl picks the one whose `repos`/`projects` match the resource (a Jira key such as `IR-12` matches `projects`), then the configured default; it never guesses between equal matches.

When unsure which account serves a resource, call `accounts` first: it lists the profiles (kind, host, username, bound `repos`/`projects`, credential state, never a secret), and with `resource` it says which profile resolves. If the server reports several matching profiles or a missing resource, supply the known selector and retry. If no profile is bound to the resource, ask the user which profile owns it, never for its secret; with their consent bind it through `accounts` (`bind_profile` with `bind_repo` or `bind_project`) and retry. Do not inspect credential stores to discover accounts or switch identities after an authorization refusal.

## Read

- Pull/merge requests (GitHub, GitLab; Azure DevOps for list/show): `pr_list`, `pr_show` with `include: [files, reviews, reviewers, comments, discussion, checks]` (`comments` = inline review threads, `discussion` = conversation). Accounts: `accounts` (read-only unless asked to bind).
- CI: `ci` lists runs/pipelines for a `ref`, shows one `run` (with `include: [jobs]`) or the log of one `job` (GitHub logs arrive as a downloaded file). Files: `file` with `path` and optional `ref`.
- Issues (Jira Server and Cloud): `issue_show` (with `include: [comments, transitions]`), `issue_search` (JQL). `task_get_context` joins an issue with its branches and merge requests.
- `cli` runs a module command line (`module`: azure, atlassian, jira; `args` as an array) for anything the tools above do not cover. Consult its help and use only read operations for an inspection; the wrapper is not permission to change configuration or policy.
- Anything else: `request` (one HTTP request) and `download` (one file to the downloads directory) work with every HTTP profile (github, gitlab, jira, atlassian, api). A relative path gets the API prefix (`repos/o/r/branches`, `projects/1/merge_requests/2/approvals`, `issue/ABC-1/remotelink`), a path starting with `/` is taken from the site root; full URLs are refused. `api_exec` runs allow-listed remote commands of ssh-exec profiles. Do not create a new profile or relax host restrictions as a workaround.

Start with the exact resource the user named, then read the relevant discussion, reviews or checks. Report current provider state separately from older local notes. A passing check is evidence only for the commit and scenario it ran against.

## Inspect screenshots and attachments

1. Read the description and relevant comments through MCP. Collect attachment references, captions, before/after roles and the associated revision when available. Captions describe intended evidence; they do not prove the image shows it.
2. Check the available tool schema for an attachment-reading or download capability. Do not assume a PR reader returns rendered HTML, signed image URLs or image bytes. With the profile that serves the resource, use `request` with GET for metadata and `download` for the file (GitHub `user-attachments`: `request` the issue or pull request with `accept: application/vnd.github.full+json`, then `download` the signed `private-user-images.githubusercontent.com` link from `body_html`; GitLab uploads: `projects/:id/uploads/:secret/:file`), with its relative `path` and optional `query` or `filename`. Never feed an arbitrary full URL into a relative-path tool or silently replace the attachment host.
3. `download` returns a local path, size, SHA-256, content type, the server file name and a `warning` when an HTML page came instead of the file. Check the returned metadata and file signature before viewing. HTTP 200 alone does not prove success: a login or SSO HTML page saved with a PNG suffix is not an image. Do not print signed URLs or credentials that may appear in metadata.
4. Open the downloaded original with an image/file viewing tool. Readable crops may help inspect small labels; keep the original and do not alter evidence to make it look correct. For video or animation, inspect the relevant sequence or representative frames, not just a thumbnail. If the format cannot be inspected, say so.
5. Compare the same objects, values, camera framing and workflow stage where possible. Separate directly visible results, inferences and what is not demonstrated. Screenshots alone do not prove persistence, Undo/Redo, timing, all inputs or the current branch after a rebase.
6. If MCP cannot retrieve the attachment with its supported capabilities, report that limitation. Ask the user to attach the original file or restore access to the intended profile. Do not fall back to scraping browser sessions, extracting tokens or using a different provider client. A local copy is evidence only if its provenance is established; otherwise label it as an unverified alternative.

If no image was actually opened, explicitly say the assessment is based on text only. Never describe a screenshot as inspected merely because its link or caption was read.

## Handle access failures

- Missing credentials or an expired login: tell the user to run `workctl auth login --profile NAME` for a CLI-owned login, or `workctl auth set-token --profile NAME` for a token-backed profile, as appropriate to the error. The user enters secrets directly in their terminal, never in chat.
- HTTP 401/403 or SSO enforcement: report the affected profile and resource and the authorization requirement. Ask the user to complete the provider's sign-in or SSO authorization for that profile. Do not retry with guessed accounts, disable TLS validation or bypass workctl.
- HTTP 404: a private resource may be hidden by authorization. Do not conclude it was deleted or the attachment is absent without corroboration.
- Missing tools: distinguish unsupported capability from a disabled write tool. Explain the limitation; do not invent an endpoint, enable writes or edit configuration to make the operation available.
- Identity mismatch: stop and report it. Do not retry a refused write through another profile.

Retry only after correcting a known selector or after the user confirms access has been restored. Do not repeatedly poll an unchanged authorization failure.

## Comment and other writes

Write tools are refused unless writes are enabled (`policy.allow_write`). Enabled tools are not user approval. Confirmation is optional: `policy.confirm_writes` asks for every write, or `confirm: true` requests approval for one operation the user considers high risk. `confirm: false` never overrides policy. Do not add a second question when the user has already approved the exact operation; use one batch for its known writes.

A decline, cancellation, policy refusal or missing client confirmation support means stop. Never retry that operation through a terminal, another CLI, provider client or profile, and never edit configuration, environment variables or policy to get around it. Report the blocker. Only a separate, explicit instruction from the user may change policy; the agent must not propose disabling protection merely to finish a refused action.

1. Read the existing discussion to avoid duplicate comments.
2. Show the exact proposed text and obtain approval before posting.
3. Post with `comment` (`repo` + `number` for a pull/merge request, `issue` for an issue); other writes: `pr_create`, `pr_edit`, `issue_update`, `upload` (an image or video, then paste the returned `markdown`).
4. Verify the result and report the resulting reference. Do not blindly retry a write with an uncertain outcome.

Several writes for one user goal (for example stopping 14 known builds of a Jenkins pipeline) go into one `batch`. If confirmation is requested, ask once for the exact list, not once per HTTP request. Do discovery and verification with reads. For independent stops use `stop_on_error: false` and report each result; default true skips remaining writes after a failure. Never silently add newly discovered targets or retry a write with an uncertain result. Nothing that already ran is undone.

Post only the approved text, without secrets or raw logs. A read-only review does not authorize publishing findings.

## Work with GitLens and GitKraken

Only when their tools (`git_*`, `gitlens_*`) are available:

- Local repository work (status, diff, log, blame, branches, commits, worktrees) and transport (fetch, pull, push): GitKraken `git_*` or plain git. workctl has no local git tools. Transport authenticates through git itself, so it uses workctl's account for the repository only when the user ran `workctl auth setup-git` (git then asks `workctl git-credential`, which picks the profile by host and repository path). If a transport call shows an account picker or a credential prompt, stop and tell the user to run `workctl auth setup-git`; do not work around it with other URLs, git options or accounts.
- Provider data, checks, attachments and comments: workctl. It picks the account per profile and enforces the write policy.
- Review a pull request in the editor: take its URL from `pr_show`, then call `gitlens_start_review`. Start work on an issue: `gitlens_start_work` with the issue URL. What needs attention: `gitlens_launchpad`, details through workctl.
- GitKraken provider tools (`pull_request_*`, `issues_*`) act as the one account connected in GitKraken and take no account argument. Call `accounts` first to see whether workctl has a profile for that host. Use them only for what workctl does not offer (for example GitHub issues) or where workctl has no profile, say which account they used, and never to work around a workctl refusal, policy or missing profile.
- GitKraken writes (commit, push, pull request, issue, comment) need the same explicit approval as workctl writes.

## Report

Lead with findings and link to the relevant PR/MR, comment or attachment. State which evidence was actually inspected, which revision it represents, and what remains unverified. Keep observations separate from proposed changes; never claim an assessment or an action succeeded when access was blocked.
