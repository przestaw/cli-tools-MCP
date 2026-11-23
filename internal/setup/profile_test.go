// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workctl/internal/config"
	"workctl/internal/testutil"
)

func TestProfileAddWritesKindsThatNeededHandEditing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	add := func(args ...string) {
		t.Helper()
		if err := ProfileCommand(path, append([]string{"add"}, args...)); err != nil {
			t.Fatalf("profile add %v: %v", args, err)
		}
	}
	add("drive", "--kind", "google", "--service", "drive", "--client-id", "cid.apps.googleusercontent.com")
	add("api1", "--kind", "api", "--host", "service.example", "--auth", "header", "--auth-header", "X-Api-Key", "--username", "svc")
	add("ado", "--kind", "azure", "--url", "https://dev.azure.com/org", "--projects", "A, B")
	add("jira1", "--kind", "jira", "--host", "https://jira.example", "--auth", "basic", "--username", "me")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if p := cfg.Profiles["drive"]; !p.HasDrive() || p.OAuth2 == nil || p.OAuth2.ClientID != "cid.apps.googleusercontent.com" || p.Auth != "oauth2" {
		t.Fatalf("google profile = %+v", p)
	}
	if p := cfg.Profiles["api1"]; p.URL != "https://service.example" || p.AuthHeader != "X-Api-Key" || p.Username != "svc" || !p.VerifyTLS {
		t.Fatalf("api profile = %+v", p)
	}
	if p := cfg.Profiles["ado"]; p.Kind != "azure" || strings.Join(p.Projects, "|") != "A|B" {
		t.Fatalf("azure profile = %+v", p)
	}
	if p := cfg.Profiles["jira1"]; p.TokenEnv != "WORKCTL_JIRA_TOKEN" {
		t.Fatalf("jira profile keeps the wizard defaults: %+v", p)
	}
	// The first profile of a kind becomes its default.
	if cfg.Defaults["api"] != "drive" || cfg.Defaults["azure"] != "ado" || cfg.Defaults["jira"] != "jira1" {
		t.Fatalf("defaults = %v", cfg.Defaults)
	}
	// The file keeps the google declaration, not the expanded runtime profile.
	if saved := readFile(t, path); !strings.Contains(saved, `"kind": "google"`) {
		t.Fatalf("google profile not saved as declared:\n%s", saved)
	}
}

func TestProfileAddRefusesBadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	cases := map[string][]string{
		"no kind":       {"add", "x", "--host", "h.example"},
		"unknown kind":  {"add", "x", "--kind", "nope", "--host", "h.example"},
		"bad auth":      {"add", "x", "--kind", "github", "--auth", "basic", "--host", "h.example"},
		"no name":       {"add"},
		"stray word":    {"add", "x", "--kind", "api", "--host", "h.example", "extra"},
		"unknown flag":  {"add", "x", "--kind", "api", "--token", "secret"},
		"edit missing":  {"edit", "ghost", "--username", "u"},
		"unknown verb":  {"remove", "x"},
		"gitlab no url": {"add", "x", "--kind", "gitlab"},
	}
	for name, args := range cases {
		if err := ProfileCommand(path, args); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("a refused command wrote the config")
	}
	if err := ProfileCommand(path, []string{"add", "x", "--kind", "api", "--host", "h.example"}); err != nil {
		t.Fatal(err)
	}
	if err := ProfileCommand(path, []string{"add", "x", "--kind", "api", "--host", "h.example"}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("adding twice: %v", err)
	}
}

func TestProfileEditChangesOnlyTheGivenFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	for _, args := range [][]string{
		{"add", "gh", "--kind", "github", "--repos", "a/*,b/c", "--default-repo", "a/x", "--username", "me"},
		{"add", "gh2", "--kind", "github", "--host", "ghe.example/api/v3"},
	} {
		if err := ProfileCommand(path, args); err != nil {
			t.Fatal(err)
		}
	}
	if err := ProfileCommand(path, []string{"edit", "gh2", "--repos", "d/*", "--username", "you", "--default", "--json"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if p := cfg.Profiles["gh2"]; strings.Join(p.Repos, "|") != "d/*" || p.Username != "you" || p.URL != "https://ghe.example/api/v3" {
		t.Fatalf("gh2 = %+v", p)
	}
	if p := cfg.Profiles["gh"]; strings.Join(p.Repos, "|") != "a/*|b/c" || p.DefaultRepo != "a/x" || p.Username != "me" {
		t.Fatalf("gh changed: %+v", p)
	}
	if cfg.Defaults["github"] != "gh2" {
		t.Fatalf("defaults = %v", cfg.Defaults)
	}

	before := readFile(t, path)
	if err := ProfileCommand(path, []string{"edit", "gh", "--auth", "basic"}); err == nil || !strings.Contains(err.Error(), "profiles.gh.auth") {
		t.Fatalf("an edit that breaks the profile: %v", err)
	}
	if readFile(t, path) != before {
		t.Fatal("a refused edit changed the file")
	}
	if err := ProfileCommand(path, []string{"edit", "gh", "--repos", ""}); err != nil {
		t.Fatal(err)
	}
	if cfg, _ = config.Load(path); len(cfg.Profiles["gh"].Repos) != 0 {
		t.Fatalf("an empty list must clear: %+v", cfg.Profiles["gh"])
	}
}

func TestProfileJSONHidesIdentifiers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.json")
	out := testutil.CaptureStdout(t, func() {
		if err := ProfileCommand(path, []string{"add", "a", "--kind", "api", "--host", "h.example", "--token-env", "MY_SECRET_VAR", "--json"}); err != nil {
			t.Error(err)
		}
	})
	var res struct {
		Name    string
		Profile config.Profile
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.Name != "a" || res.Profile.TokenEnv != "***" {
		t.Fatalf("%v\n%s", err, out)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
