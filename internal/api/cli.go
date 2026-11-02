// Optional transport that sends API requests through the provider's own CLI (gh, glab).
//
// A profile with `"backend": "cli"` keeps no token in workctl: gh or glab owns the login and refreshes it. Only the
// transport changes; endpoints, payloads and result handling stay with the provider packages.

package api

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

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/proc"
)

// gitlabAPIPrefix is served by glab implicitly: `glab api` takes paths relative to it.
const gitlabAPIPrefix = "/api/v4/"

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
	tool    string        // executable name: gh or glab
	host    string        // host the CLI is logged in to, without scheme or API path
	timeout time.Duration // limit for one request; 0 means the caller's context only
	want    string        // account the profile is bound to (Profile.Username); empty means no check
	checked bool          // true once the CLI was seen logged in as want, so the lookup happens once per client
}

// newCLIBackend resolves the executable and host of a validated cli profile. It starts no process.
func newCLIBackend(profile config.Profile, timeout time.Duration) (*cliBackend, error) {
	u, err := url.Parse(config.NormalizeBaseURL(profile.URL))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q", profile.URL)
	}
	host := u.Host
	if host == "api.github.com" {
		host = "github.com" // gh is logged in to the web host, not to the API host
	}
	return &cliBackend{tool: config.CLITools[profile.Kind], host: host, timeout: timeout, want: profile.Username}, nil
}

// newCLIClient builds the Client of a validated cli profile. It reads no credential. The client's Do delegates
// to gh or glab with the same Accept header and path prefix as the native client.
func newCLIClient(profile config.Profile, timeout time.Duration) (*Client, error) {
	b, err := newCLIBackend(profile, timeout)
	if err != nil {
		return nil, err
	}
	client := &Client{baseURL: config.NormalizeBaseURL(profile.URL), headers: make(http.Header), cli: b, readOnly: profile.ReadOnly}
	client.headers.Set("Accept", "application/json")
	if profile.Kind == "github" {
		client.headers.Set("Accept", "application/vnd.github+json")
	} else {
		client.prefix = "/api/v4"
	}
	return client, nil
}

// do performs one request through the CLI. The body is sent through stdin; the returned content type is empty
// (the CLIs do not report it). The server's error statuses come back as *Error, everything else (CLI missing,
// not logged in) as a plain error. A POST is never retried: it could post a comment twice. A write is refused
// unless the CLI is logged in as the account the profile is bound to.
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
	cmd := proc.Command(ctx, b.tool, args...)
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
		return nil, "", &Error{Method: method, URL: b.host + target, Status: status, Body: cliutil.FirstNonEmpty(strings.TrimSpace(stdout.String()), msg)}
	}
	if len(msg) > 2000 {
		msg = msg[:2000] + "..."
	}
	return nil, "", fmt.Errorf("%s: %s", b.tool, cliutil.FirstNonEmpty(msg, err.Error()))
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
	if who := cliutil.JSONField(out, field); who != "" {
		return who, nil
	}
	return "", fmt.Errorf("%s returned no %s", b.tool, field)
}

// login runs the CLI's own interactive login on the terminal. Prompts stay enabled, unlike in do().
func (b *cliBackend) login(ctx context.Context) error {
	cmd := proc.Command(ctx, b.tool, "auth", "login", "--hostname", b.host)
	cmd.Env = cliEnv(cmd.Environ())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s auth login: %w", b.tool, err)
	}
	return nil
}

// CLIWhoami reports which account the CLI of a cli profile is logged in as.
func CLIWhoami(cfg *config.Config, profile config.Profile) (tool, who string, err error) {
	b, err := newCLIBackend(profile, cfg.ProfileTimeout(profile))
	if err != nil {
		return "", "", err
	}
	who, err = b.whoami(context.Background())
	return b.tool, who, err
}

// BindCLI optionally logs in through the provider's CLI, then binds the profile to the account the CLI is logged
// in as (Profile.Username). It fails when the CLI is not logged in, or when the profile is already bound to another
// account; to move a profile to another account, clear its "username" first. Pass login=false to only check an
// existing login (scripts).
func BindCLI(cfg *config.Config, name string, profile config.Profile, login bool) (config.Profile, error) {
	b, err := newCLIBackend(profile, cfg.ProfileTimeout(profile))
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
