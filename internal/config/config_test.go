// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
	"workctl/internal/config"
	"workctl/internal/testutil"
)

func TestV1ConfigIsRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.json")
	for _, body := range []string{`{"version":1}`, `{"version":2,"jira":{"url":"https://jira.test"}}`} {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), "version 2") {
			t.Fatalf("%s: err = %v", body, err)
		}
	}
}

func TestMissingConfigFileMeansDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	cfg, err := config.LoadOrDefault(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Policy.AllowWrite || cfg.Has("jira") || cfg.Has("gitlab") || cfg.Path() != path {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("Load must stay strict about a missing file")
	}
}

func TestWritePolicy(t *testing.T) {
	cfg := config.Default()
	cfg.SetPath("x.json")
	if err := cfg.RequireWrite(); err == nil {
		t.Fatal("expected write to be blocked")
	}
	cfg.Policy.AllowWrite = true
	if err := cfg.RequireWrite(); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmationPoliciesAreMutuallyExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "conflicting-policy.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"policy":{"confirm_writes":true,"confirm_session_writes":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil || !strings.Contains(err.Error(), "cannot both") {
		t.Fatalf("load error = %v", err)
	}
}

func TestLoadKeepsUnusedMalformedProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	body := `{"version":2,"defaults":{"github":"selected"},"profiles":{"selected":{"kind":"github","url":"https://api.github.com"},"unused":{"kind":"github","url":"not a URL"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("load config with unused malformed profile: %v", err)
	}
	name, _, err := cfg.ProfileOf("github")
	if err != nil || name != "selected" {
		t.Fatalf("selected profile = %q, %v", name, err)
	}
}

func TestConfigExampleValidates(t *testing.T) {
	cfg, err := config.Load(filepath.Join(testutil.RepoRoot(t), "config.example.json"))
	if err != nil {
		t.Fatalf("load config.example.json: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("validate config.example.json: %v", err)
	}
	if len(cfg.Profiles) != 4 {
		t.Fatalf("expected 4 profiles in config.example.json, got %d", len(cfg.Profiles))
	}
}

func TestValidateBackend(t *testing.T) {
	ok := config.Profile{Kind: "github", URL: "https://api.github.com", Backend: "cli", VerifyTLS: true}
	with := func(edit func(*config.Profile)) config.Profile { p := ok; edit(&p); return p }
	for name, tc := range map[string]struct {
		profile config.Profile
		wantErr string
	}{
		"valid github":    {ok, ""},
		"valid gitlab":    {with(func(p *config.Profile) { p.Kind, p.URL = "gitlab", "https://gitlab.example.com" }), ""},
		"native default":  {with(func(p *config.Profile) { p.Backend, p.TokenEnv = "", "TOK" }), ""},
		"unknown backend": {with(func(p *config.Profile) { p.Backend = "ssh" }), "unsupported"},
		"jira":            {with(func(p *config.Profile) { p.Kind, p.URL = "jira", "https://jira.example.com" }), "only available for github and gitlab"},
		"url with path":   {with(func(p *config.Profile) { p.URL = "https://ghe.example.com/api/v3" }), "without a path"},
		"http":            {with(func(p *config.Profile) { p.URL, p.VerifyTLS = "http://localhost:3000", false }), "https://HOST"},
		"token env":       {with(func(p *config.Profile) { p.TokenEnv = "TOK" }), "token_env"},
		"ca file":         {with(func(p *config.Profile) { p.CAFile = os.TempDir() }), "ca_file"},
		"tls off":         {with(func(p *config.Profile) { p.VerifyTLS = false }), "verify_tls"},
		"cli with ssh":    {with(func(p *config.Profile) { p.SSHDestination = "bastion" }), "not used by the cli backend"},
		"ssh tunnel":      {with(func(p *config.Profile) { p.Backend, p.SSHDestination = "ssh-tunnel", "bastion" }), ""},
		"ssh no dest":     {with(func(p *config.Profile) { p.Backend = "ssh-tunnel" }), "ssh_destination must be"},
		"ssh option":      {with(func(p *config.Profile) { p.Backend, p.SSHDestination = "ssh-tunnel", "-oProxyCommand=x" }), "not starting with -"},
		"ssh space":       {with(func(p *config.Profile) { p.Backend, p.SSHDestination = "ssh-tunnel", "a b" }), "no spaces"},
		"ssh azure":       {with(func(p *config.Profile) { p.Kind, p.Backend, p.SSHDestination = "azure", "ssh-tunnel", "b" }), "only available"},
		"native with ssh": {with(func(p *config.Profile) { p.Backend, p.SSHDestination = "", "bastion" }), "needs backend"},
	} {
		err := config.ValidateProfile("p", tc.profile)
		if (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.wantErr)
		}
	}
}

func TestValidateAPIAuth(t *testing.T) {
	ok := config.Profile{Kind: "api", URL: "https://fleet.example.com", VerifyTLS: true}
	with := func(edit func(*config.Profile)) config.Profile { p := ok; edit(&p); return p }
	for name, tc := range map[string]struct {
		profile config.Profile
		wantErr string
	}{
		"default bearer":    {ok, ""},
		"none":              {with(func(p *config.Profile) { p.Auth = "none" }), ""},
		"basic":             {with(func(p *config.Profile) { p.Auth, p.Username = "basic", "me" }), ""},
		"basic no user":     {with(func(p *config.Profile) { p.Auth = "basic" }), "username is required"},
		"header":            {with(func(p *config.Profile) { p.Auth, p.AuthHeader = "header", "X-Api-Key" }), ""},
		"header no name":    {with(func(p *config.Profile) { p.Auth = "header" }), "auth_header is required"},
		"header bad name":   {with(func(p *config.Profile) { p.Auth, p.AuthHeader = "header", "X-Key: x" }), "not a valid header"},
		"name without auth": {with(func(p *config.Profile) { p.AuthHeader = "X-Api-Key" }), "not allowed otherwise"},
		"unknown auth":      {with(func(p *config.Profile) { p.Auth = "oauth" }), "unsupported for api"},
		"none on github":    {with(func(p *config.Profile) { p.Kind, p.Auth = "github", "none" }), "unsupported for github"},
		"csrf": {with(func(p *config.Profile) {
			p.CSRF = &config.CSRF{Path: "/token", Header: "X-CSRF-Token", TokenField: "token"}
		}), ""},
		"csrf two headers": {with(func(p *config.Profile) {
			p.CSRF = &config.CSRF{Path: "/t", Header: "X-T", HeaderField: "f", TokenField: "token"}
		}), "exactly one"},
		"csrf no path": {with(func(p *config.Profile) { p.CSRF = &config.CSRF{Header: "X-T", TokenField: "token"} }), "needs path"},
		"csrf on jira": {with(func(p *config.Profile) {
			p.Kind, p.CSRF = "jira", &config.CSRF{Path: "/token", Header: "X-CSRF-Token", TokenField: "token"}
		}), "only available for kind api"},
	} {
		err := config.ValidateProfile("p", tc.profile)
		if (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.wantErr)
		}
	}
}

func TestValidateSSHExec(t *testing.T) {
	ok := config.Profile{Kind: "api", Backend: "ssh-exec", Auth: "none", SSHDestination: "gerrit", Commands: []config.SSHCommand{{Prefix: []string{"gerrit", "query"}}}}
	with := func(edit func(*config.Profile)) config.Profile { p := ok; edit(&p); return p }
	for name, tc := range map[string]struct {
		profile config.Profile
		wantErr string
	}{
		"valid, no URL":    {ok, ""},
		"not api":          {with(func(p *config.Profile) { p.Kind = "gitlab" }), "only available for kind api"},
		"token auth":       {with(func(p *config.Profile) { p.Auth = "bearer" }), "auth \"none\""},
		"token env":        {with(func(p *config.Profile) { p.TokenEnv = "T" }), "auth \"none\""},
		"no commands":      {with(func(p *config.Profile) { p.Commands = nil }), "at least one"},
		"empty word":       {with(func(p *config.Profile) { p.Commands = []config.SSHCommand{{Prefix: []string{"a", ""}}} }), "non-empty words"},
		"bad destination":  {with(func(p *config.Profile) { p.SSHDestination = "-x" }), "not starting with -"},
		"commands on http": {config.Profile{Kind: "api", URL: "https://x.test", VerifyTLS: true, Commands: ok.Commands}, "needs backend"},
	} {
		err := config.ValidateProfile("p", tc.profile)
		if (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.wantErr)
		}
	}
	if _, err := config.ResolveProfileSecret("p", ok); err == nil || !strings.Contains(err.Error(), "SSH key") {
		t.Fatalf("ssh-exec profile resolved a token: %v", err)
	}
}

func TestPRProfileRequiresExplicitChoiceForMultipleProfiles(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"github.com": {Kind: "github", URL: "https://api.github.example"},
		"gitlab":     {Kind: "gitlab", URL: "https://gitlab.example"},
		"azure":      {Kind: "azure", URL: "https://dev.azure.example"},
	}
	if _, _, err := cfg.PRProfile("", ""); err == nil {
		t.Fatal("expected an explicit profile when several are configured")
	}
	name, profile, err := cfg.PRProfile("azure", "azure")
	if err != nil || name != "azure" || profile.Kind != "azure" {
		t.Fatalf("profile = %q / %#v / %v", name, profile, err)
	}
}

func TestResolveProfileSecretOrder(t *testing.T) {
	keyring.MockInit()
	p := config.Profile{Kind: "jira", URL: "https://x.test", TokenEnv: "WORKCTL_TEST_TOKEN"}
	t.Setenv("WORKCTL_TEST_TOKEN", "")
	if _, err := config.ResolveProfileSecret("j", p); err == nil || !strings.Contains(err.Error(), "auth set-token --profile j") {
		t.Fatalf("missing token error = %v", err)
	}
	if err := config.TokenStore.Set("jira:j", "ring"); err != nil {
		t.Fatal(err)
	}
	if v, _ := config.ResolveProfileSecret("j", p); v != "ring" {
		t.Fatalf("keyring: %q", v)
	}
	t.Setenv("WORKCTL_TEST_TOKEN", "env")
	if v, _ := config.ResolveProfileSecret("j", p); v != "env" {
		t.Fatalf("env first: %q", v)
	}
}

func TestResourceMatchingInReposAndProjects(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"przestaw": {Kind: "github", URL: "https://api.github.com", Repos: []string{"przestaw/*"}},
		"work":     {Kind: "github", URL: "https://api.github.com", Repos: []string{"corp/*", "przestaw/shared-repo"}, DefaultRepo: "corp/main"},
		"gl-root":  {Kind: "gitlab", URL: "https://gitlab.example", Projects: []string{"org/*"}},
		"gl-sub":   {Kind: "gitlab", URL: "https://gitlab.example", Projects: []string{"org/sub/*"}},
		"jira-ir":  {Kind: "jira", URL: "https://jira.example", Projects: []string{"IR"}},
		"jira-abc": {Kind: "jira", URL: "https://jira.example", Projects: []string{"ABC"}},
	}
	cfg.Defaults["github"] = "work"

	// Wildcard match picks "przestaw" even though defaults.github is "work".
	scoped, err := cfg.ScopedFor("", "github", "przestaw/cli-tools-MCP")
	if err != nil || scoped.ProfileName() != "przestaw" {
		t.Fatalf("przestaw/* match: profile=%q err=%v", scoped.ProfileName(), err)
	}
	// Exact repo match beats prefix wildcard ("przestaw/*").
	scoped, err = cfg.ScopedFor("", "github", "przestaw/shared-repo")
	if err != nil || scoped.ProfileName() != "work" {
		t.Fatalf("exact match over wildcard: profile=%q err=%v", scoped.ProfileName(), err)
	}
	// Explicit profile argument always wins over resource match.
	scoped, err = cfg.ScopedFor("work", "github", "przestaw/cli-tools-MCP")
	if err != nil || scoped.ProfileName() != "work" {
		t.Fatalf("explicit profile: profile=%q err=%v", scoped.ProfileName(), err)
	}
	// Unmatched resource falls back to defaults.github.
	scoped, err = cfg.ScopedFor("", "github", "other/repo")
	if err != nil {
		t.Fatal(err)
	}
	if name, _, err := scoped.ProfileOf("github"); err != nil || name != "work" {
		t.Fatalf("fallback to default: name=%q err=%v", name, err)
	}
	// Longer prefix wins in GitLab nested groups.
	scoped, err = cfg.ScopedFor("", "gitlab", "org/sub/service")
	if err != nil || scoped.ProfileName() != "gl-sub" {
		t.Fatalf("nested gitlab group: profile=%q err=%v", scoped.ProfileName(), err)
	}
	// Jira issue key resolves by project prefix.
	scoped, err = cfg.ScopedFor("", "jira", "IR-402")
	if err != nil || scoped.ProfileName() != "jira-ir" {
		t.Fatalf("jira issue key: profile=%q err=%v", scoped.ProfileName(), err)
	}
	// Wildcard entry in Repos does not act as a concrete default repository when repo argument is omitted.
	if _, err := config.ProfileRepository(cfg.Profiles["przestaw"], ""); err == nil {
		t.Fatal("wildcard entry must not be used as concrete default repository")
	}
}

func TestTimeoutResolution(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"version":2,"timeout_seconds":30,"timeouts":{"jira":90},
	  "profiles":{"j":{"kind":"jira","url":"https://jira.test"},
	              "slow":{"kind":"jira","url":"https://jira2.test","timeout_seconds":300},
	              "g":{"kind":"gitlab","url":"https://gitlab.test"}},
	  "defaults":{"jira":"j"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"profile override", cfg.ProfileTimeout(cfg.Profiles["slow"]), 300 * time.Second},
		{"kind override", cfg.ProfileTimeout(cfg.Profiles["j"]), 90 * time.Second},
		{"global fallback", cfg.ProfileTimeout(cfg.Profiles["g"]), 30 * time.Second},
		{"command uses default profile", cfg.CommandTimeout("jira"), 90 * time.Second},
		{"kind without profile", cfg.CommandTimeout("github"), 30 * time.Second},
		{"max over profiles", cfg.MaxTimeout(), 300 * time.Second},
	} {
		if c.got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, c.got, c.want)
		}
	}
	cfg.SetProfileName("slow")
	if got := cfg.CommandTimeout("jira"); got != 300*time.Second {
		t.Errorf("--profile: got %v, want 300s", got)
	}
}

func TestOldConfigKeepsGlobalTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"version":2,"timeout_seconds":45,"profiles":{"j":{"kind":"jira","url":"https://jira.test"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.CommandTimeout("jira"); got != 45*time.Second {
		t.Errorf("got %v, want 45s", got)
	}
}

func TestNegativeKindTimeoutIsRejected(t *testing.T) {
	cfg := config.Default()
	cfg.Timeouts = map[string]int{"jira": -1}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "timeouts.jira") {
		t.Fatalf("got %v", err)
	}
}
