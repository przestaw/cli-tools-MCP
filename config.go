package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Profile is one named provider connection. New unified commands use profiles
// exclusively; the profile name is also the stable keyring identity.
type Profile struct {
	Kind           string   `json:"kind"`
	URL            string   `json:"url"`
	Auth           string   `json:"auth,omitempty"`
	Username       string   `json:"username,omitempty"`
	TokenEnv       string   `json:"token_env,omitempty"`
	VerifyTLS      bool     `json:"verify_tls"`
	CAFile         string   `json:"ca_file,omitempty"`
	DefaultRepo    string   `json:"default_repo,omitempty"`
	DefaultProject string   `json:"default_project,omitempty"`
	Repos          []string `json:"repos,omitempty"`
	Projects       []string `json:"projects,omitempty"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty"`
}

func (p Profile) endpoint() EndpointConfig {
	return EndpointConfig{
		URL:       p.URL,
		Auth:      p.Auth,
		Username:  p.Username,
		TokenEnv:  p.TokenEnv,
		VerifyTLS: p.VerifyTLS,
		CAFile:    p.CAFile,
	}
}

type EndpointConfig struct {
	URL       string `json:"url"`
	Auth      string `json:"auth,omitempty"`
	Username  string `json:"username,omitempty"`
	Token     string `json:"token,omitempty"`
	TokenEnv  string `json:"token_env,omitempty"`
	VerifyTLS bool   `json:"verify_tls"`
	CAFile    string `json:"ca_file,omitempty"`
}

type GitLabConfig struct {
	EndpointConfig
	DefaultProject string   `json:"default_project,omitempty"`
	Projects       []string `json:"projects,omitempty"`
}

// GitHubConfig URL is the API base, for example https://api.github.com.
type GitHubConfig struct {
	EndpointConfig
	DefaultRepo string   `json:"default_repo,omitempty"`
	Repos       []string `json:"repos,omitempty"`
}

type PolicyConfig struct {
	AllowWrite bool `json:"allow_write"`
}

type MCPConfig struct {
	MaxOutputBytes int `json:"max_output_bytes"`
}

// ModulesConfig applies to the CLIs compiled into the binary (azure, atlassian). They keep their own config and credentials.
type ModulesConfig struct {
	TimeoutSeconds int `json:"timeout_seconds"` // per MCP call and doctor check
}

type Config struct {
	Version        int                `json:"version"`
	TimeoutSeconds int                `json:"timeout_seconds"`
	Defaults       map[string]string  `json:"defaults,omitempty"`
	Profiles       map[string]Profile `json:"profiles,omitempty"`
	Jira           EndpointConfig     `json:"jira"`
	GitLab         GitLabConfig       `json:"gitlab"`
	GitHub         GitHubConfig       `json:"github"`
	Modules        ModulesConfig      `json:"modules"`
	Policy         PolicyConfig       `json:"policy"`
	MCP            MCPConfig          `json:"mcp"`
	path           string
}

func defaultConfig() *Config {
	return &Config{
		Version:        2,
		TimeoutSeconds: 30,
		Defaults:       map[string]string{},
		Profiles:       map[string]Profile{},
		Jira: EndpointConfig{
			Auth:      "bearer",
			TokenEnv:  "WORKCTL_JIRA_TOKEN",
			VerifyTLS: true,
		},
		GitLab: GitLabConfig{
			EndpointConfig: EndpointConfig{
				Auth:      "private-token",
				TokenEnv:  "WORKCTL_GITLAB_TOKEN",
				VerifyTLS: true,
			},
		},
		GitHub: GitHubConfig{
			EndpointConfig: EndpointConfig{
				Auth:      "bearer",
				TokenEnv:  "WORKCTL_GITHUB_TOKEN",
				VerifyTLS: true,
			},
		},
		Modules: ModulesConfig{TimeoutSeconds: 120},
		Policy:  PolicyConfig{AllowWrite: false},
		MCP:     MCPConfig{MaxOutputBytes: 500000},
	}
}

func defaultConfigPath() (string, error) {
	if p := os.Getenv("WORKCTL_CONFIG"); p != "" {
		return p, nil
	}
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "workctl", "config.json"), nil
		}
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "workctl", "config.json"), nil
}

