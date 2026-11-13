# Plans and design records

Author: [przestaw](https://github.com/przestaw)

Each file states its status in its first lines.

| Plan | Status |
| --- | --- |
| [plan-agent-accounts.md](plan-agent-accounts.md) | implemented |
| [plan-issue-layer.md](plan-issue-layer.md) | implemented (0.4.3) |
| [plan-form-session.md](plan-form-session.md) | implemented |
| [plan-generic-api-ssh.md](plan-generic-api-ssh.md) | implemented |
| [plan-compact-output.md](plan-compact-output.md) | part 1 implemented (0.4.4); parts 2-3 open |
| [plan-oauth-sso.md](plan-oauth-sso.md) | idea, not implemented |

## Not yet planned in detail

- **Implement `bitbucket` and `asana`:** replace the entry in `internal/app` (plannedModules) with a real module and its MCP tools.
- **Macros as MCP tools:** a named, parameterised sequence of module calls defined in a JSON file, for example
  "everything about ticket X" (Jira issue, matching pull requests, their open threads) in one call. A macro is one MCP
  tool (`macro_run`, with `macro_list` to discover them); it counts as a read only if every step is a read, and its steps
  go through the same policy and child-process path as the `cli` tool. `task ISSUE-KEY` is the
  hand-written ancestor of this.
