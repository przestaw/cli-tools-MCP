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
	cfg.Jira.URL = base
	cfg.GitLab.URL = base
	cfg.Jira.TokenEnv = "TEST_JIRA_TOKEN"
	cfg.GitLab.TokenEnv = "TEST_GITLAB_TOKEN"
	cfg.GitLab.DefaultProject = "group/project"
	cfg.GitLab.Projects = []string{"group/project"}
	return cfg
}

func TestSetupNonInteractiveWritesConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "company.json")
	err := cmdSetup(path, []string{
		"--non-interactive",
		"--jira-url", "https://jira.example.test",
		"--gitlab-url", "https://gitlab.example.test",
		"--gitlab-projects", "team/a,team/b",
		"--gitlab-default-project", "team/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Jira.URL != "https://jira.example.test" {
		t.Fatalf("jira url = %q", cfg.Jira.URL)
	}
	if cfg.GitLab.URL != "https://gitlab.example.test" {
		t.Fatalf("gitlab url = %q", cfg.GitLab.URL)
	}
	if cfg.GitLab.DefaultProject != "team/a" || len(cfg.GitLab.Projects) != 2 {
		t.Fatalf("projects = %#v", cfg.GitLab)
	}
	if cfg.Policy.AllowWrite {
		t.Fatal("writes should be disabled by default")
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
	c, err := newJiraClient(cfg)
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
	c, err := newGitLabClient(cfg)
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
	jc, _ := newJiraClient(cfg)
	gc, _ := newGitLabClient(cfg)
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

func TestConfigShowDoesNotNeedTokenExpansion(t *testing.T) {
	cfg := defaultConfig()
	cfg.Jira.URL = "https://jira.example"
	cfg.GitLab.URL = "https://gitlab.example"
	cfg.Jira.Token = "literal-secret"
	cfg.GitLab.Token = "literal-gl-secret"
	cfg.path = filepath.Join(t.TempDir(), "cfg.json")
	if err := cfg.save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(cfg.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "literal-secret") {
		t.Fatal("save should support literal token")
	}
}
