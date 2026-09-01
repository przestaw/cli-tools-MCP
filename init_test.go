package main

import (
	"net/http"
	"net/http/httptest"
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
	if cfg.Jira.Token != "" {
		t.Fatal("the token must not be written to the config file")
	}
	got, err := resolveSecret("jira", cfg.Jira)
	if err != nil || got != "s3cret" {
		t.Fatalf("resolveSecret = %q, %v", got, err)
	}
}

func TestResolveSecretOrder(t *testing.T) {
	keyring.MockInit()
	ec := EndpointConfig{URL: "https://x.test", TokenEnv: "WORKCTL_TEST_TOKEN", Token: "legacy"}
	t.Setenv("WORKCTL_TEST_TOKEN", "")
	if v, _ := resolveSecret("jira", ec); v != "legacy" {
		t.Fatalf("config token last: %q", v)
	}
	if err := tokenStore.Set(keyringAccount("jira", ec), "ring"); err != nil {
		t.Fatal(err)
	}
	if v, _ := resolveSecret("jira", ec); v != "ring" {
		t.Fatalf("keyring before config token: %q", v)
	}
	t.Setenv("WORKCTL_TEST_TOKEN", "env")
	if v, _ := resolveSecret("jira", ec); v != "env" {
		t.Fatalf("env first: %q", v)
	}
	if _, err := resolveSecret("gitlab", EndpointConfig{URL: "https://x.test"}); err == nil || !strings.Contains(err.Error(), "workctl gitlab init") {
		t.Fatalf("missing token error = %v", err)
	}
}
