// Package config holds the workctl configuration file: named provider profiles,
// per-kind defaults, the write policy and the credential resolution order
// (environment variable, then OS keyring). Everything else in workctl receives a
// *Config and never reads the file itself.
package config

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
	"time"
)

// BackendCLI is the Profile.Backend value that routes requests through gh or glab;
// an empty value or "native" selects net/http.
const BackendCLI = "cli"

// CLITools maps a profile kind to the executable that serves its cli backend.
var CLITools = map[string]string{"github": "gh", "gitlab": "glab"}

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
	Backend        string   `json:"backend,omitempty"` // "native" (default) or "cli": gh or glab sends the requests and username pins the account
}

// UsesCLI reports whether the profile sends its requests through gh or glab.
func (p Profile) UsesCLI() bool { return strings.EqualFold(p.Backend, BackendCLI) }

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

// Path is where the configuration was loaded from or will be saved to.
func (c *Config) Path() string { return c.path }

// SetPath overrides the file the configuration is saved to.
func (c *Config) SetPath(path string) { c.path = path }

// ProfileName is the profile selected with --profile, or "" for the per-kind defaults.
func (c *Config) ProfileName() string { return c.profile }

// SetProfileName selects a profile to use instead of defaults.<kind> for its kind.
func (c *Config) SetProfileName(name string) { c.profile = name }

