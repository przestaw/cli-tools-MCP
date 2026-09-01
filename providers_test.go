package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestJiraAndGitLabAreOptional(t *testing.T) {
	cfg := defaultConfig()
	if cfg.hasJira() || cfg.hasGitLab() {
		t.Fatal("nothing should be configured by default")
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("an empty config must validate: %v", err)
	}
	if _, err := newJiraClient(cfg); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("jira client error = %v", err)
	}
	if _, err := newGitLabClient(cfg); err == nil || !strings.Contains(err.Error(), "not configured") {
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
	cfg.Jira.URL = "https://jira.example.test"
	got := names()
	if !got["jira_get_issue"] || got["gitlab_get_project"] || got["task_get_context"] {
		t.Fatalf("jira only, tools = %v", got)
	}
	cfg.GitLab.URL = "https://gitlab.example.test"
	if got := names(); !got["task_get_context"] || !got["gitlab_get_project"] {
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
	if cfg.Policy.AllowWrite || cfg.hasJira() || cfg.hasGitLab() || cfg.path != path {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("loadConfig must stay strict about a missing file")
	}
}

func TestSetupWithoutAnyEndpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := cmdSetup(path, []string{"--non-interactive"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.hasJira() || cfg.hasGitLab() {
		t.Fatalf("endpoints = %+v / %+v", cfg.Jira, cfg.GitLab)
	}
}
