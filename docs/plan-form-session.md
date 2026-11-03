# Plan: keep the form-login session between runs

Status: **implemented** next to variant B: a profile with `form_login.logout_path` logs out after every run (B); a
profile without it keeps its session between runs (A). The expired-session check looks for a password input of
`password_field` (a real login form), not just the user field. User manual: [services-auth.md](services-auth.md).

## Problem

A profile with `auth: "form"` logs in on the first request of every client, and every `workctl` command is a
new process, so every command opens a new server session and none is closed. Servers cap the sessions per
user (OTRS answers `Session per user limit reached!` on the login page, HTTP 200, no session cookie), after
which every login fails until the old sessions expire. The failure reads like a wrong password.

## Variants (no change on the server in either)

| | B. log out after each run (shipped) | A. keep the session (this plan) |
| --- | --- | --- |
| Config | optional `form_login.logout_path` | none |
| Sessions held | 0 after a run | 1 per profile |
| Requests per command | login page, login, request, logout | request only, login once |
| Verification | replay original cookies against a protected page | session cookie stored on disk |
| Main risk | cleanup failures must stop the caller | session cookie stored on disk |

Variant B supports a logout challenge token and adds `api session-check`; it adds no MCP tool.

## Check before relying on B

Configure `logout_path`, `logout_token_field: "ChallengeToken"` and a protected
`logout_verify_path`, as described in [services-auth.md](services-auth.md). Then run
`workctl --profile otrs api session-check`. It checks login and server-side invalidation in one
operation without printing page contents or credentials. A successful response from `Action=Logout`
alone is not evidence that the session was invalidated. Do not use a standalone raw logout request:
that request opens its own session and does not close sessions from previous processes.

## Design of A

- **Where**: `os.UserCacheDir()/workctl/session-<hash>.json`, mode 0600 in a 0700 directory; `<hash>` is the
  first 8 bytes of SHA-256 over profile name, URL and user name. No cache directory means no persistence.
- **What**: a name-to-value map of the cookies the jar holds for the login URL. Never the password.
- **Load**: before the first request, if the file holds the `session_cookie`, put the cookies into the jar and
  treat the client as logged in (skip the login).
- **Save**: after a successful login. A failed write only costs a login next time.
- **Expiry**: a session the server dropped is answered with the login page. A loaded session is checked once, on
  the first response: when the body contains `name="<user_field>"`, delete the file, reset the jar, log in again
  and repeat the request once. The repeat is safe because the login page means the request did not run.
- **Tests**: two runs share one login; a stale cookie in the file leads to a second login and a correct page;
  tests point the cache directory at a temporary one (`XDG_CACHE_HOME`, `HOME`, `LocalAppData`).
- **Docs**: one paragraph in `services-auth.md` that names the file and the re-login.

## Open points for A

- The login-page check assumes ordinary pages never contain a password input named `password_field`.
- ~~`auth logout` does not clear the file yet~~: it does (`api.ForgetSession`).
- Two commands started at the same time without a saved session both log in, so two sessions exist briefly.
