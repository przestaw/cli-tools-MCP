// @file cli_backend_test.go
// @brief Contract tests of the gh/glab transport, run against a fake child process (no network, no real gh or glab).
// @see cli_backend.go

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestCLIHelperProcess is the fake gh/glab. The mode comes from WORKCTL_TEST_CLI; it is a no-op in a normal test run.
func TestCLIHelperProcess(t *testing.T) {
	mode := os.Getenv("WORKCTL_TEST_CLI")
	if mode == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch mode {
	case "ok":
		stdin, _ := io.ReadAll(os.Stdin)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": args, "stdin": string(stdin), "ghToken": os.Getenv("GH_TOKEN")})
	case "404":
		os.Stdout.WriteString(`{"message":"Not Found"}`)
		os.Stderr.WriteString("gh: Not Found (HTTP 404)")
		os.Exit(1)
	case "auth":
		os.Stderr.WriteString("To get started with GitHub CLI, please run:  gh auth login")
		os.Exit(4)
	case "user": // `auth login` succeeds silently, any API call answers with the logged-in account
		if args[1] != "auth" {
			os.Stdout.WriteString(`{"login":"przestaw","username":"przestaw"}`)
		}
	case "sleep":
		time.Sleep(10 * time.Second)
	}
	os.Exit(0)
}

// fakeCLI makes execCommand start the test binary as the fake CLI in the given mode and records every call
// (executable name first).
func fakeCLI(t *testing.T, mode string) *[][]string {
	t.Helper()
	var calls [][]string
	old := execCommand
	execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, arg...))
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestCLIHelperProcess", "--", name}, arg...)...)
		cmd.Env = append(os.Environ(), "WORKCTL_TEST_CLI="+mode)
		return cmd
	}
	t.Cleanup(func() { execCommand = old })
	return &calls
}

// cliTestClient builds the client of a cli profile through the production constructor.
func cliTestClient(t *testing.T, kind, url string) *APIClient {
	t.Helper()
	c, err := newProfileClient(defaultConfig(), kind, Profile{Kind: kind, URL: url, Backend: backendCLI, VerifyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// cliEcho is what the fake CLI reports about the call it received.
type cliEcho struct {
	Args    []string `json:"args"`
	Stdin   string   `json:"stdin"`
	GhToken string   `json:"ghToken"`
}

func decodeEcho(t *testing.T, b []byte) cliEcho {
	t.Helper()
	var e cliEcho
	if err := json.Unmarshal(b, &e); err != nil {
		t.Fatalf("fake CLI output %q: %v", b, err)
	}
	return e
}

func TestCLIBackendGitHubCommentUsesGhWithBodyOnStdin(t *testing.T) {
	fakeCLI(t, "ok")
	t.Setenv("GH_TOKEN", "must-not-reach-gh")
	c := cliTestClient(t, "github", "https://api.github.com")

	b, err := githubAddComment(context.Background(), c, "o/r", 5, "line 1\nżółć `x` $(y)")
	if err != nil {
		t.Fatal(err)
	}
	e := decodeEcho(t, b)
	want := "gh api --hostname github.com --method POST -H Accept: application/vnd.github+json -H Content-Type: application/json --input - -- /repos/o/r/issues/5/comments"
	if got := strings.Join(e.Args, " "); got != want {
		t.Fatalf("args = %q\nwant   %q", got, want)
	}
	var payload struct{ Body string }
	if err := json.Unmarshal([]byte(e.Stdin), &payload); err != nil || payload.Body != "line 1\nżółć `x` $(y)" {
		t.Fatalf("stdin = %q, %v", e.Stdin, err)
	}
	if e.GhToken != "" {
		t.Fatal("GH_TOKEN of the environment reached gh")
	}
}

func TestCLIBackendGitLabDropsAPIPrefixAndKeepsQuery(t *testing.T) {
	fakeCLI(t, "ok")
	c := cliTestClient(t, "gitlab", "https://gitlab.example.com")

	b, err := gitlabAddMRNote(context.Background(), c, "g/p", 7, "a note")
	if err != nil {
		t.Fatal(err)
	}
	e := decodeEcho(t, b)
	want := "glab api --hostname gitlab.example.com --method POST -H Accept: application/json -- projects/g%2Fp/merge_requests/7/notes?body=a+note"
	if got := strings.Join(e.Args, " "); got != want {
		t.Fatalf("args = %q\nwant   %q", got, want)
	}
	if e.Stdin != "" {
		t.Fatalf("a request without a body got stdin %q", e.Stdin)
	}
}

func TestCLIBackendKeepsRawAcceptHeaderAndEscapesPlaceholders(t *testing.T) {
	fakeCLI(t, "ok")
	c := cliTestClient(t, "github", "https://api.github.com")

	b, err := githubGetFile(context.Background(), c, "o/r", "docs/:repo {owner}.md", "main")
	if err != nil {
		t.Fatal(err)
	}
	e := decodeEcho(t, b)
	got := strings.Join(e.Args, " ")
	if !strings.Contains(got, "-H Accept: application/vnd.github.raw+json") {
		t.Fatalf("raw Accept header lost: %q", got)
	}
	if !strings.HasSuffix(got, "-- /repos/o/r/contents/docs/%3Arepo%20%7Bowner%7D.md?ref=main") {
		t.Fatalf("placeholders reach gh unescaped: %q", got)
	}
}

func TestCLIBackendMapsHTTPFailureToAPIError(t *testing.T) {
	fakeCLI(t, "404")
	c := cliTestClient(t, "github", "https://api.github.com")

	_, err := githubGetRepo(context.Background(), c, "o/missing")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || !strings.Contains(apiErr.Body, "Not Found") {
		t.Fatalf("err = %#v", err)
	}
}

func TestCLIBackendReportsMissingLoginAndMissingTool(t *testing.T) {
	fakeCLI(t, "auth")
	c := cliTestClient(t, "github", "https://api.github.com")
	_, err := githubUser(context.Background(), c)
	var apiErr *APIError
	if err == nil || errors.As(err, &apiErr) || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("not logged in: %v", err)
	}

	old := execCommand
	execCommand = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "workctl-no-such-tool", arg...)
	}
	t.Cleanup(func() { execCommand = old })
	if _, err := githubUser(context.Background(), c); err == nil || !strings.Contains(err.Error(), "gh not found in PATH") {
		t.Fatalf("missing tool: %v", err)
	}
}

