// @file cli_backend.go
// @brief Optional transport that sends API requests through the provider's own CLI (gh, glab).
//
// A profile with `"backend": "cli"` keeps no token in workctl: gh or glab owns the login and refreshes it. Only the
// transport changes; endpoints, payloads and result handling stay in github.go, gitlab.go and pr.go.
//
// @see config.go      Profile.Backend and validateProfile, which calls validateBackend
// @see httpclient.go  APIClient.do hands the request to cliBackend.do when the profile uses this backend
// @see auth.go        `workctl auth login|status` call bindCLI and whoami
// @see init.go        `workctl github|gitlab init --backend cli` calls bindCLI
// @see modules.go      execCommand, the process hook shared with the embedded-module child processes

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// backendCLI is the Profile.Backend value that selects this transport; an empty value or "native" selects net/http.
const backendCLI = "cli"

// gitlabAPIPrefix is served by glab implicitly: `glab api` takes paths relative to it.
const gitlabAPIPrefix = "/api/v4/"

// cliTools maps a profile kind to the executable that serves it.
var cliTools = map[string]string{"github": "gh", "gitlab": "glab"}

// cliEnvDrop lists variables that would make gh or glab act as another account, host or repository than the profile
// says, or print debug output that could carry a credential.
var cliEnvDrop = []string{
	"GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN", "GH_HOST", "GH_REPO", "GH_DEBUG",
	"GITLAB_TOKEN", "GITLAB_ACCESS_TOKEN", "OAUTH_TOKEN", "GITLAB_HOST", "GL_HOST", "GITLAB_URI", "GITLAB_REPO", "GLAB_DEBUG_HTTP",
}

// cliPlaceholders keeps gh and glab from expanding :repo, {owner} and the like from the working directory's repository.
var cliPlaceholders = strings.NewReplacer(":", "%3A", "{", "%7B", "}", "%7D")

// cliStatusRe finds the HTTP status in the stderr of a failed request, for example "gh: Not Found (HTTP 404)".
var cliStatusRe = regexp.MustCompile(`\(HTTP (\d{3})\)`)

// cliBackend runs API requests as `gh api` or `glab api` child processes.
type cliBackend struct {
	tool    string        ///< executable name: gh or glab
	host    string        ///< host the CLI is logged in to, without scheme or API path
	timeout time.Duration ///< limit for one request; 0 means the caller's context only
	want    string        ///< account the profile is bound to (Profile.Username); empty means no check
	checked bool          ///< true once the CLI was seen logged in as want, so the lookup happens once per client
}

// usesCLI reports whether the profile sends its requests through gh or glab.
func (p Profile) usesCLI() bool { return strings.EqualFold(p.Backend, backendCLI) }

// validateBackend checks Profile.Backend against the settings that the CLI transport cannot honour.
//
// @param name    profile name, for the error message
// @param profile profile being validated
// @param u       parsed profile URL
// @return nil for a native profile or a consistent cli profile
// @note The CLI owns credentials and TLS, so token_env, ca_file and verify_tls=false are refused instead of ignored.
func validateBackend(name string, profile Profile, u *url.URL) error {
	switch strings.ToLower(profile.Backend) {
	case "", "native":
		return nil
	case backendCLI:
	default:
		return fmt.Errorf("profiles.%s.backend %q is unsupported (use native or cli)", name, profile.Backend)
	}
	tool := cliTools[profile.Kind]
	switch {
	case tool == "":
		return fmt.Errorf("profiles.%s.backend cli is only available for github and gitlab", name)
	case u.Scheme != "https" || strings.Trim(u.Path, "/") != "":
		return fmt.Errorf("profiles.%s.url must be https://HOST without a path for the cli backend (%s adds the API path)", name, tool)
	case profile.TokenEnv != "" || profile.CAFile != "" || !profile.VerifyTLS:
		return fmt.Errorf("profiles.%s: the cli backend needs verify_tls=true and no token_env or ca_file (%s keeps its own credentials and TLS settings)", name, tool)
	}
	return nil
}

