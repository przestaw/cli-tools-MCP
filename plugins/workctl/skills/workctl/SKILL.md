---
name: workctl
description: 'Look up pull requests, merge requests, pipelines and Jira tickets, and post comments on them, through the workctl MCP server (GitHub, GitLab, Jira, Azure DevOps, Confluence). Use when asked to read a PR or MR, its comments, reviews or checks, a Jira issue, or to comment on one.'
---

# workctl

Author: [przestaw](https://github.com/przestaw)

Use the workctl MCP tools. Do not call gh, glab or curl yourself, and never ask for, read or print a token.

## Choose the account

Most tools take an optional `profile` (account) and `repo` or `project`; omit them to use the configured defaults.
When the user works with several accounts, name the profile explicitly.

If a call says there is no credential or the CLI is not logged in, stop and tell the user to run
`workctl auth login --profile NAME` (accounts handled through gh or glab) or `workctl auth set-token --profile NAME`.

## Read

- Any provider: `pr_list`, `pr_show` (GitHub, GitLab, Azure DevOps).
- GitHub: `github_get_pull_request`, `github_get_pull_request_discussion` (the conversation),
  `github_get_pull_request_comments` (inline review threads), `github_get_pull_request_reviews`, `github_get_pull_request_checks`.
- GitLab: `gitlab_get_merge_request`, `gitlab_get_merge_request_notes`, `gitlab_list_pipelines`.
- Jira: `jira_get_issue`, `jira_get_comments`. `task_get_context` joins an issue with its branches and merge requests.
- Azure DevOps and Confluence: the `azure` and `atlassian` tools take a command line as an array, for example
  `["pr","threads","123","-p","Project"]`; use `["--help"]` to explore.
- Other services the user configured (Jenkins, Artifactory, ...): `api_request` with a path relative to the profile URL;
  `api_exec` runs one allow-listed remote command over ssh. Neither accepts a full URL or a command outside the allowlist.

## Comment

Write tools exist only when the user enabled writes (`policy.allow_write`). If they are missing, say so; do not work around it.

1. Read the existing discussion first.
2. Show the user the exact text and wait for approval.
3. Post with `github_add_pull_request_comment`, `gitlab_add_merge_request_note` or `jira_add_comment`.

The text is posted as written: no secrets, no raw logs. If a write is refused because the CLI is logged in as another
account than the profile is bound to, stop and report it; do not retry with another profile.
