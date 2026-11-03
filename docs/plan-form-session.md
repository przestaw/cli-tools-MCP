# Plan: keep the form-login session between runs (future)

Status: **not implemented**. The shipped fix is `form_login.logout_path` (variant B below). This plan is the
fallback if logging out is not enough. User manual: [services-auth.md](services-auth.md).

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
| Code | about 15 lines | about 85 lines |
| Main risk | the server may want an anti-CSRF token on the logout request | session cookie stored on disk |

Neither adds a CLI command or an MCP tool.

## Check before relying on B

The logout request has to work as a plain GET. OTRS puts a `ChallengeToken` in the logout link of its pages and
may refuse `Action=Logout` without it. Validate once the session limit allows a login:

1. `workctl --profile otrs api raw GET '/index.pl?Action=Logout'` after a login (the call itself logs in first)
   and read the answer: the login page with a "logged out" message means it works, a token error means it does not.
2. Run a few commands in a row with `logout_path` set and confirm the logins keep working, i.e. the sessions
   are really released.

If the token is required, B needs the token read from a logged-in page, which is no longer a small change:
use A instead.

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

- The `name="<user_field>"` check assumes ordinary pages of the application never contain that exact attribute.
- `auth logout` does not clear the file yet; it should for form profiles.
- Two commands started at the same time without a saved session both log in, so two sessions exist briefly.
