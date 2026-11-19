// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config_test

import (
	"strings"
	"testing"

	"workctl/internal/config"
)

func selectionConfig() *config.Config {
	return &config.Config{
		Profiles: map[string]config.Profile{
			"gh":     {Kind: "github", Repos: []string{"org/*"}},
			"gh2":    {Kind: "github", Repos: []string{"other/*", "org/special"}},
			"gl":     {Kind: "gitlab", Projects: []string{"team"}},
			"az":     {Kind: "azure", Projects: []string{"Proj"}},
			"jira-a": {Kind: "jira", Projects: []string{"IR"}},
			"plain":  {Kind: "api"},
		},
	}
}

func TestKindCapabilities(t *testing.T) {
	tests := []struct {
		kind     string
		pr, http bool
	}{
		{"github", true, true},
		{"gitlab", true, true},
		{"azure", true, false},
		{"jira", false, true},
		{"atlassian", false, true},
		{"api", false, true},
		{"unknown", false, false},
	}
	for _, tc := range tests {
		if got := config.SupportsPR(tc.kind); got != tc.pr {
			t.Errorf("SupportsPR(%q) = %v, want %v", tc.kind, got, tc.pr)
		}
		if got := config.IsHTTPKind(tc.kind); got != tc.http {
			t.Errorf("IsHTTPKind(%q) = %v, want %v", tc.kind, got, tc.http)
		}
	}
}

func TestProfileOfSelection(t *testing.T) {
	tests := []struct {
		name     string
		cfg      func() *config.Config
		kind     string
		want     string
		errMatch string
	}{
		{"single profile of kind", selectionConfig, "gitlab", "gl", ""},
		{"several of kind", selectionConfig, "github", "", "several profiles match"},
		{"any pr provider is ambiguous", selectionConfig, "", "", "several profiles match"},
		{"none of kind", selectionConfig, "atlassian", "", "not configured"},
		{"default wins", func() *config.Config {
			c := selectionConfig()
			c.Defaults = map[string]string{"github": "gh2"}
			return c
		}, "github", "gh2", ""},
		{"explicit profile of kind wins over default", func() *config.Config {
			c := selectionConfig()
			c.Defaults = map[string]string{"github": "gh2"}
			c.SetProfileName("gh")
			return c
		}, "github", "gh", ""},
		{"explicit profile of other kind is skipped", func() *config.Config {
			c := selectionConfig()
			c.SetProfileName("gl")
			return c
		}, "azure", "az", ""},
	}
	for _, tc := range tests {
		name, _, err := tc.cfg().ProfileOf(tc.kind)
		if tc.errMatch != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errMatch) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.errMatch)
			}
			continue
		}
		if err != nil || name != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, name, err, tc.want)
		}
	}
}

func TestPRProfileExplicitName(t *testing.T) {
	t.Setenv("WORKCTL_PROFILE", "")
	tests := []struct {
		name, requested, kind, env, want, errMatch string
	}{
		{"named", "az", "", "", "az", ""},
		{"named wrong kind", "az", "github", "", "", "is azure, not github"},
		{"named unknown", "nope", "", "", "", "not configured"},
		{"environment name", "", "", "gl", "gl", ""},
		{"request beats environment", "az", "", "gl", "az", ""},
	}
	for _, tc := range tests {
		t.Setenv("WORKCTL_PROFILE", tc.env)
		name, _, err := selectionConfig().PRProfile(tc.requested, tc.kind)
		if tc.errMatch != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errMatch) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.errMatch)
			}
			continue
		}
		if err != nil || name != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, name, err, tc.want)
		}
	}
}

func TestResolveByResource(t *testing.T) {
	t.Setenv("WORKCTL_PROFILE", "")
	tests := []struct {
		name, requested, kind, resource, want, errMatch string
	}{
		{"owner wildcard", "", "github", "org/a", "gh", ""},
		{"exact beats wildcard", "", "github", "org/special", "gh2", ""},
		{"gitlab owner alias", "", "gitlab", "team/app", "gl", ""},
		{"azure owner alias", "", "azure", "proj/repo", "az", ""},
		{"explicit name skips matching", "gh2", "github", "org/a", "gh2", ""},
		{"no match falls back to kind", "", "gitlab", "unrelated/x", "gl", ""},
		{"no match and ambiguous", "", "github", "unrelated/x", "", "several profiles match"},
	}
	for _, tc := range tests {
		name, _, err := selectionConfig().PRProfileFor(tc.requested, tc.kind, tc.resource)
		if tc.errMatch != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errMatch) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.errMatch)
			}
			continue
		}
		if err != nil || name != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, name, err, tc.want)
		}
	}
}

func TestResolveForAnyKindAndTies(t *testing.T) {
	t.Setenv("WORKCTL_PROFILE", "")
	name, _, err := selectionConfig().ResolveFor("", "IR-12")
	if err != nil || name != "jira-a" {
		t.Fatalf("ResolveFor any kind = %q, %v", name, err)
	}
	cfg := selectionConfig()
	cfg.Profiles["gh3"] = config.Profile{Kind: "github", Repos: []string{"org/*"}}
	if _, _, err := cfg.ResolveFor("github", "org/a"); err == nil || !strings.Contains(err.Error(), "gh, gh3") {
		t.Fatalf("tie error = %v, want both profiles listed", err)
	}
}

func TestProfileRepository(t *testing.T) {
	tests := []struct {
		name     string
		profile  config.Profile
		arg      string
		want     string
		errMatch string
	}{
		{"argument wins", config.Profile{Kind: "github", DefaultRepo: "org/a"}, "x/y", "x/y", ""},
		{"default repo", config.Profile{Kind: "github", DefaultRepo: "org/a"}, "", "org/a", ""},
		{"wildcard default is not concrete", config.Profile{Kind: "github", DefaultRepo: "org/*"}, "", "", "repository is required"},
		{"single concrete repo", config.Profile{Kind: "github", Repos: []string{"org/*", "org/a"}}, "", "org/a", ""},
		{"two concrete repos", config.Profile{Kind: "github", Repos: []string{"org/a", "org/b"}}, "", "", "repository is required"},
		{"gitlab default project", config.Profile{Kind: "gitlab", DefaultProject: "g/p"}, "", "g/p", ""},
		{"gitlab single project", config.Profile{Kind: "gitlab", Projects: []string{"g/p"}}, "", "g/p", ""},
		{"github ignores projects", config.Profile{Kind: "github", Projects: []string{"g/p"}}, "", "", "repository is required"},
	}
	for _, tc := range tests {
		got, err := config.ProfileRepository(tc.profile, tc.arg)
		if tc.errMatch != "" {
			if err == nil || !strings.Contains(err.Error(), tc.errMatch) {
				t.Errorf("%s: err = %v, want %q", tc.name, err, tc.errMatch)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
