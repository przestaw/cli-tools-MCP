package main

import (
	"context"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJiraAndGitLabAreOptional(t *testing.T) {
	cfg := defaultConfig()
	if cfg.has("jira") || cfg.has("gitlab") {
		t.Fatal("nothing should be configured by default")
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("an empty config must validate: %v", err)
	}
	if _, err := newKindClient(cfg, "jira"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("jira client error = %v", err)
	}
	if _, err := newKindClient(cfg, "gitlab"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("gitlab client error = %v", err)
	}
}

func TestRootHTTPPoliciesAfterConsolidation(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "http://localhost"+strings.TrimPrefix(r.Host, "127.0.0.1")+"/target", 302)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/error" {
			w.WriteHeader(403)
		}
		w.Write([]byte(`{"message":"denied"}`))
	}))
	defer srv.Close()
	client, err := newAPIClient(Profile{URL: srv.URL, VerifyTLS: true}, "/api", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.endpoint("https://outside.test"); err == nil {
		t.Fatal("absolute URL accepted")
	}
	if endpoint, err := client.endpoint("items"); err != nil || endpoint != srv.URL+"/api/items" {
		t.Fatalf("%s %v", endpoint, err)
	}
	_, _, err = client.do(context.Background(), "GET", "/redirect", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "cross-host redirect") || requests != 1 {
		t.Fatalf("redirect: %v requests=%d", err, requests)
	}
	_, contentType, err := client.do(context.Background(), "GET", "/error", nil, nil, "")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != 403 || contentType != "application/json" {
		t.Fatalf("HTTP error changed: %v %s", err, contentType)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = client.do(ctx, "GET", "/cancel", nil, nil, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
}

func TestRootHTTPUsesCustomCA(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) }))
	defer srv.Close()
	file := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(file, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	client, err := newAPIClient(Profile{URL: srv.URL, VerifyTLS: true, CAFile: file}, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.do(context.Background(), "GET", "/", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := newAPIClient(Profile{URL: srv.URL, VerifyTLS: true, CAFile: file}, "", time.Second); err == nil {
		t.Fatal("invalid CA accepted")
	}
}

func TestOnlyConfiguredBackendsGetMCPTools(t *testing.T) {
	cfg := defaultConfig()
	names := func() map[string]bool {
		m := map[string]bool{}
		for _, tool := range mcpTools(cfg) {
			m[tool.Name] = true
		}
		return m
	}
	if got := names(); len(got) != 2 || !got["azure"] || !got["atlassian"] {
		t.Fatalf("no Jira/GitLab configured, expected only the embedded azure and atlassian tools, got %v", got)
	}
	cfg.Profiles["jira"] = Profile{Kind: "jira", URL: "https://jira.example.test"}
	got := names()
	if !got["jira_get_issue"] || got["gitlab_get_project"] || got["task_get_context"] || got["pr_list"] {
		t.Fatalf("jira only, tools = %v", got)
	}
	cfg.Profiles["gitlab"] = Profile{Kind: "gitlab", URL: "https://gitlab.example.test"}
	if got := names(); !got["task_get_context"] || !got["gitlab_get_project"] || !got["pr_list"] {
		t.Fatalf("both configured, tools = %v", got)
	}
	if _, err := callMCPTool(nil, defaultConfig(), "jira_get_issue", map[string]any{"key": "X-1"}); err == nil {
		t.Fatal("a tool of an unconfigured backend must be refused")
	}
}

func TestMissingConfigFileMeansDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	cfg, err := loadConfigOrDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Policy.AllowWrite || cfg.has("jira") || cfg.has("gitlab") || cfg.path != path {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig must stay strict about a missing file")
	}
}
