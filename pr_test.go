package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/vcs"
)

func TestPRProfileRequiresExplicitChoiceForMultipleProfiles(t *testing.T) {
	cfg := defaultConfig()
	cfg.Profiles = map[string]Profile{
		"github.com": {Kind: "github", URL: "https://api.github.example"},
		"gitlab":     {Kind: "gitlab", URL: "https://gitlab.example"},
		"azure":      {Kind: "azure", URL: "https://dev.azure.example"},
	}
	if _, _, err := prProfile(cfg, "", ""); err == nil {
		t.Fatal("expected an explicit profile when several are configured")
	}
	name, profile, err := prProfile(cfg, "azure", "azure")
	if err != nil || name != "azure" || profile.Kind != "azure" {
		t.Fatalf("profile = %q / %#v / %v", name, profile, err)
	}
}

func TestNormalizePullRequestsSupportsGitHubAndGitLab(t *testing.T) {
	github, err := normalizePullRequests("github", "octo/example", []byte(`{"number":4,"title":"Fix","body":"PR body text","state":"open","draft":true,"html_url":"https://github.example/pr/4","user":{"login":"octo"},"head":{"ref":"fix"},"base":{"ref":"main"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(github.Items) != 1 || github.Items[0].ID != 4 || github.Items[0].SourceBranch != "fix" || github.Items[0].Description != "PR body text" {
		t.Fatalf("github items = %#v", github.Items)
	}
	gitlab, err := normalizePullRequests("gitlab", "group/example", []byte(`[{"iid":7,"title":"Fix","description":"MR desc text","state":"opened","draft":false,"web_url":"https://gitlab.example/mr/7","author":{"username":"octo"},"source_branch":"fix","target_branch":"main"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(gitlab.Items) != 1 || gitlab.Items[0].ID != 7 || gitlab.Items[0].TargetBranch != "main" || gitlab.Items[0].Description != "MR desc text" {
		t.Fatalf("gitlab items = %#v", gitlab.Items)
	}
}

func TestNormalizePullRequestRejectsMissingID(t *testing.T) {
	if _, err := normalizePullRequests("github", "octo/example", []byte(`{"title":"missing number"}`)); err == nil {
		t.Fatal("expected a missing PR number to be rejected")
	}
}

func TestConfigExampleValidates(t *testing.T) {
	cfg, err := loadConfig("config.example.json")
	if err != nil {
		t.Fatalf("load config.example.json: %v", err)
	}
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("validate config.example.json: %v", err)
	}
	if len(cfg.Profiles) != 4 {
		t.Fatalf("expected 4 profiles in config.example.json, got %d", len(cfg.Profiles))
	}
}

func TestPullRequestRequestsPerProvider(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case strings.Contains(r.URL.Path, "/_apis/git/pullrequests/"):
			_, _ = w.Write([]byte(`{"pullRequestId":5,"title":"one"}`))
		case strings.HasSuffix(r.URL.Path, "/_apis/git/pullrequests"):
			_, _ = w.Write([]byte(`{"value":[{"pullRequestId":5,"title":"one"}]}`))
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_, _ = w.Write([]byte(`[{"number":1,"merged_at":"2026-01-01T00:00:00Z"},{"number":2,"merged_at":null}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	t.Setenv("TEST_TOKEN", "x")
	cfg := defaultConfig()
	cfg.Profiles = map[string]Profile{
		"az": {Kind: "azure", URL: srv.URL + "/org", TokenEnv: "TEST_TOKEN", VerifyTLS: true, DefaultProject: "Def"},
		"gh": {Kind: "github", URL: srv.URL, TokenEnv: "TEST_TOKEN", VerifyTLS: true, DefaultRepo: "o/r"},
		"gl": {Kind: "gitlab", URL: srv.URL, TokenEnv: "TEST_TOKEN", VerifyTLS: true, DefaultProject: "g/p"},
	}
	fetch := func(name, project, state string, id int) []vcs.PullRequest {
		t.Helper()
		res, err := fetchPullRequests(context.Background(), cfg, name, cfg.Profiles[name], "", project, "", state, id)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res.Items
	}
	fetch("az", "", "merged", 0)
	fetch("az", "Other", "", 5)
	if items := fetch("gh", "", "closed", 0); len(items) != 1 || items[0].ID != 2 {
		t.Fatalf("closed must exclude merged: %#v", items)
	}
	fetch("gl", "", "open", 0)
	want := []string{
		"/org/Def/_apis/git/pullrequests?", "searchCriteria.status=completed",
		"/org/Other/_apis/git/pullrequests/5?",
		"/repos/o/r/pulls?", "state=closed",
		"/api/v4/projects/g/p/merge_requests?", "state=opened",
	}
	all := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing %q in requests:\n%s", w, all)
		}
	}
	if _, err := fetchPullRequests(context.Background(), cfg, "gl", cfg.Profiles["gl"], "", "", "", "opened", 0); err == nil {
		t.Fatal("provider-specific state must be rejected")
	}
}

func TestMCPProfileToolsRegisteredAndCallable(t *testing.T) {
	cfg := defaultConfig()
	cfg.Profiles = map[string]Profile{
		"gh": {
			Kind:        "github",
			URL:         "https://api.github.test",
			DefaultRepo: "owner/repo",
		},
	}
	tools := mcpTools(cfg)
	hasTool := func(name string) bool {
		for _, tool := range tools {
			if tool.Name == name {
				return true
			}
		}
		return false
	}
	if !hasTool("pr_list") || !hasTool("pr_show") {
		t.Fatalf("expected pr_list and pr_show tools when profiles exist, got %v", tools)
	}
}
