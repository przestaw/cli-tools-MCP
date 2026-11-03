// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package auth

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/secret"
	"workctl/internal/testutil"
)

func TestCLIHelperProcess(t *testing.T) { testutil.CLIHelperProcess() }

func TestGitCredentialAnswersOnlyForItsHost(t *testing.T) {
	t.Setenv("TEST_GH_CRED", "tok")
	cfg := config.Default()
	cfg.Profiles["gh"] = config.Profile{Kind: "github", URL: "https://api.github.com", TokenEnv: "TEST_GH_CRED"}
	cfg.SetProfileName("gh")
	get := func(host string) string {
		var out strings.Builder
		if err := GitCredential(cfg, []string{"get"}, strings.NewReader("protocol=https\nhost="+host+"\n\n"), &out); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := get("github.com"); got != "username=x-access-token\npassword=tok\n" {
		t.Fatalf("github.com: %q", got)
	}
	if got := get("evil.example"); got != "" {
		t.Fatalf("other host got a credential: %q", got)
	}
	cfg.SetProfileName("")
	if err := GitCredential(cfg, []string{"get"}, strings.NewReader(""), &strings.Builder{}); err == nil {
		t.Fatal("a profile must be required")
	}
}

func TestAuthSetTokenAndStatusAndLogout(t *testing.T) {
	keyring.MockInit()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"gh": {Kind: "github", URL: "https://api.github.example"},
	}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	old := cliutil.Stdin
	cliutil.Stdin = strings.NewReader("my-secret-token\n")
	defer func() { cliutil.Stdin = old }()

	if err := Command(cfgPath, "", []string{"set-token", "--profile", "gh", "--stdin"}); err != nil {
		t.Fatalf("set-token failed: %v", err)
	}

	account := secret.Account("github", "gh")
	val, err := config.TokenStore.Get(account)
	if err != nil || val != "my-secret-token" {
		t.Fatalf("token in keyring = %q, %v", val, err)
	}

	if err := Command(cfgPath, "", []string{"status", "--profile", "gh"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	if err := Command(cfgPath, "", []string{"logout", "--profile", "gh"}); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	val, err = config.TokenStore.Get(account)
	if err == nil {
		t.Fatalf("expected token to be deleted, got %q", val)
	}
}

func TestAuthStatusWithEnvVar(t *testing.T) {
	keyring.MockInit()
	cfgPath := filepath.Join(t.TempDir(), "config.json")

	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"gl": {Kind: "gitlab", URL: "https://gitlab.example", TokenEnv: "TEST_GL_TOKEN_AUTH"},
	}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_GL_TOKEN_AUTH", "env-secret")
	if err := Command(cfgPath, "", []string{"status", "--profile", "gl"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}
}

func TestAuthLoginBindsACLIProfileToItsAccount(t *testing.T) {
	testutil.FakeCLI(t, "user")
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{"gh": {Kind: "github", URL: "https://api.github.com", Backend: config.BackendCLI, VerifyTLS: true}}
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	if err := Command(cfgPath, "gh", []string{"login"}); err != nil { // the profile comes from the global --profile
		t.Fatal(err)
	}
	saved, err := config.Load(cfgPath)
	if err != nil || saved.Profiles["gh"].Username != "przestaw" {
		t.Fatalf("profile not bound: %+v, %v", saved.Profiles["gh"], err)
	}
	if err := Command(cfgPath, "", []string{"status"}); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"set-token", "logout"} {
		if err := Command(cfgPath, "gh", []string{sub}); err == nil || !strings.Contains(err.Error(), "auth login") {
			t.Errorf("%s on a cli profile: %v", sub, err)
		}
	}

	other := saved.Profiles["gh"]
	other.Username = "someone-else"
	saved.Profiles["gh"] = other
	if err := saved.Save(); err != nil {
		t.Fatal(err)
	}
	if err := Command(cfgPath, "gh", []string{"login"}); err == nil || !strings.Contains(err.Error(), "bound to") {
		t.Fatalf("login as another account: %v", err)
	}
	if again, _ := config.Load(cfgPath); again.Profiles["gh"].Username != "someone-else" {
		t.Fatal("a refused login changed the profile")
	}
}

func TestAccountsMCPToolListsResolvesAndBinds(t *testing.T) {
	keyring.MockInit()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"przestaw": {Kind: "github", URL: "https://api.github.com", Username: "przestaw", VerifyTLS: true},
		"work":     {Kind: "github", URL: "https://api.github.com", Username: "work-user", Repos: []string{"corp/*"}, VerifyTLS: true},
		"jira-ir":  {Kind: "jira", URL: "https://jira.example", Projects: []string{"IR"}, VerifyTLS: true},
	}
	cfg.Defaults["github"] = "work"
	cfg.SetPath(cfgPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := config.TokenStore.Set(secret.Account("github", "przestaw"), "super-secret-token"); err != nil {
		t.Fatal(err)
	}

	// Bind "przestaw/*" to profile "przestaw" and resolve "przestaw/cli-tools-MCP".
	out, err := CallTool(context.Background(), cfg, "accounts", map[string]any{
		"bind_profile": "przestaw",
		"bind_repo":    "przestaw/*",
		"resource":     "przestaw/cli-tools-MCP",
	})
	if err != nil {
		t.Fatalf("accounts bind: %v", err)
	}
	if strings.Contains(string(out), "super-secret-token") {
		t.Fatal("accounts output leaked the secret token")
	}
	var report AccountsReport
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatal(err)
	}
	if report.ResolvedProfile != "przestaw" {
		t.Fatalf("resolved_profile = %q, want przestaw", report.ResolvedProfile)
	}
	saved, err := config.Load(cfgPath)
	if err != nil || len(saved.Profiles["przestaw"].Repos) != 1 || saved.Profiles["przestaw"].Repos[0] != "przestaw/*" {
		t.Fatalf("saved repos = %+v, err = %v", saved.Profiles["przestaw"].Repos, err)
	}

	// Refuses binding a pattern already owned by another profile on the same host.
	if _, err := CallTool(context.Background(), cfg, "accounts", map[string]any{
		"bind_profile": "work",
		"bind_repo":    "przestaw/*",
	}); err == nil || !strings.Contains(err.Error(), "already bound") {
		t.Fatalf("expected conflict error, got %v", err)
	}

	// Resolves Jira issue key across kinds.
	out, err = CallTool(context.Background(), cfg, "accounts", map[string]any{"resource": "IR-101"})
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(out, &report)
	if report.ResolvedProfile != "jira-ir" {
		t.Fatalf("jira resolved_profile = %q, want jira-ir", report.ResolvedProfile)
	}
}
