package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
)

// Profile is one named provider connection; the profile name is also the keyring identity.
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
	Modules        ModulesConfig      `json:"modules"`
	Policy         PolicyConfig       `json:"policy"`
	MCP            MCPConfig          `json:"mcp"`
	path           string
	profile        string // --profile: replaces defaults.<kind> for the kind of that profile
}

func defaultConfig() *Config {
	return &Config{
		Version:        2,
		TimeoutSeconds: 30,
		Defaults:       map[string]string{},
		Profiles:       map[string]Profile{},
		Modules:        ModulesConfig{TimeoutSeconds: 120},
		Policy:         PolicyConfig{AllowWrite: false},
		MCP:            MCPConfig{MaxOutputBytes: 500000},
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
		return cfg, fmt.Errorf("config file %s does not exist; run `workctl --config %q init`", path, path)
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	var v1 struct{ Jira, GitLab, GitHub json.RawMessage }
	_ = json.Unmarshal(b, &v1)
	if cfg.Version != 2 || v1.Jira != nil || v1.GitLab != nil || v1.GitHub != nil {
		return nil, fmt.Errorf("config %s is not version 2 (top-level jira/gitlab/github are gone): move them to profiles, see config.example.json", path)
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

var prKinds = map[string]bool{"github": true, "gitlab": true, "azure": true}

// profileOf picks defaults.<kind>, else the only profile of that kind; an empty kind means any pull request provider.
func (c *Config) profileOf(kind string) (string, Profile, error) {
	if p, ok := c.Profiles[c.profile]; ok && (p.Kind == kind || kind == "" && prKinds[p.Kind]) {
		return c.profile, p, nil
	}
	if name := c.Defaults[kind]; name != "" {
		return name, c.Profiles[name], nil
	}
	var names []string
	for name, p := range c.Profiles {
		if p.Kind == kind || kind == "" && prKinds[p.Kind] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	switch len(names) {
	case 1:
		return names[0], c.Profiles[names[0]], nil
	case 0:
		if kind == "" {
			kind = "github, gitlab or azure"
		}
		return "", Profile{}, fmt.Errorf("%s is not configured: add a profile of that kind (see config.example.json)", kind)
	}
	return "", Profile{}, fmt.Errorf("several profiles match (%s); pass --profile or set defaults.<kind>", strings.Join(names, ", "))
}

func (c *Config) has(kind string) bool {
	for _, p := range c.Profiles {
		if p.Kind == kind {
			return true
		}
	}
	return false
}

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
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("profiles.%s.url must be an absolute http or https URL", name)
	}
	if parsed.Scheme == "http" && profile.VerifyTLS && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
		return fmt.Errorf("profiles.%s.url: http is only allowed for localhost or when verify_tls is false", name)
	}
	if profile.CAFile != "" && !filepath.IsAbs(os.ExpandEnv(profile.CAFile)) {
		return fmt.Errorf("profiles.%s.ca_file must be absolute", name)
	}
	// The first scheme is the default when auth is empty.
	auths := map[string][]string{"github": {"bearer"}, "gitlab": {"private-token", "bearer"}, "jira": {"bearer", "basic"}}[kind]
	if auth := strings.ToLower(profile.Auth); auth != "" && auths != nil && !slices.Contains(auths, auth) {
		return fmt.Errorf("profiles.%s.auth %q is unsupported for %s (use %s)", name, profile.Auth, kind, strings.Join(auths, " or "))
	}
	if strings.EqualFold(profile.Auth, "basic") && profile.Username == "" {
		return fmt.Errorf("profiles.%s.username is required for basic auth", name)
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