func loadConfig(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = defaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	cfg := defaultConfig()
	cfg.path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, fmt.Errorf("config file %s does not exist; run `workctl --config %q setup`", path, path)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.path = path
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.MCP.MaxOutputBytes <= 0 {
		cfg.MCP.MaxOutputBytes = 500000
	}
	if cfg.Defaults == nil {
		cfg.Defaults = map[string]string{}
	}
	if cfg.Profiles == nil {
		cfg.Profiles = map[string]Profile{}
	}
	return cfg, nil
}

func loadConfigForSetup(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = defaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	cfg := defaultConfig()
	cfg.path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse existing config %s: %w", path, err)
	}
	cfg.path = path
	return cfg, nil
}

func (c *Config) save() error {
	if c.path == "" {
		p, err := defaultConfigPath()
		if err != nil {
			return err
		}
		c.path = p
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	c.Version = 2
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	return os.WriteFile(c.path, b, 0o600)
}

func normalizeBaseURL(s string) string {
	return strings.TrimRight(strings.TrimSpace(os.ExpandEnv(s)), "/")
}

func (c *Config) hasJira() bool   { return normalizeBaseURL(c.Jira.URL) != "" }
func (c *Config) hasGitLab() bool { return normalizeBaseURL(c.GitLab.URL) != "" }
func (c *Config) hasGitHub() bool { return normalizeBaseURL(c.GitHub.URL) != "" }

// validateConfig checks the endpoints that are configured. Jira and GitLab are each optional.
func validateConfig(cfg *Config) error {
	for name, profile := range cfg.Profiles {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("profile name cannot be empty")
		}
		if err := validateProfile(name, profile); err != nil {
			return err
		}
	}
	for kind, name := range cfg.Defaults {
		profile, ok := cfg.Profiles[name]
		if !ok {
			return fmt.Errorf("defaults.%s references unknown profile %q", kind, name)
		}
		if profile.Kind != kind {
			return fmt.Errorf("defaults.%s references profile %q of kind %q", kind, name, profile.Kind)
		}
	}
	if cfg.hasJira() {
		switch strings.ToLower(cfg.Jira.Auth) {
		case "bearer", "basic":
		default:
			return fmt.Errorf("unsupported jira.auth %q (use bearer or basic)", cfg.Jira.Auth)
		}
	}
	if cfg.hasGitHub() && strings.ToLower(cfg.GitHub.Auth) != "bearer" {
		return fmt.Errorf("unsupported github.auth %q (use bearer)", cfg.GitHub.Auth)
	}
	if cfg.hasGitLab() {
		switch strings.ToLower(cfg.GitLab.Auth) {
		case "private-token", "bearer":
		default:
			return fmt.Errorf("unsupported gitlab.auth %q (use private-token or bearer)", cfg.GitLab.Auth)
		}
	}
	return nil
}

func validateProfile(name string, profile Profile) error {
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	switch kind {
	case "github", "gitlab", "azure", "jira", "atlassian":
	default:
		return fmt.Errorf("profiles.%s.kind %q is unsupported", name, profile.Kind)
	}
	parsed, err := url.Parse(normalizeBaseURL(profile.URL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("profiles.%s.url must be an absolute https URL", name)
	}
	if profile.CAFile != "" && !filepath.IsAbs(os.ExpandEnv(profile.CAFile)) {
		return fmt.Errorf("profiles.%s.ca_file must be absolute", name)
	}
	if kind == "github" && profile.Auth != "" && strings.ToLower(profile.Auth) != "bearer" {
		return fmt.Errorf("profiles.%s.auth %q is unsupported for github", name, profile.Auth)
	}
	if (kind == "jira" || kind == "atlassian") && profile.DefaultRepo != "" {
		return fmt.Errorf("profiles.%s.default_repo is not valid for %s", name, kind)
	}
	return nil
}

// loadConfigOrDefault is loadConfig, except that a missing file yields the defaults (read-only, nothing configured).
func loadConfigOrDefault(path string) (*Config, error) {
	p := path
	if p == "" {
		var err error
		if p, err = defaultConfigPath(); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		cfg := defaultConfig()
		cfg.path = p
		return cfg, nil
	}
	return loadConfig(path)
}
