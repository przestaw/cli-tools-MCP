package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	} {
		err := config.ValidateProfile("p", tc.profile)
		if (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.wantErr)
		}
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