func TestCLIBackendHonoursCancellationAndTimeout(t *testing.T) {
	fakeCLI(t, "sleep")
	c := cliTestClient(t, "github", "https://api.github.com")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := githubUser(ctx, c); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	c.cli.timeout = 100 * time.Millisecond
	if _, err := githubUser(context.Background(), c); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}

func TestCLIBackendRefusesPathsOutsideTheProfileHost(t *testing.T) {
	calls := fakeCLI(t, "ok")
	gh := cliTestClient(t, "github", "https://api.github.com")
	gl := cliTestClient(t, "gitlab", "https://gitlab.example.com")

	for _, tc := range []struct {
		c    *APIClient
		path string
	}{
		{gh, "https://evil.example/x"},
		{gh, "//evil.example/x"},
		{gl, "/not-the-api/x"},
	} {
		if _, _, err := tc.c.do(context.Background(), "GET", tc.path, nil, nil, ""); err == nil {
			t.Errorf("%q accepted", tc.path)
		}
	}
	if len(*calls) != 0 {
		t.Fatalf("a refused path started a process: %v", *calls)
	}
}

func TestCLIEnvDropsAccountSelectingVariables(t *testing.T) {
	got := strings.Join(cliEnv([]string{"PATH=p", "GH_TOKEN=a", "gh_host=b", "GITLAB_TOKEN=c", "KEEP=1"}), " ")
	if got != "PATH=p KEEP=1 NO_COLOR=1" {
		t.Fatalf("env = %q", got)
	}
}

func TestCLIBackendRefusesWritesAsAnotherAccount(t *testing.T) {
	calls := fakeCLI(t, "user")
	ctx := context.Background()
	profile := Profile{Kind: "github", URL: "https://api.github.com", Backend: backendCLI, VerifyTLS: true, Username: "someone-else"}
	c, err := newProfileClient(defaultConfig(), "gh", profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := githubAddComment(ctx, c, "o/r", 1, "x"); err == nil || !strings.Contains(err.Error(), `"przestaw"`) {
		t.Fatalf("write as another account: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("only the account lookup may run, got %v", *calls)
	}

	profile.Username = "PrzeStaw" // same account, any letter case
	if c, err = newProfileClient(defaultConfig(), "gh", profile); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := githubAddComment(ctx, c, "o/r", 1, "x"); err != nil {
			t.Fatal(err)
		}
	}
	if len(*calls) != 1+1+2 { // the account is looked up once per client, not once per write
		t.Fatalf("calls = %v", *calls)
	}
}

func TestValidateBackend(t *testing.T) {
	ok := Profile{Kind: "github", URL: "https://api.github.com", Backend: "cli", VerifyTLS: true}
	with := func(edit func(*Profile)) Profile { p := ok; edit(&p); return p }
	for name, tc := range map[string]struct {
		profile Profile
		wantErr string
	}{
		"valid github":    {ok, ""},
		"valid gitlab":    {with(func(p *Profile) { p.Kind, p.URL = "gitlab", "https://gitlab.example.com" }), ""},
		"native default":  {with(func(p *Profile) { p.Backend, p.TokenEnv = "", "TOK" }), ""},
		"unknown backend": {with(func(p *Profile) { p.Backend = "ssh" }), "unsupported"},
		"jira":            {with(func(p *Profile) { p.Kind, p.URL = "jira", "https://jira.example.com" }), "only available for github and gitlab"},
		"url with path":   {with(func(p *Profile) { p.URL = "https://ghe.example.com/api/v3" }), "without a path"},
		"http":            {with(func(p *Profile) { p.URL, p.VerifyTLS = "http://localhost:3000", false }), "https://HOST"},
		"token env":       {with(func(p *Profile) { p.TokenEnv = "TOK" }), "token_env"},
		"ca file":         {with(func(p *Profile) { p.CAFile = os.TempDir() }), "ca_file"},
		"tls off":         {with(func(p *Profile) { p.VerifyTLS = false }), "verify_tls"},
	} {
		err := validateProfile("p", tc.profile)
		if (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.wantErr)
		}
	}
}
