// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package generic

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"workctl/internal/config"
	"workctl/internal/testutil"
)

func testConfig(url string) *config.Config {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{"svc": {Kind: Kind, URL: url, Auth: "none", VerifyTLS: true}}
	return cfg
}

func TestAPIRequestReadsAndGuardsWrites(t *testing.T) {
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, path, body = r.Method, r.URL.Path, string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL + "/base")
	ctx := context.Background()

	out, err := CallTool(ctx, cfg, "request", map[string]any{"path": "/job/x/api/json"})
	if err != nil || string(out) != `{"ok":true}` || method != "GET" || path != "/base/job/x/api/json" {
		t.Fatalf("GET: %s %v %s %s", out, err, method, path)
	}
	if _, err := CallTool(ctx, cfg, "request", map[string]any{"method": "post", "path": "/build"}); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("POST without allow_write: %v", err)
	}
	if _, err := CallTool(ctx, cfg, "request", map[string]any{"path": "https://elsewhere.test/"}); err == nil {
		t.Fatal("absolute URL accepted")
	}
	cfg.Policy.AllowWrite = true
	if _, err := CallTool(ctx, cfg, "request", map[string]any{"method": "POST", "path": "/build", "body": `{"a":1}`}); err != nil || method != "POST" || body != `{"a":1}` {
		t.Fatalf("POST: %v %s %q", err, method, body)
	}
}

func TestCLIHelperProcess(t *testing.T) { testutil.CLIHelperProcess() }

func TestFormSessionCleanupReachesCallers(t *testing.T) {
	for _, call := range []string{"cli", "request", "download", "session-check"} {
		for _, logoutOK := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/logout=%v", call, logoutOK), func(t *testing.T) {
				var logouts atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/login":
						if r.Method == http.MethodPost {
							http.SetCookie(w, &http.Cookie{Name: "sid", Value: "session", Path: "/"})
						}
					case "/logout":
						logouts.Add(1)
						if !logoutOK {
							http.Error(w, "private session details", http.StatusForbidden)
							return
						}
						http.SetCookie(w, &http.Cookie{Name: "sid", Path: "/", MaxAge: -1})
					default:
						_, _ = w.Write([]byte("ok"))
					}
				}))
				t.Cleanup(srv.Close)
				t.Setenv("WORKCTL_TEST_SESSION_PASSWORD", "pw")
				t.Setenv("XDG_CACHE_HOME", t.TempDir())
				cfg := testConfig(srv.URL)
				cfg.Profiles["svc"] = config.Profile{Kind: Kind, URL: srv.URL, Auth: "form", Username: "me", TokenEnv: "WORKCTL_TEST_SESSION_PASSWORD", VerifyTLS: true, ReadOnly: true,
					FormLogin: &config.FormLogin{Path: "/login", UserField: "User", PasswordField: "Password", SessionCookie: "sid", LogoutPath: "/logout"}}
				var err error
				switch call {
				case "cli":
					err = Command(cfg, []string{"raw", "GET", "/home"})
				case "session-check":
					err = Command(cfg, []string{"session-check"})
					if err == nil || logouts.Load() != 0 {
						t.Fatalf("incomplete session-check: %v, logouts=%d", err, logouts.Load())
					}
					return
				default:
					_, err = CallTool(context.Background(), cfg, call, map[string]any{"path": "/home"})
				}
				if (err == nil) != logoutOK || logouts.Load() != 1 {
					t.Fatalf("err=%v, logouts=%d", err, logouts.Load())
				}
				if err != nil && strings.Contains(err.Error(), "private") {
					t.Fatal("logout exposed response contents")
				}
			})
		}
	}
}

func TestAPIRequestEncodesQuery(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { rawQuery = r.URL.RawQuery }))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Policy.AllowWrite = true
	query := map[string]any{
		"region":  []any{"eu-west", "eu-central"},
		"dry_run": false,
		"retries": float64(2),
	}
	if _, err := CallTool(context.Background(), cfg, "request", map[string]any{"method": "POST", "path": "/job/x/buildWithParameters", "query": query}); err != nil {
		t.Fatal(err)
	}
	want := "dry_run=false&region=eu-west&region=eu-central&retries=2"
	if rawQuery != want {
		t.Fatalf("query = %s\nwant    %s", rawQuery, want)
	}
	if _, err := CallTool(context.Background(), cfg, "request", map[string]any{"path": "/", "query": map[string]any{"a": map[string]any{}}}); err == nil {
		t.Fatal("nested object accepted as a query value")
	}
}

func TestAPIExecRunsOnlyAllowedQuotedCommands(t *testing.T) {
	calls := testutil.FakeCLI(t, "ok")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{"gerrit": {
		Kind: Kind, Backend: config.BackendSSHExec, Auth: "none", SSHDestination: "gerrit-prod",
		Commands: []config.SSHCommand{{Prefix: []string{"gerrit", "query"}, ReadOnly: true}, {Prefix: []string{"gerrit", "review"}}},
	}}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	call := func(args ...any) ([]byte, error) { return CallTool(ctx, cfg, "api_exec", map[string]any{"args": args}) }

	if _, err := call("gerrit", "query", "x; rm -rf ~", "it's"); err != nil {
		t.Fatal(err)
	}
	want := `ssh -o BatchMode=yes -o LogLevel=ERROR -T -- gerrit-prod 'gerrit' 'query' 'x; rm -rf ~' 'it'\''s'`
	if got := strings.Join((*calls)[0], " "); got != want {
		t.Fatalf("ssh call = %s\nwant       %s", got, want)
	}
	if _, err := call("gerrit", "ls-projects"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("command outside the allowlist: %v", err)
	}
	if _, err := call("gerrit"); err == nil || !strings.Contains(err.Error(), "not allowed") {
		t.Fatalf("shorter than a prefix: %v", err)
	}
	if _, err := call("gerrit", "review", "1,1", "--code-review", "+1"); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("write without allow_write: %v", err)
	}
	if _, err := CallTool(ctx, cfg, "request", map[string]any{"path": "/"}); err == nil || !strings.Contains(err.Error(), "api exec") {
		t.Fatalf("HTTP on an ssh-exec profile: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("refused commands reached ssh: %q", *calls)
	}
}
