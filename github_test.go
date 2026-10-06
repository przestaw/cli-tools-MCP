package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func githubTestConfig(base string) *Config {
	cfg := defaultConfig()
	cfg.Profiles["gh"] = Profile{Kind: "github", URL: base, TokenEnv: "TEST_GITHUB_TOKEN", VerifyTLS: true, DefaultRepo: "acme/app"}
	return cfg
}

func TestGitHubSecondAccountByProfile(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "first")
	t.Setenv("TEST_GITHUB_TOKEN_2", "second")
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Header.Get("Authorization")+" "+r.URL.Path)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	cfg := githubTestConfig(srv.URL)
	cfg.Profiles["work"] = Profile{Kind: "github", URL: srv.URL, TokenEnv: "TEST_GITHUB_TOKEN_2", VerifyTLS: true, DefaultRepo: "corp/svc"}
	cfg.Defaults["github"] = "gh"
	ctx := context.Background()
	for _, args := range []map[string]any{{}, {"profile": "work"}} {
		if _, err := callMCPTool(ctx, cfg, nil, nil, "github_get_repo", args); err != nil {
			t.Fatal(err)
		}
	}
	cfg.profile = "work" // global --profile
	if _, err := ghRepo(cfg, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := callMCPTool(ctx, cfg, nil, nil, "github_get_repo", nil); err != nil {
		t.Fatal(err)
	}
	want := "Bearer first /repos/acme/app|Bearer second /repos/corp/svc|Bearer second /repos/corp/svc"
	if strings.Join(got, "|") != want {
		t.Fatalf("requests = %v", got)
	}
	if _, err := callMCPTool(ctx, cfg, nil, nil, "github_get_repo", map[string]any{"profile": "nope"}); err == nil {
		t.Fatal("unknown profile must be rejected")
	}
}

func TestGitHubAuthHeadersAndPaths(t *testing.T) {
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
	cfg := githubTestConfig(srv.URL)
	for _, tool := range []string{"github_get_pull_request", "github_get_pull_request_checks", "github_get_pull_request_comments"} {
		if _, err := callMCPTool(context.Background(), cfg, nil, nil, tool, map[string]any{"number": float64(7)}); err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
	want := []string{"/repos/acme/app/pulls/7", "/repos/acme/app/pulls/7", "/repos/acme/app/commits/abc123/check-runs?per_page=100", "/repos/acme/app/pulls/7/comments?per_page=100"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Fatalf("paths = %v", paths)
	}
}

func TestGitHubFileIsRawAndEscaped(t *testing.T) {
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
	out, err := callMCPTool(context.Background(), githubTestConfig(srv.URL), nil, nil, "github_get_file", map[string]any{"path": "docs/a b.md"})
	if err != nil || string(out) != "hello" {
		t.Fatalf("out=%q err=%v", out, err)
	}
}

func TestGitHubRepoIsValidated(t *testing.T) {
	cfg := githubTestConfig("https://api.example.test")
	for _, bad := range []string{"acme", "acme/app/extra", "../x", "a b/c"} {
		if _, err := ghRepo(cfg, bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	cfg.Profiles["gh"] = Profile{Kind: "github", URL: "https://api.example.test"}
	if _, err := ghRepo(cfg, ""); err == nil {
		t.Error("missing repository accepted")
	}
}

func TestGitHubWritesNeedPolicy(t *testing.T) {
	t.Setenv("TEST_GITHUB_TOKEN", "x")
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		_, _ = w.Write([]byte(`{"number":9}`))
	}))
	defer srv.Close()
	cfg := githubTestConfig(srv.URL)
	args := map[string]any{"head": "feat", "base": "main", "title": "T", "draft": true}
	if _, err := callMCPTool(context.Background(), cfg, nil, nil, "github_create_pull_request", args); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("write without policy: %v", err)
	}
	cfg.Policy.AllowWrite = true
	if _, err := callMCPTool(context.Background(), cfg, nil, nil, "github_create_pull_request", args); err != nil {
		t.Fatal(err)
	}
	if body["head"] != "feat" || body["base"] != "main" || body["draft"] != true {
		t.Fatalf("payload = %v", body)
	}
}

func TestGitHubToolsOnlyWhenConfigured(t *testing.T) {
	has := func(cfg *Config, name string) bool {
		for _, tool := range mcpTools(cfg) {
			if tool.Name == name {
				return true
			}
		}
		return false
	}
	cfg := defaultConfig()
	if has(cfg, "github_get_repo") {
		t.Fatal("github tool advertised without configuration")
	}
	cfg.Profiles["gh"] = Profile{Kind: "github", URL: "https://api.github.com"}
	if !has(cfg, "github_get_repo") || has(cfg, "github_create_pull_request") {
		t.Fatal("expected read tools only")
	}
	cfg.Policy.AllowWrite = true
	if !has(cfg, "github_create_pull_request") {
		t.Fatal("write tool missing with allow_write")
	}
}
