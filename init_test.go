package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
)

func TestJiraInitStoresTokenInKeyringNotInConfig(t *testing.T) {
	keyring.MockInit()
	t.Setenv("WORKCTL_JIRA_TOKEN", "")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer s3cret" {
			t.Errorf("auth = %q", got)
		}
		_, _ = w.Write([]byte(`{"displayName":"Alice"}`))
	}))
	defer srv.Close()

	old := stdin
	stdin = strings.NewReader("s3cret\n")
	defer func() { stdin = old }()
	path := filepath.Join(t.TempDir(), "c.json")
	if err := cmdEndpointInit(path, "jira", []string{"--non-interactive", "--token-stdin", "--url", srv.URL}, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "s3cret") {
		t.Fatal("the token must not be written to the config file")
	}
	got, err := resolveProfileSecret("jira", cfg.Profiles["jira"])
	if err != nil || got != "s3cret" {
		t.Fatalf("resolveProfileSecret = %q, %v", got, err)
	}
}

func TestResolveProfileSecretOrder(t *testing.T) {
	keyring.MockInit()
	p := Profile{Kind: "jira", URL: "https://x.test", TokenEnv: "WORKCTL_TEST_TOKEN"}
	t.Setenv("WORKCTL_TEST_TOKEN", "")
	if _, err := resolveProfileSecret("j", p); err == nil || !strings.Contains(err.Error(), "auth set-token --profile j") {
		t.Fatalf("missing token error = %v", err)
	}
	if err := tokenStore.Set("jira:j", "ring"); err != nil {
		t.Fatal(err)
	}
	if v, _ := resolveProfileSecret("j", p); v != "ring" {
		t.Fatalf("keyring: %q", v)
	}
	t.Setenv("WORKCTL_TEST_TOKEN", "env")
	if v, _ := resolveProfileSecret("j", p); v != "env" {
		t.Fatalf("env first: %q", v)
	}
}

func TestInitCLIBackendStoresNoTokenAndBindsTheAccount(t *testing.T) {
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), "c.json")
	args := []string{"--backend", "cli", "--non-interactive"}

	fakeCLI(t, "auth") // not logged in: nothing may be saved
	if err := cmdEndpointInit(path, "github", args, nil); err == nil || !strings.Contains(err.Error(), "auth login --profile github") {
		t.Fatalf("init without a login: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a failed init wrote the config")
	}

	fakeCLI(t, "user")
	if err := cmdEndpointInit(path, "github", args, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["github"]
	if p.Backend != backendCLI || p.Username != "przestaw" || p.TokenEnv != "" || !p.VerifyTLS || cfg.Defaults["github"] != "github" {
		t.Fatalf("profile = %+v, defaults = %v", p, cfg.Defaults)
	}
	if _, err := tokenStore.Get("github:github"); err == nil {
		t.Fatal("a cli profile must not store a token")
	}
}
