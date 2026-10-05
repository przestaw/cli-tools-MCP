package main

import "testing"

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
	github, err := normalizePullRequests("github", "octo/example", []byte(`{"number":4,"title":"Fix","state":"open","draft":true,"html_url":"https://github.example/pr/4","user":{"login":"octo"},"head":{"ref":"fix"},"base":{"ref":"main"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(github.Items) != 1 || github.Items[0].ID != 4 || github.Items[0].SourceBranch != "fix" {
		t.Fatalf("github items = %#v", github.Items)
	}
	gitlab, err := normalizePullRequests("gitlab", "group/example", []byte(`[{"iid":7,"title":"Fix","state":"opened","draft":false,"web_url":"https://gitlab.example/mr/7","author":{"username":"octo"},"source_branch":"fix","target_branch":"main"}]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(gitlab.Items) != 1 || gitlab.Items[0].ID != 7 || gitlab.Items[0].TargetBranch != "main" {
		t.Fatalf("gitlab items = %#v", gitlab.Items)
	}
}

func TestNormalizePullRequestRejectsMissingID(t *testing.T) {
	if _, err := normalizePullRequests("github", "octo/example", []byte(`{"title":"missing number"}`)); err == nil {
		t.Fatal("expected a missing PR number to be rejected")
	}
}
