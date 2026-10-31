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
	if err := cmdAuth(cfgPath, "", []string{"set-token", "--profile", "gh", "--stdin"}); err != nil {
		t.Fatalf("set-token failed: %v", err)
	}

	// Verify token was stored in keyring
	account := secret.Account("github", "gh")
	val, err := tokenStore.Get(account)
	if err != nil || val != "my-secret-token" {
		t.Fatalf("token in keyring = %q, %v", val, err)
	}

	// Test status
	if err := cmdAuth(cfgPath, "", []string{"status", "--profile", "gh"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}

	// Test logout
	if err := cmdAuth(cfgPath, "", []string{"logout", "--profile", "gh"}); err != nil {
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
	if err := cmdAuth(cfgPath, "", []string{"status", "--profile", "gl"}); err != nil {
		t.Fatalf("status failed: %v", err)
	}
}

func TestAuthLoginBindsACLIProfileToItsAccount(t *testing.T) {
	fakeCLI(t, "user")
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	cfg := defaultConfig()
	cfg.Profiles = map[string]Profile{"gh": {Kind: "github", URL: "https://api.github.com", Backend: backendCLI, VerifyTLS: true}}
	cfg.path = cfgPath
	if err := cfg.save(); err != nil {
		t.Fatal(err)
	}

	if err := cmdAuth(cfgPath, "gh", []string{"login"}); err != nil { // the profile comes from the global --profile
		t.Fatal(err)
	}
	saved, err := loadConfig(cfgPath)
	if err != nil || saved.Profiles["gh"].Username != "przestaw" {
		t.Fatalf("profile not bound: %+v, %v", saved.Profiles["gh"], err)
	}
	if err := cmdAuth(cfgPath, "", []string{"status"}); err != nil {
		t.Fatal(err)
	}
	for _, sub := range []string{"set-token", "logout"} {
		if err := cmdAuth(cfgPath, "gh", []string{sub}); err == nil || !strings.Contains(err.Error(), "auth login") {
			t.Errorf("%s on a cli profile: %v", sub, err)
		}
	}

	other := saved.Profiles["gh"]
	other.Username = "someone-else"
	saved.Profiles["gh"] = other
	if err := saved.save(); err != nil {
		t.Fatal(err)
	}
	if err := cmdAuth(cfgPath, "gh", []string{"login"}); err == nil || !strings.Contains(err.Error(), "bound to") {
		t.Fatalf("login as another account: %v", err)
	}
	if again, _ := loadConfig(cfgPath); again.Profiles["gh"].Username != "someone-else" {
		t.Fatal("a refused login changed the profile")
	}
}
