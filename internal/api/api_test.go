// Contract tests of the HTTP client and the gh/glab transport, run against httptest servers
// and a fake child process (no network, no real gh or glab).

package api

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workctl/internal/config"
	"workctl/internal/proc"
	"workctl/internal/testutil"
)

func TestCLIHelperProcess(t *testing.T) { testutil.CLIHelperProcess() }

func fakeCLI(t *testing.T, mode string) *[][]string { return testutil.FakeCLI(t, mode) }

// cliTestClient builds the client of a cli profile through the production constructor.
func cliTestClient(t *testing.T, kind, url string) *Client {
	t.Helper()
	c, err := NewProfileClient(config.Default(), kind, config.Profile{Kind: kind, URL: url, Backend: config.BackendCLI, VerifyTLS: true})
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

func TestHTTPClientPolicies(t *testing.T) {
	requests := 0
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://localhost"+strings.TrimPrefix(r.Host, "127.0.0.1")+"/target", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/error" {
			w.WriteHeader(403)
		}
		_, _ = w.Write([]byte(`{"message":"denied"}`))
	}))
	defer srv.Close()
	client, err := newClient(config.Profile{URL: srv.URL, VerifyTLS: true}, "/api", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Do(context.Background(), "GET", "https://outside.test", nil, nil, ""); err == nil {
		t.Fatal("absolute URL accepted")
	}
	if _, _, err := client.Do(context.Background(), "GET", "items", nil, nil, ""); err != nil || paths[len(paths)-1] != "/api/items" {
		t.Fatalf("prefix lost: %v %v", paths, err)
	}
	requests = 0
	_, _, err = client.Do(context.Background(), "GET", "/redirect", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "cross-host redirect") || requests != 1 {
		t.Fatalf("redirect: %v requests=%d", err, requests)
	}
	_, contentType, err := client.Do(context.Background(), "GET", "/error", nil, nil, "")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || contentType != "application/json" {
		t.Fatalf("HTTP error changed: %v %s", err, contentType)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = client.Do(ctx, "GET", "/cancel", nil, nil, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestHTTPClientUsesCustomCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := newClient(config.Profile{URL: srv.URL, VerifyTLS: true, CAFile: file}, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Do(context.Background(), "GET", "/", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newClient(config.Profile{URL: srv.URL, VerifyTLS: true, CAFile: file}, "", time.Second); err == nil {
		t.Fatal("invalid CA accepted")
	}
}

func TestAPIProfileAuthSchemes(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { got = r.Header.Clone() }))
	defer srv.Close()
	t.Setenv("WORKCTL_TEST_API_TOKEN", "s3cret")
	for name, tc := range map[string]struct {
		auth, header, username string
		wantHeader, wantValue  string
	}{
		"bearer": {"", "", "", "Authorization", "Bearer s3cret"},
		"basic":  {"basic", "", "me", "Authorization", "Basic bWU6czNjcmV0"},
		"header": {"header", "X-Api-Key", "", "X-Api-Key", "s3cret"},
		"none":   {"none", "", "", "Authorization", ""},
	} {
		profile := config.Profile{Kind: "api", URL: srv.URL, Auth: tc.auth, AuthHeader: tc.header, Username: tc.username, TokenEnv: "WORKCTL_TEST_API_TOKEN", VerifyTLS: true}
		c, err := NewProfileClient(config.Default(), "p", profile)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, _, err := c.Do(context.Background(), http.MethodGet, "/", nil, nil, ""); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if v := got.Get(tc.wantHeader); v != tc.wantValue {
			t.Errorf("%s: %s = %q, want %q", name, tc.wantHeader, v, tc.wantValue)
		}
	}
}

func TestSSHTunnelCarriesVerifiedTLSAndToken(t *testing.T) {
	calls := fakeCLI(t, "ssh-w")
	var auth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKCTL_TEST_API_TOKEN", "s3cret")
	profile := config.Profile{Kind: "api", URL: srv.URL, CAFile: ca, VerifyTLS: true, TokenEnv: "WORKCTL_TEST_API_TOKEN", Backend: config.BackendSSHTunnel, SSHDestination: "bastion-prod"}
	c, err := NewProfileClient(config.Default(), "p", profile)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := c.Do(context.Background(), http.MethodGet, "/x", nil, nil, "")
	if err != nil || string(b) != `{"ok":true}` || auth != "Bearer s3cret" {
		t.Fatalf("tunnelled request: %s %v auth=%q", b, err, auth)
	}
	addr := strings.TrimPrefix(srv.URL, "https://")
	want := "ssh -o BatchMode=yes -o LogLevel=ERROR -W " + addr + " -- bastion-prod"
	if len(*calls) != 1 || strings.Join((*calls)[0], " ") != want {
		t.Fatalf("ssh calls = %q, want %q", *calls, want)
	}
	for _, arg := range (*calls)[0] {
		if strings.Contains(arg, "s3cret") {
			t.Fatal("token reached the ssh command line")
		}
	}
}

func TestSSHTunnelReportsSSHFailure(t *testing.T) {
	fakeCLI(t, "ssh-denied")
	profile := config.Profile{Kind: "api", URL: "https://svc.internal.test", Auth: "none", VerifyTLS: true, Backend: config.BackendSSHTunnel, SSHDestination: "bastion"}
	c, err := NewProfileClient(config.Default(), "p", profile)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Do(context.Background(), http.MethodGet, "/", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "Permission denied (publickey)") {
		t.Fatalf("err = %v", err)
	}
}

func TestCLIBackendGitHubPostUsesGhWithBodyOnStdin(t *testing.T) {
	fakeCLI(t, "ok")
	t.Setenv("GH_TOKEN", "must-not-reach-gh")
	c := cliTestClient(t, "github", "https://api.github.com")

	payloadIn, _ := json.Marshal(map[string]string{"body": "line 1\nżółć `x` $(y)"})
	b, _, err := c.Do(context.Background(), http.MethodPost, "/repos/o/r/issues/5/comments", nil, payloadIn, "application/json")
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

	b, _, err := c.Do(context.Background(), http.MethodPost, "/api/v4/projects/g%2Fp/merge_requests/7/notes", url.Values{"body": {"a note"}}, nil, "")
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

	raw := c.WithAccept("application/vnd.github.raw+json")
	b, _, err := raw.Do(context.Background(), http.MethodGet, "/repos/o/r/contents/docs/:repo%20%7Bowner%7D.md", url.Values{"ref": {"main"}}, nil, "")
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

func TestCLIBackendMapsHTTPFailureToError(t *testing.T) {
	fakeCLI(t, "404")
	c := cliTestClient(t, "github", "https://api.github.com")

	_, _, err := c.Do(context.Background(), http.MethodGet, "/repos/o/missing", nil, nil, "")
	var apiErr *Error
	if !errors.As(err, &apiErr) || apiErr.Status != 404 || !strings.Contains(apiErr.Body, "Not Found") {
		t.Fatalf("err = %#v", err)
	}
}

func TestCLIBackendReportsMissingLoginAndMissingTool(t *testing.T) {
	fakeCLI(t, "auth")
	c := cliTestClient(t, "github", "https://api.github.com")
	_, _, err := c.Do(context.Background(), http.MethodGet, "/user", nil, nil, "")
	var apiErr *Error
	if err == nil || errors.As(err, &apiErr) || !strings.Contains(err.Error(), "gh auth login") {
		t.Fatalf("not logged in: %v", err)
	}

	old := proc.Command
	proc.Command = func(ctx context.Context, _ string, arg ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "workctl-no-such-tool", arg...)
	}
	t.Cleanup(func() { proc.Command = old })
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/user", nil, nil, ""); err == nil || !strings.Contains(err.Error(), "gh not found in PATH") {
		t.Fatalf("missing tool: %v", err)
	}
}

func TestCLIBackendHonoursCancellationAndTimeout(t *testing.T) {
	fakeCLI(t, "sleep")
	c := cliTestClient(t, "github", "https://api.github.com")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := c.Do(ctx, http.MethodGet, "/user", nil, nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	c.cli.timeout = 100 * time.Millisecond
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/user", nil, nil, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
}

func TestCLIBackendRefusesPathsOutsideTheProfileHost(t *testing.T) {
	calls := fakeCLI(t, "ok")
	gh := cliTestClient(t, "github", "https://api.github.com")
	gl := cliTestClient(t, "gitlab", "https://gitlab.example.com")

	for _, tc := range []struct {
		c    *Client
		path string
	}{
		{gh, "https://evil.example/x"},
		{gh, "//evil.example/x"},
		{gl, "/not-the-api/x"},
	} {
		if _, _, err := tc.c.Do(context.Background(), "GET", tc.path, nil, nil, ""); err == nil {
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
	post := func(c *Client) error {
		payload, _ := json.Marshal(map[string]string{"body": "x"})
		_, _, err := c.Do(ctx, http.MethodPost, "/repos/o/r/issues/1/comments", nil, payload, "application/json")
		return err
	}
	profile := config.Profile{Kind: "github", URL: "https://api.github.com", Backend: config.BackendCLI, VerifyTLS: true, Username: "someone-else"}
	c, err := NewProfileClient(config.Default(), "gh", profile)
	if err != nil {
		t.Fatal(err)
	}
	if err := post(c); err == nil || !strings.Contains(err.Error(), `"przestaw"`) {
		t.Fatalf("write as another account: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("only the account lookup may run, got %v", *calls)
	}

	profile.Username = "PrzeStaw" // same account, any letter case
	if c, err = NewProfileClient(config.Default(), "gh", profile); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := post(c); err != nil {
			t.Fatal(err)
		}
	}
	if len(*calls) != 1+1+2 { // the account is looked up once per client, not once per write
		t.Fatalf("calls = %v", *calls)
	}
}
