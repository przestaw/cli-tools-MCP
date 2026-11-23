# CLI for managing profiles and config

Author: [przestaw](https://github.com/przestaw)

Status: implemented. Usage: [cli.md](../cli.md#profiles-config-and-credentials-cli-only).

A script or another program should drive workctl through its CLI, not by editing `config.json`. Everything below is CLI only; no MCP tool
is added, so an agent still cannot change profiles, the write policy or credentials.

| Need | Command |
| --- | --- |
| Profiles of every kind (api, google, azure, atlassian, ...) | `profile add NAME --kind K [flags]`, `profile edit NAME [flags]` |
| Policy, timeouts, defaults | `config set KEY VALUE`, `config validate [--json]` (exit 1, every problem listed) |
| Credential state, no secret | `auth status --json` (`state`, `source`, `fix`) |
| Repair hints | `doctor --json`: `fix` on each failed check |
| Use an existing gh/glab login | `auth login --existing` (never starts a login) |
| Machine-readable results | `--json` on `auth login` and `auth set-token` |

Design: one `config.Config` is loaded, changed in memory, validated as a whole and saved, as in `init`. `profile`
registers one flag per profile field and parses straight into the profile, so `add` and `edit` share all of it.
`config.Config.Validate` joins all problems instead of stopping at the first. `auth.ProfileCredential` is the single
place that reads credential state; `accounts`, `auth status` and `doctor` use it.
