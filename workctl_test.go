package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConfig(base string) *Config {
	cfg := defaultConfig()
	cfg.Profiles = map[string]Profile{
		"jira":   {Kind: "jira", URL: base, TokenEnv: "TEST_JIRA_TOKEN", VerifyTLS: true},
		"gitlab": {Kind: "gitlab", URL: base, TokenEnv: "TEST_GITLAB_TOKEN", VerifyTLS: true, DefaultProject: "group/project", Projects: []string{"group/project"}},
	}
	return cfg
}

func TestGitLabInitWritesProfile(t *testing.T) {
	t.Setenv("WORKCTL_GITLAB_TOKEN", "x")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"17.0"}`))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "company.json")
	if err := cmdEndpointInit(path, "gitlab", []string{"--non-interactive", "--url", srv.URL, "--projects", "team/a,team/b"}, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["gitlab"]
	if p.Kind != "gitlab" || p.URL != srv.URL || p.DefaultProject != "team/a" || len(p.Projects) != 2 || cfg.Defaults["gitlab"] != "gitlab" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.Policy.AllowWrite {
		t.Fatal("writes should be disabled by default")
	}
}

func TestV1ConfigIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.json")
	for _, body := range []string{`{"version":1}`, `{"version":2,"jira":{"url":"https://jira.test"}}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), "version 2") {
			t.Fatalf("%s: err = %v", body, err)
		}
	}
}

func TestJiraBearerAuthAndIssuePath(t *testing.T) {
	t.Setenv("TEST_JIRA_TOKEN", "jira-secret")
	t.Setenv("TEST_GITLAB_TOKEN", "gl-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue/DEMO-123" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer jira-secret" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"DEMO-123"}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	c, err := newKindClient(cfg, "jira")
	if err != nil {
		t.Fatal(err)
	}
	b, err := jiraGetIssue(context.Background(), c, "DEMO-123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DEMO-123") {
		t.Fatalf("body = %s", b)
	}
}

func TestGitLabPrivateTokenAndEscapedProject(t *testing.T) {
	t.Setenv("TEST_JIRA_TOKEN", "jira-secret")
	t.Setenv("TEST_GITLAB_TOKEN", "gl-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Go decodes %2F into URL.Path; EscapedPath lets us verify the actual request target.
		if got := r.URL.EscapedPath(); got != "/api/v4/projects/group%2Fproject/merge_requests/42" {
			t.Errorf("escaped path = %s", got)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "gl-secret" {
			t.Errorf("token = %q", got)
		}
		_, _ = w.Write([]byte(`{"iid":42}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	c, err := newKindClient(cfg, "gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gitlabGetMR(context.Background(), c, "group/project", 42); err != nil {
		t.Fatal(err)
	}
}

func TestTaskContextAggregatesJiraBranchesAndMRs(t *testing.T) {
	t.Setenv("TEST_JIRA_TOKEN", "jira-secret")
	t.Setenv("TEST_GITLAB_TOKEN", "gl-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/"):
			_, _ = w.Write([]byte(`{"key":"DEMO-1","fields":{"summary":"x"}}`))
		case strings.HasSuffix(r.URL.Path, "/repository/branches"):
			_, _ = w.Write([]byte(`[{"name":"agent/DEMO-1"}]`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			_, _ = w.Write([]byte(`[{"iid":7,"title":"DEMO-1 fix"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	jc, _ := newKindClient(cfg, "jira")
	gc, _ := newKindClient(cfg, "gitlab")
	b, err := getTaskContext(context.Background(), cfg, jc, gc, "DEMO-1")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["key"] != "DEMO-1" {
		t.Fatalf("key = %#v", got["key"])
	}
	if _, ok := got["jira"]; !ok {
		t.Fatal("missing jira")
	}
	if _, ok := got["gitlab"]; !ok {
		t.Fatal("missing gitlab")
	}
}

func TestWritePolicy(t *testing.T) {
	cfg := defaultConfig()
	cfg.path = "x.json"
	if err := requireWrite(cfg); err == nil {
		t.Fatal("expected write to be blocked")
	}
	cfg.Policy.AllowWrite = true
	if err := requireWrite(cfg); err != nil {
		t.Fatal(err)
	}
}