// newCLIBackend resolves the executable and host of a validated cli profile. It starts no process.
//
// @param profile validated profile with Backend "cli"
// @param timeout limit for one request
func newCLIBackend(profile Profile, timeout time.Duration) (*cliBackend, error) {
	u, err := url.Parse(normalizeBaseURL(profile.URL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q", profile.URL)
	}
	host := u.Host
	if host == "api.github.com" {
		host = "github.com" // gh is logged in to the web host, not to the API host
	}
	return &cliBackend{tool: cliTools[profile.Kind], host: host, timeout: timeout, want: profile.Username}, nil
}

// newCLIClient builds the APIClient of a validated cli profile. It reads no credential.
//
// @return client whose do() delegates to gh or glab, with the same Accept header and path prefix as the native client
func newCLIClient(profile Profile, timeout time.Duration) (*APIClient, error) {
	b, err := newCLIBackend(profile, timeout)
	if err != nil {
		return nil, err
	}
	client := &APIClient{baseURL: normalizeBaseURL(profile.URL), headers: make(http.Header), cli: b}
	client.headers.Set("Accept", "application/json")
	if profile.Kind == "github" {
		client.headers.Set("Accept", "application/vnd.github+json")
	} else {
		client.prefix = "/api/v4"
	}
	return client, nil
}

// do performs one request through the CLI.
//
// @param ctx         cancellation and deadline of the request
// @param method      HTTP method
// @param target      server-relative path with the query already appended, as built by APIClient.do
// @param accept      Accept header to send
// @param body        request body, sent through stdin; nil for none
// @param contentType Content-Type of body; defaults to application/json
// @return response body and an empty content type (the CLIs do not report it)
// @return *APIError when the server answered with an error status, a plain error otherwise (CLI missing, not logged in)
// @note A POST is never retried: it could post a comment twice. A write is refused unless the CLI is logged in as the
// account the profile is bound to.
func (b *cliBackend) do(ctx context.Context, method, target, accept string, body []byte, contentType string) ([]byte, string, error) {
	if strings.HasPrefix(target, "//") {
		return nil, "", fmt.Errorf("invalid request path %q", target)
	}
	if method != http.MethodGet && b.want != "" && !b.checked {
		who, err := b.whoami(ctx)
		if err != nil {
			return nil, "", err
		}
		if !strings.EqualFold(who, b.want) {
			return nil, "", fmt.Errorf("%s is logged in as %q but the profile is bound to %q: run `workctl auth login`", b.tool, who, b.want)
		}
		b.checked = true
	}
	endpoint := target
	if b.tool == "glab" {
		var ok bool
		if endpoint, ok = strings.CutPrefix(target, gitlabAPIPrefix); !ok {
			return nil, "", fmt.Errorf("glab serves only %s, got %q", gitlabAPIPrefix, target)
		}
	}
	args := []string{"api", "--hostname", b.host, "--method", method}
	if accept != "" {
		args = append(args, "-H", "Accept: "+accept)
	}
	if body != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		args = append(args, "-H", "Content-Type: "+contentType, "--input", "-")
	}
	args = append(args, "--", cliPlaceholders.Replace(endpoint))

	if b.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}
	cmd := execCommand(ctx, b.tool, args...)
	cmd.Env = append(cliEnv(cmd.Environ()), "GH_PROMPT_DISABLED=1")
	if body != nil {
		cmd.Stdin = bytes.NewReader(body)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), "", nil
	}
	if ctx.Err() != nil {
		return nil, "", fmt.Errorf("%s: %w", b.tool, ctx.Err())
	}
	if errors.Is(err, exec.ErrNotFound) {
		return nil, "", fmt.Errorf("%s not found in PATH: install it and run `%s auth login`", b.tool, b.tool)
	}
	msg := strings.TrimSpace(stderr.String())
	if m := cliStatusRe.FindStringSubmatch(msg); m != nil {
		status, _ := strconv.Atoi(m[1])
		return nil, "", &APIError{Method: method, URL: b.host + target, Status: status, Body: firstNonEmpty(strings.TrimSpace(stdout.String()), msg)}
	}
	if len(msg) > 2000 {
		msg = msg[:2000] + "..."
	}
	return nil, "", fmt.Errorf("%s: %s", b.tool, firstNonEmpty(msg, err.Error()))
}

// cliEnv returns env without the cliEnvDrop variables and with colors switched off.
func cliEnv(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if !slices.ContainsFunc(cliEnvDrop, func(name string) bool { return strings.EqualFold(key, name) }) { // Windows names are case-insensitive
			out = append(out, kv)
		}
	}
	return append(out, "NO_COLOR=1")
}

// whoami asks the provider which account the CLI is logged in as. It is the one identity check of init, auth and writes.
func (b *cliBackend) whoami(ctx context.Context) (string, error) {
	path, field := "/user", "login"
	if b.tool == "glab" {
		path, field = gitlabAPIPrefix+"user", "username"
	}
	out, _, err := b.do(ctx, http.MethodGet, path, "", nil, "")
	if err != nil {
		return "", err
	}
	if who := jsonField(out, field); who != "" {
		return who, nil
	}
	return "", fmt.Errorf("%s returned no %s", b.tool, field)
}

// login runs the CLI's own interactive login on the terminal. Prompts stay enabled, unlike in do().
func (b *cliBackend) login(ctx context.Context) error {
	cmd := execCommand(ctx, b.tool, "auth", "login", "--hostname", b.host)
	cmd.Env = cliEnv(cmd.Environ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s auth login: %w", b.tool, err)
	}
	return nil
}

// bindCLI optionally logs in, then binds the profile to the account the CLI is logged in as.
//
// @param cfg     configuration, for the request timeout
// @param name    profile name, for messages
// @param profile cli profile to bind
// @param login   false to only check an existing login (scripts)
// @return profile with Username set to the logged-in account
// @return an error when the CLI is not logged in, or when the profile is bound to another account already
// @note To move a profile to another account, clear its "username" first.
func bindCLI(cfg *Config, name string, profile Profile, login bool) (Profile, error) {
	b, err := newCLIBackend(profile, profileTimeout(cfg, profile))
	if err != nil {
		return profile, err
	}
	ctx := context.Background()
	if login {
		if err := b.login(ctx); err != nil {
			return profile, err
		}
	}
	who, err := b.whoami(ctx)
	if err != nil {
		return profile, fmt.Errorf("profile %q: %w (run `workctl auth login --profile %s`)", name, err, name)
	}
	if profile.Username != "" && !strings.EqualFold(who, profile.Username) {
		return profile, fmt.Errorf("%s is logged in as %q but profile %q is bound to %q: clear its \"username\" to rebind it", b.tool, who, name, profile.Username)
	}
	profile.Username = who
	return profile, nil
}

// errCLIManaged explains why a cli profile has no workctl token to set or remove; nil for any other profile.
func errCLIManaged(name string, profile Profile) error {
	if !profile.usesCLI() {
		return nil
	}
	tool := cliTools[profile.Kind]
	return fmt.Errorf("profile %q is logged in through %s: use `workctl auth login --profile %s` (to log out run `%s auth logout`)", name, tool, name, tool)
}
