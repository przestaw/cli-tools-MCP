# Plan: browser OAuth / SSO login for native profiles

Author: [przestaw](https://github.com/przestaw)

Status: future idea (2026-11-03). Nothing implemented.

Goal: let `workctl auth login --profile NAME` authenticate a native token profile (`backend: "native"`, the default)
through a browser OAuth / SSO flow and store the resulting token directly in the OS keyring (`secret.Account(kind, name)`),
without requiring the user to mint a Personal Access Token by hand or switch the active account of `gh`/`glab`.

## Why this fits the identity rule

In [plan-generic-api-ssh.md](plan-generic-api-ssh.md) and [plan-agent-accounts.md](plan-agent-accounts.md):

- A `cli` profile delegates both login and request signing to `gh` or `glab` (which have a single active account per
  host at a time unless switched).
- A `native` profile owns its own credential in the workctl keyring, pinned to one profile name and one `username`,
  independent of what `gh`/`glab` is currently logged in as.

Adding browser OAuth to `workctl auth login` changes only **how the token enters the keyring** for a `native` profile:
workctl remains the sole owner of the request credential, and each profile (`przestaw`, `work`, etc.) keeps its own
isolated token in the keyring.

## Candidate flows

### 1. OAuth 2.0 Device Authorization Flow (RFC 8628) — primary for GitHub / GitLab

Same mechanism as `gh auth login` (`https://github.com/login/device`), implemented with stdlib `net/http` only:

1. `POST /login/device/code` with a public `client_id` and requested `scope` (e.g. `repo read:org gist workflow`).
2. Print `user_code` on the terminal and open `verification_uri` in the system browser (`rundll32` on Windows, `open` on
   macOS, `xdg-open` on Linux; `--no-browser` prints the URL only).
3. Poll `POST /login/oauth/access_token` at the server's `interval` until the user approves (including 2FA and
   organization SAML SSO in the browser) or the code expires.
4. Call `GET /user` (or `/api/v4/user`) with the new token to verify the account and bind `profile.Username` (refusing
   if `profile.Username` is already bound to a different login).
5. Save the token in the keyring under `<kind>:<profile-name>`.

### 2. OAuth 2.0 Authorization Code + PKCE (`127.0.0.1` loopback) — fallback for OIDC / self-hosted IdPs

For providers or enterprise IdPs that do not enable the device flow:

1. Start a temporary listener on `127.0.0.1:0` (random local port).
2. Generate PKCE `code_verifier` / `code_challenge` (`S256`) and a random `state`.
3. Open the provider's `/authorize` URL in the browser.
4. Receive `code` on the loopback callback, exchange it at `/token`, verify identity, and store the token in the
   keyring.

## Design considerations and constraints

1. **`client_id` source:**
   - Default public `client_id` compiled in for `github.com` / `gitlab.com` (not a secret under RFC 8628 / PKCE).
   - Optional `oauth_client_id` field on `config.Profile` so self-hosted GitHub Enterprise, GitLab, or custom `api`
     profiles can supply their own registered OAuth application ID.
2. **GitHub organization SAML SSO:**
   - During the browser step, GitHub prompts the user to click **Authorize** next to any SAML-enforced organization.
   - If an organization enforces strict *OAuth App access restrictions*, an org admin must approve the OAuth app once
     (or the profile can override `oauth_client_id`, or fall back to `workctl auth set-token` with an SSO-authorized
     PAT).
3. **Token refresh (`refresh_token`):**
   - Classic GitHub OAuth app tokens (`gho_...`) do not expire while used.
   - GitLab OAuth tokens, GitHub App user-to-server tokens (`ghu_...`), and corporate OIDC tokens expire (typically
     1–2 hours) and return a `refresh_token`.
   - Supporting expiring tokens requires storing `{access_token, refresh_token, expires_at}` in the keyring (or
     refreshing transparently in `api.Client` on expiry / `401` with a mutex + keyring writeback).
4. **Current workaround (no code changes):**
   - Authenticate via `gh` in the browser and pipe the resulting token into workctl's keyring:
     ```powershell
     gh auth login --hostname github.com --git-protocol https --web
     gh auth token --user <login> | workctl auth set-token --profile <profile> --stdin
     ```
