// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package setup

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/testutil"
)

func TestCLIHelperProcess(t *testing.T) { testutil.CLIHelperProcess() }

func TestGitLabInitWritesProfile(t *testing.T) {
	t.Setenv("WORKCTL_GITLAB_TOKEN", "x")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"version":"17.0"}`))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "company.json")
	if err := EndpointInit(path, "gitlab", []string{"--non-interactive", "--url", srv.URL, "--projects", "team/a,team/b"}, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["gitlab"]
	if p.Kind != "gitlab" || p.URL != srv.URL || p.DefaultProject != "team/a" || len(p.Projects) != 2 || cfg.Defaults["gitlab"] != "gitlab" {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.Policy.AllowWrite {
		t.Fatal("writes should be disabled by default")
	}
}

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

	old := cliutil.Stdin
	cliutil.Stdin = strings.NewReader("s3cret\n")
	defer func() { cliutil.Stdin = old }()
	path := filepath.Join(t.TempDir(), "c.json")
	if err := EndpointInit(path, "jira", []string{"--non-interactive", "--token-stdin", "--url", srv.URL}, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), "s3cret") {
		t.Fatal("the token must not be written to the config file")
	}
	got, err := config.ResolveProfileSecret("jira", cfg.Profiles["jira"])
	if err != nil || got != "s3cret" {
		t.Fatalf("ResolveProfileSecret = %q, %v", got, err)
	}
}

func TestInitCLIBackendStoresNoTokenAndBindsTheAccount(t *testing.T) {
	keyring.MockInit()
	path := filepath.Join(t.TempDir(), "c.json")
	args := []string{"--backend", "cli", "--non-interactive"}

	testutil.FakeCLI(t, "auth") // not logged in: nothing may be saved
	if err := EndpointInit(path, "github", args, nil); err == nil || !strings.Contains(err.Error(), "auth login --profile github") {
		t.Fatalf("init without a login: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("a failed init wrote the config")
	}

	testutil.FakeCLI(t, "user")
	if err := EndpointInit(path, "github", args, nil); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.Profiles["github"]
	if p.Backend != config.BackendCLI || p.Username != "przestaw" || p.TokenEnv != "" || !p.VerifyTLS || cfg.Defaults["github"] != "github" {
		t.Fatalf("profile = %+v, defaults = %v", p, cfg.Defaults)
	}
	if _, err := config.TokenStore.Get("github:github"); err == nil {
		t.Fatal("a cli profile must not store a token")
	}
}
