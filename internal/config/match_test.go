package config_test

import (
	"testing"

	"workctl/internal/config"
)

func TestMatchResourceScores(t *testing.T) {
	tests := []struct {
		name     string
		profile  config.Profile
		resource string
		want     int
	}{
		{"empty resource", config.Profile{Kind: "github", Repos: []string{"org/a"}}, " / ", 0},
		{"exact repo", config.Profile{Kind: "github", Repos: []string{"org/a"}}, "ORG/a", 1000 + len("org/a")},
		{"default repo", config.Profile{Kind: "github", DefaultRepo: "org/a"}, "org/a", 1000 + len("org/a")},
		{"owner wildcard", config.Profile{Kind: "github", Repos: []string{"org/*"}}, "org/a", 1 + len("org/")},
		{"wildcard owner itself", config.Profile{Kind: "github", Repos: []string{"org/*"}}, "org", 1 + len("org/")},
		{"wildcard miss", config.Profile{Kind: "github", Repos: []string{"org/*"}}, "other/a", 0},
		{"github owner alias is not a match", config.Profile{Kind: "github", Repos: []string{"org"}}, "org/a", 0},
		{"gitlab owner alias", config.Profile{Kind: "gitlab", Projects: []string{"org"}}, "org/sub/a", 1 + len("org/")},
		{"azure owner alias", config.Profile{Kind: "azure", Projects: []string{"Proj"}}, "proj/repo", 1 + len("Proj/")},
		{"exact beats wildcard", config.Profile{Kind: "gitlab", Projects: []string{"org/*", "org/a"}}, "org/a", 1000 + len("org/a")},
		{"jira issue key", config.Profile{Kind: "jira", Projects: []string{"IR"}}, "ir-402", 1000 + len("IR")},
		{"jira wildcard key", config.Profile{Kind: "atlassian", Projects: []string{"IR-*"}}, "IR", 1000 + len("IR")},
		{"jira default project", config.Profile{Kind: "jira", DefaultProject: "ABC"}, "ABC-1", 1000 + len("ABC")},
		{"jira miss", config.Profile{Kind: "jira", Projects: []string{"IR"}}, "XY-1", 0},
	}
	for _, tc := range tests {
		if got := tc.profile.MatchResource(tc.resource); got != tc.want {
			t.Errorf("%s: MatchResource(%q) = %d, want %d", tc.name, tc.resource, got, tc.want)
		}
	}
}
