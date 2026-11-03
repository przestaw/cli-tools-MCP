// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/config"
	"workctl/internal/vcs"
)

func testConfig(base string) *config.Config {
	cfg := config.Default()
	cfg.Profiles["gh"] = config.Profile{Kind: "github", URL: base, TokenEnv: "TEST_GITHUB_TOKEN", VerifyTLS: true, DefaultRepo: "acme/app"}
	return cfg
}

func TestSecondAccountByProfile(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "first")
	t.Setenv("TEST_GITHUB_TOKEN_2", "second")
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization")+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	cfg.Profiles["work"] = config.Profile{Kind: "github", URL: srv.URL, TokenEnv: "TEST_GITHUB_TOKEN_2", VerifyTLS: true, DefaultRepo: "corp/svc"}
	cfg.Defaults["github"] = "gh"
	ctx := context.Background()
	for _, args := range []map[string]any{{}, {"profile": "work"}} {
		if _, err := CallTool(ctx, cfg, "github_get_repo", args); err != nil {
			t.Fatal(err)
		}
	}
	cfg.SetProfileName("work") // global --profile
	if _, err := repoOf(cfg, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := CallTool(ctx, cfg, "github_get_repo", nil); err != nil {
		t.Fatal(err)
	}
	want := "Bearer first /repos/acme/app|Bearer second /repos/corp/svc|Bearer second /repos/corp/svc"
	if strings.Join(got, "|") != want {
		t.Fatalf("requests = %v", got)
	}
	if _, err := CallTool(ctx, cfg, "github_get_repo", map[string]any{"profile": "nope"}); err == nil {
		t.Fatal("unknown profile must be rejected")
	}
}

