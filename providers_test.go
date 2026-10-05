package main

import (
	"path/filepath"
	"strings"
	"testing"
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
	if _, err := callMCPTool(nil, defaultConfig(), nil, nil, "jira_get_issue", map[string]any{"key": "X-1"}); err == nil {
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