func Default() *Config {
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

func DefaultPath() (string, error) {
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

func Load(path string) (*Config, error) {
	if path == "" {
		var err error
		path, err = DefaultPath()
		if err != nil {
			return nil, err
		}
	}
	cfg := Default()
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

// LoadOrDefault is Load, except that a missing file yields the defaults (read-only, nothing configured).
func LoadOrDefault(path string) (*Config, error) {
	p := path
	if p == "" {
		var err error
		if p, err = DefaultPath(); err != nil {
			return nil, err
		}
	}
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		cfg := Default()
		cfg.path = p
		return cfg, nil
	}
	return Load(path)
}

func (c *Config) Save() error {
	if c.path == "" {
		p, err := DefaultPath()
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

func NormalizeBaseURL(s string) string {
	return strings.TrimRight(strings.TrimSpace(os.ExpandEnv(s)), "/")
}

// PRKinds are the profile kinds that can serve pull or merge requests.
var PRKinds = map[string]bool{"github": true, "gitlab": true, "azure": true}

// ProfileOf picks defaults.<kind>, else the only profile of that kind; an empty kind means any pull request provider.
func (c *Config) ProfileOf(kind string) (string, Profile, error) {
	if p, ok := c.Profiles[c.profile]; ok && (p.Kind == kind || kind == "" && PRKinds[p.Kind]) {
		return c.profile, p, nil
	}
	if name := c.Defaults[kind]; name != "" {
		return name, c.Profiles[name], nil
	}
	var names []string
	for name, p := range c.Profiles {
		if p.Kind == kind || kind == "" && PRKinds[p.Kind] {
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

// PRProfile resolves the profile of a pull request command: the requested name, then --profile or
// WORKCTL_PROFILE, then the per-kind default. An empty requestedKind accepts any pull request provider.
func (c *Config) PRProfile(requestedName, requestedKind string) (string, Profile, error) {
	if requestedName == "" {
		requestedName = c.profile
		if requestedName == "" {
			requestedName = strings.TrimSpace(os.Getenv("WORKCTL_PROFILE"))
		}
	}
	if requestedName != "" {
		profile, ok := c.Profiles[requestedName]
		if !ok {
			return "", Profile{}, fmt.Errorf("profile %q is not configured", requestedName)
		}
		if requestedKind != "" && profile.Kind != requestedKind {
			return "", Profile{}, fmt.Errorf("profile %q is %s, not %s", requestedName, profile.Kind, requestedKind)
		}
		return requestedName, profile, nil
	}
	return c.ProfileOf(requestedKind)
}

// ProfileRepository picks the repository (or GitLab project) of a call: the argument, then the profile defaults.
func ProfileRepository(profile Profile, repository string) (string, error) {
	if repository != "" {
		return repository, nil
	}
	if profile.DefaultRepo != "" {
		return profile.DefaultRepo, nil
	}
	if len(profile.Repos) == 1 {
		return profile.Repos[0], nil
	}
	if profile.Kind == "gitlab" && profile.DefaultProject != "" {
		return profile.DefaultProject, nil
	}
	if profile.Kind == "gitlab" && len(profile.Projects) == 1 {
		return profile.Projects[0], nil
	}
	return "", fmt.Errorf("repository is required: set repo (--repo on the CLI) or default_repo in the profile")
}

func (c *Config) Has(kind string) bool {
	for _, p := range c.Profiles {
		if p.Kind == kind {
			return true
		}
	}
	return false
}

// ProfileTimeout is the request timeout of a profile, falling back to the global timeout.
func (c *Config) ProfileTimeout(profile Profile) time.Duration {
	seconds := profile.TimeoutSeconds
	if seconds <= 0 {
		seconds = c.TimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

// RequireWrite refuses an operation that changes something unless policy.allow_write is on.
func (c *Config) RequireWrite() error {
	if !c.Policy.AllowWrite {
		return fmt.Errorf("write operation blocked by policy; set policy.allow_write=true in %s", c.path)
	}
	return nil
}

// Scoped narrows the configuration to one named profile of the given kind; an empty name returns c itself.
func (c *Config) Scoped(profileName, kind string) (*Config, error) {
	if profileName == "" {
		return c, nil
	}
	if c.Profiles[profileName].Kind != kind {
		return nil, fmt.Errorf("profile %q is not a configured %s profile", profileName, kind)
	}
	scoped := *c
	scoped.profile = profileName
	return &scoped, nil
}

func (c *Config) Validate() error {
	for name, profile := range c.Profiles {
		if strings.TrimSpace(name) == "" {
			return fmt.Errorf("profile name cannot be empty")
		}
		if err := ValidateProfile(name, profile); err != nil {
			return err
		}
	}
	for kind, name := range c.Defaults {
		profile, ok := c.Profiles[name]
		if !ok {
			return fmt.Errorf("defaults.%s references unknown profile %q", kind, name)
		}
		if profile.Kind != kind {
			return fmt.Errorf("defaults.%s references profile %q of kind %q", kind, name, profile.Kind)
		}
	}
	return nil
}

func ValidateProfile(name string, profile Profile) error {
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	switch kind {
	case "github", "gitlab", "azure", "jira", "atlassian":
	default:
		return fmt.Errorf("profiles.%s.kind %q is unsupported", name, profile.Kind)
	}
	parsed, err := url.Parse(NormalizeBaseURL(profile.URL))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
		return fmt.Errorf("profiles.%s.url must be an absolute http or https URL", name)
	}
	if parsed.Scheme == "http" && profile.VerifyTLS && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
		return fmt.Errorf("profiles.%s.url: http is only allowed for localhost or when verify_tls is false", name)
	}
	if profile.CAFile != "" && !filepath.IsAbs(os.ExpandEnv(profile.CAFile)) {
		return fmt.Errorf("profiles.%s.ca_file must be absolute", name)
	}
	if err := validateBackend(name, profile, parsed); err != nil {
		return err
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

// validateBackend checks Profile.Backend against the settings that the CLI transport cannot honour.
// The CLI owns credentials and TLS, so token_env, ca_file and verify_tls=false are refused instead of ignored.
func validateBackend(name string, profile Profile, u *url.URL) error {
	switch strings.ToLower(profile.Backend) {
	case "", "native":
		return nil
	case BackendCLI:
	default:
		return fmt.Errorf("profiles.%s.backend %q is unsupported (use native or cli)", name, profile.Backend)
	}
	tool := CLITools[profile.Kind]
	switch {
	case tool == "":
		return fmt.Errorf("profiles.%s.backend cli is only available for github and gitlab", name)
	case u.Scheme != "https" || strings.Trim(u.Path, "/") != "":
		return fmt.Errorf("profiles.%s.url must be https://HOST without a path for the cli backend (%s adds the API path)", name, tool)
	case profile.TokenEnv != "" || profile.CAFile != "" || !profile.VerifyTLS:
		return fmt.Errorf("profiles.%s: the cli backend needs verify_tls=true and no token_env or ca_file (%s keeps its own credentials and TLS settings)", name, tool)
	}
	return nil
}