func TestAuthHeadersAndPaths(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "gh-secret")
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		if got := r.Header.Get("Authorization"); got != "Bearer gh-secret" {
			t.Errorf("auth = %q", got)
		}
		if r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Error("missing API version header")
		}
		switch r.URL.Path {
		case "/repos/acme/app/pulls/7":
			_, _ = w.Write([]byte(`{"head":{"sha":"abc123"}}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	for _, tool := range []string{"github_get_pull_request", "github_get_pull_request_checks", "github_get_pull_request_comments"} {
		if _, err := CallTool(context.Background(), cfg, tool, map[string]any{"number": float64(7)}); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	want := []string{"/repos/acme/app/pulls/7", "/repos/acme/app/pulls/7", "/repos/acme/app/commits/abc123/check-runs?per_page=100", "/repos/acme/app/pulls/7/comments?per_page=100"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Fatalf("paths = %v", paths)
	}
}

func TestFileIsRawAndEscaped(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "x")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Accept"); got != "application/vnd.github.raw+json" {
			t.Errorf("accept = %q", got)
		}
		if got := r.URL.EscapedPath(); got != "/repos/acme/app/contents/docs/a%20b.md" {
			t.Errorf("path = %s", got)
		}
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	out, err := CallTool(context.Background(), testConfig(srv.URL), "github_get_file", map[string]any{"path": "docs/a b.md"})
	if err != nil || string(out) != "hello" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestRepoIsValidated(t *testing.T) {
	cfg := testConfig("https://api.example.test")
	for _, bad := range []string{"acme", "acme/app/extra", "../x", "a b/c"} {
		if _, err := repoOf(cfg, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	cfg.Profiles["gh"] = config.Profile{Kind: "github", URL: "https://api.example.test"}
	if _, err := repoOf(cfg, ""); err == nil {
		t.Error("missing repository accepted")
	}
}

func TestWritesNeedPolicy(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "x")
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"number":9}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	args := map[string]any{"head": "feat", "base": "main", "title": "T", "draft": true}
	if _, err := CallTool(context.Background(), cfg, "github_create_pull_request", args); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("write without policy: %v", err)
	}
	cfg.Policy.AllowWrite = true
	if _, err := CallTool(context.Background(), cfg, "github_create_pull_request", args); err != nil {
		t.Fatal(err)
	}
	if body["head"] != "feat" || body["base"] != "main" || body["draft"] != true {
		t.Fatalf("payload = %v", body)
	}
}

func TestReviewersAndEditCalls(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "secret-token")
	var recorded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded = append(recorded, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/repos/acme/app/pulls/149/requested_reviewers":
			_, _ = w.Write([]byte(`{"users":[{"login":"alice"}]}`))
		case "/repos/acme/app/pulls/149":
			_, _ = w.Write([]byte(`{"number":149,"title":"New title"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()

	cfg := testConfig(srv.URL)
	ctx := context.Background()

	// Read: get requested reviewers
	out, err := CallTool(ctx, cfg, "github_get_pull_request_reviewers", map[string]any{"number": float64(149)})
	if err != nil {
		t.Fatalf("get reviewers: %v", err)
	}
	if !strings.Contains(string(out), "alice") {
		t.Fatalf("expected alice, got %s", string(out))
	}

	// Writes require allow_write
	if _, err := CallTool(ctx, cfg, "github_request_reviewers", map[string]any{"number": float64(149), "reviewers": "alice,bob"}); err == nil {
		t.Fatal("request_reviewers must fail without allow_write")
	}

	cfg.Policy.AllowWrite = true

	if _, err := CallTool(ctx, cfg, "github_request_reviewers", map[string]any{"number": float64(149), "reviewers": "alice,bob"}); err != nil {
		t.Fatalf("request reviewers: %v", err)
	}

	editArgs := map[string]any{
		"number":        float64(149),
		"title":         "New title",
		"add_reviewers": "alice",
	}
	if _, err := CallTool(ctx, cfg, "github_edit_pull_request", editArgs); err != nil {
		t.Fatalf("edit pr: %v", err)
	}

	// CLI tests with reordered args (149 before flags)
	if err := Command(cfg, []string{"pr", "get", "149", "--repo", "acme/app"}); err != nil {
		t.Fatalf("cli get: %v", err)
	}
	if err := Command(cfg, []string{"pr", "reviewers", "149"}); err != nil {
		t.Fatalf("cli reviewers: %v", err)
	}
	if err := Command(cfg, []string{"pr", "edit", "149", "--add-reviewer", "alice", "--title", "Updated"}); err != nil {
		t.Fatalf("cli edit: %v", err)
	}

	wantPaths := []string{
		"GET /repos/acme/app/pulls/149/requested_reviewers",
		"POST /repos/acme/app/pulls/149/requested_reviewers",
		"PATCH /repos/acme/app/pulls/149",
		"POST /repos/acme/app/pulls/149/requested_reviewers",
		"GET /repos/acme/app/pulls/149",
		"GET /repos/acme/app/pulls/149/requested_reviewers",
		"PATCH /repos/acme/app/pulls/149",
		"POST /repos/acme/app/pulls/149/requested_reviewers",
	}
	if strings.Join(recorded, "; ") != strings.Join(wantPaths, "; ") {
		t.Fatalf("recorded requests mismatch:\ngot:  %v\nwant: %v", recorded, wantPaths)
	}
}

func TestPRReadRegistryMatchesCLIAndMCP(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "token")
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		_, _ = w.Write([]byte(`{"head":{"sha":"abc"}}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	for action, entry := range prReads {
		t.Run(action, func(t *testing.T) {
			paths = nil
			if err := Command(cfg, []string{"pr", action, "7", "--repo", "acme/app"}); err != nil {
				t.Fatal(err)
			}
			cliPaths := strings.Join(paths, "|")
			paths = nil
			if _, err := CallTool(context.Background(), cfg, entry.tool, map[string]any{"number": 7}); err != nil {
				t.Fatal(err)
			}
			if strings.Join(paths, "|") != cliPaths {
				t.Fatalf("CLI %s differs from MCP %v", cliPaths, paths)
			}
			found := false
			for _, tool := range Tools(cfg) {
				if tool.Name == entry.tool {
					found = true
				}
			}
			if !found {
				t.Fatalf("unadvertised tool %s", entry.tool)
			}
		})
	}
	for _, args := range [][]string{{"pr", "unknown"}, {"pr", "create"}, {"pr", "edit", "7", "--title", "new"}, {"pr", "comment", "add", "7", "--body", "text"}} {
		paths = nil
		if err := Command(cfg, args); err == nil {
			t.Fatalf("accepted %v", args)
		}
		if len(paths) != 0 {
			t.Fatalf("rejected action made a request: %v", paths)
		}
	}
}

func TestEditStopsOnFirstFailure(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "token")
	for _, failAt := range []string{"PATCH", "POST", "DELETE"} {
		t.Run(failAt, func(t *testing.T) {
			var methods []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				methods = append(methods, r.Method)
				if r.Method == failAt {
					w.WriteHeader(403)
				}
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()
			cfg := testConfig(srv.URL)
			cfg.Policy.AllowWrite = true
			_, err := CallTool(context.Background(), cfg, "github_edit_pull_request", map[string]any{
				"number": 7, "title": "new", "add_reviewers": "alice", "remove_reviewers": "bob",
			})
			if err == nil || methods[len(methods)-1] != failAt {
				t.Fatalf("methods=%v err=%v", methods, err)
			}
			want := map[string]string{"PATCH": "PATCH", "POST": "PATCH,POST", "DELETE": "PATCH,POST,DELETE"}[failAt]
			if strings.Join(methods, ",") != want {
				t.Fatal(methods)
			}
		})
	}
}

func TestNormalizePullRequest(t *testing.T) {
	res, err := vcs.Normalize("github", "octo/example", []byte(`{"number":4,"title":"Fix","body":"PR body text","state":"open","draft":true,"html_url":"https://github.example/pr/4","user":{"login":"octo"},"head":{"ref":"fix"},"base":{"ref":"main"}}`), fillPullRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != 4 || res.Items[0].SourceBranch != "fix" || res.Items[0].Description != "PR body text" {
		t.Fatalf("items = %#v", res.Items)
	}
	if _, err := vcs.Normalize("github", "octo/example", []byte(`{"title":"missing number"}`), fillPullRequest); err == nil {
		t.Fatal("expected a missing PR number to be rejected")
	}
}

func TestSinglePullRequestErrors(t *testing.T) {
	requestErr := errors.New("request failed")
	if _, err := vcs.Single("github", "o/r", "not found", nil, requestErr, fillPullRequest); !errors.Is(err, requestErr) {
		t.Fatal(err)
	}
	for _, body := range []string{`[]`, `null`, `not json`, `{}`, `{"number":1.5,"iid":1.5}`} {
		if _, err := vcs.Single("github", "o/r", "not found", []byte(body), nil, fillPullRequest); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
