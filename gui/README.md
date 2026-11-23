# workctl GUI (prototype)

Author: [przestaw](https://github.com/przestaw)

A small Qt (PySide6) desktop front end for the `workctl` binary: list profiles and their credential state, add a
profile and store its credential, run `doctor`, and edit the write policy, defaults and repo/project bindings.
It is a prototype; the CLI stays the reference.

## Install and run

```sh
cd gui
python3 -m venv .venv
.venv/bin/pip install -r requirements.txt
.venv/bin/python -m workctl_gui [--workctl PATH] [--config FILE]
```

The binary is `--workctl`, else `$WORKCTL_GUI_BINARY`, else `workctl` on `PATH`; both can be changed at the top of
the window. Tests (offscreen, with a fake workctl; scratch files go to `gui/.scratch/`):

```sh
QT_QPA_PLATFORM=offscreen .venv/bin/python -m pytest -q tests
```

## Layout

| File | Role |
| --- | --- |
| `workctl_gui/cli.py` | the only code that runs processes: timeouts, captured stderr, JSON parsing, secrets on stdin |
| `workctl_gui/models.py` | parsed `accounts`/`doctor` output, and `config.json` editing (atomic write, `.bak`) |
| `workctl_gui/actions.py` | multi-step operations (create a profile, save and check the config), no Qt |
| `workctl_gui/views.py`, `dialogs.py` | Profiles, Doctor and Config tabs; add-profile, login and diff dialogs |
| `workctl_gui/tasks.py`, `app.py` | background runner and main window |

## What runs where

- Profiles: `workctl accounts` (JSON). Credential state is the `credential` field; secrets are never read.
- Add profile: `workctl github|gitlab|jira init --non-interactive ...`; for `api`, `google`, `azure` and `atlassian`
  there is no such command, so the GUI writes the profile into `config.json` itself. The token then goes to
  `workctl auth set-token --profile NAME --stdin`; OAuth2 and Google profiles start `workctl auth login`.
- Doctor: `workctl doctor` (always JSON; exit 1 when a check failed).
- Config: read from the path `workctl config path` prints; edits are shown as a diff, then written atomically after
  copying the old file to `config.json.bak`. `workctl config show` must load the result, else the backup is restored.

## Safety

- The GUI never touches the OS keyring and never runs gh, glab or any provider CLI; it runs only `workctl`.
- Secrets are typed into masked fields, passed to workctl on stdin, then cleared; they are never in argv, in the
  environment, in logs or on screen.
- The config writer keeps unknown fields, refuses to save when the file changed on disk since it was loaded, and
  writes files readable by the owner only.
- To try it without your real setup, point `HOME`, `XDG_CONFIG_HOME` and `XDG_CACHE_HOME` at a scratch directory and
  unset `DBUS_SESSION_BUS_ADDRESS` (workctl then cannot reach the keyring and reports a keyring error instead).

## Known gaps

- `workctl auth status` has no JSON output; the GUI uses `workctl accounts` instead.
- No CLI command adds an `api`, `google`, `azure` or `atlassian` profile, sets `policy.*` or `defaults.*`, or binds
  repos/projects (the `accounts` MCP tool can bind, the `accounts` command cannot): the GUI edits `config.json`.
- `workctl config show` checks only what loading checks (JSON, version, policy, google presets), not the full
  profile validation that `init` runs; there is no `workctl config validate`.
- `doctor` has no fix hint field; the GUI takes the command quoted in the error, or a hint per `code`.
- `workctl auth login` for a `cli` (gh/glab) profile runs `gh|glab auth login`, which needs a terminal; run it in a
  terminal, or log in with gh/glab first and create the profile with backend `cli`.
