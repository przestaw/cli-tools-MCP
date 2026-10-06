package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
	"workctl/internal/secret"
)

func TestGitCredentialAnswersOnlyForItsHost(t *testing.T) {
	t.Setenv("TEST_GH_CRED", "tok")
	cfg := defaultConfig()
	cfg.Profiles["gh"] = Profile{Kind: "github", URL: "https://api.github.com", TokenEnv: "TEST_GH_CRED"}
	cfg.profile = "gh"
	get := func(host string) string {
		var out strings.Builder
		if err := cmdGitCredential(cfg, []string{"get"}, strings.NewReader("protocol=https\nhost="+host+"\n\n"), &out); err != nil {
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
	cfg.profile = ""
	if err := cmdGitCredential(cfg, []string{"get"}, strings.NewReader(""), &strings.Builder{}); err == nil {
		t.Fatal("a profile must be required")
	}
}

func TestAuthSetTokenAndStatusAndLogout(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	cfg := defaultConfig()
	cfg.Profiles = map[string]Profile{
		"gh": {Kind: "github", URL: "https://api.github.example"},
	}
	cfg.path = cfgPath
	if err := cfg.save(); err != nil {
		t.Fatal(err)
	}

	old := stdin
	stdin = strings.NewReader("my-secret-token\n")
	defer func() { stdin = old }()

	// Test set-token
	if err := cmdAuth(cfgPath, []string{"set-token", "--profile", "gh", "--stdin"}); err != nil {
		t.Fatalf("set-token failed: %v", err)
	}

	// Verify token was stored in keyring
	account := secret.Account("github", "gh")
	val, err := tokenStore.Get(account)
	if err != nil || val != "my-secret-token" {
		t.Fatalf("token in keyring = %q, %v", val, err)
	}

	// Test status
	if err := cmdAuth(cfgPath, []string{"status", "--profile", "gh"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	// Test logout
	if err := cmdAuth(cfgPath, []string{"logout", "--profile", "gh"}); err != nil {
		t.Fatalf("logout failed: %v", err)
	}

	// Verify token deleted
	val, err = tokenStore.Get(account)
	if err == nil {
		t.Fatalf("expected token to be deleted, got %q", val)
	}
}

func TestAuthStatusWithEnvVar(t *testing.T) {
	keyring.MockInit()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")

	cfg := defaultConfig()
	cfg.Profiles = map[string]Profile{
		"gl": {Kind: "gitlab", URL: "https://gitlab.example", TokenEnv: "TEST_GL_TOKEN_AUTH"},
	}
	cfg.path = cfgPath
	if err := cfg.save(); err != nil {
		t.Fatal(err)
	}

	t.Setenv("TEST_GL_TOKEN_AUTH", "env-secret")
	if err := cmdAuth(cfgPath, []string{"status", "--profile", "gl"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}
}
