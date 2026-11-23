# Plans and design records

Author: [przestaw](https://github.com/przestaw)

Each file states its status in its first lines.

| Plan | Status |
| --- | --- |
| [plan-agent-accounts.md](plan-agent-accounts.md) | implemented |
| [plan-issue-layer.md](plan-issue-layer.md) | implemented (0.4.3) |
| [plan-form-session.md](plan-form-session.md) | implemented |
| [plan-generic-api-ssh.md](plan-generic-api-ssh.md) | implemented |
| [plan-compact-output.md](plan-compact-output.md) | part 1 implemented (0.4.4); part 3 for GitHub and GitLab; part 2 open |
| [plan-oauth-sso.md](plan-oauth-sso.md) | phases 0, 1 and 2b merged (#9, #10, #14); Colab API (2), device flow and token lock (3), GitHub/GitLab OAuth (4) open (Polish) |
| [plan-luki-cli.md](plan-luki-cli.md) | 9 CLI gaps reported by an agent: implemented and merged (#8, #11) (Polish) |
| [raport-prostota-kodu.md](raport-prostota-kodu.md) | partly done: 6 refactors merged (#16); Q7, M4, M5, M8 open (Polish) |
| [plan-kompresja-odpowiedzi.md](plan-kompresja-odpowiedzi.md) | opt-in compaction merged (#15), RTK companion skill (#12); document mode, TOON, retention, XML not done (Polish) |
| [plan-instalacja-skilli.md](plan-instalacja-skilli.md) | skills embedded in the binary, `workctl skills`, drift check in doctor (#17) (Polish) |
| [plan-cli-profile-management.md](plan-cli-profile-management.md) | implemented: CLI-only commands for managing profiles and config (`profile`, `config set|validate`, `auth status --json`, doctor `fix`) |

## Not yet planned in detail

- **Implement `bitbucket` and `asana`:** replace the entry in `internal/app` (plannedModules) with a real module and its MCP tools.
- **Macros as MCP tools:** a named, parameterised sequence of module calls defined in a JSON file, for example
  "everything about ticket X" (Jira issue, matching pull requests, their open threads) in one call. A macro is one MCP
  tool (`macro_run`, with `macro_list` to discover them); it counts as a read only if every step is a read, and its steps
  go through the same policy and child-process path as the `cli` tool. `task ISSUE-KEY` is the
  hand-written ancestor of this.
