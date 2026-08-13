// Package config loads and persists atl-cli configuration (YAML) and resolves
// the Atlassian API token via environment, OS keyring, or interactive prompt.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	"workctl/internal/secret"
)

// store keeps the tokens under the service name the standalone CLI used, so existing entries still resolve.
var store = secret.Store{Service: "atl-cli"}

// Profile holds the connection settings for one Atlassian Cloud site.
type Profile struct {
	URL       string `yaml:"url"`   // site base, e.g. https://your-team.atlassian.net
	Email     string `yaml:"email"` // Atlassian account email (Basic auth user)
	Timeout   string `yaml:"timeout"`
	VerifyTLS bool   `yaml:"verify_tls"`
}

// Config is the on-disk configuration document.
type Config struct {
	CurrentProfile string             `yaml:"current_profile"`
	Profiles       map[string]Profile `yaml:"profiles"`
	SavedJQL       map[string]string  `yaml:"saved_jql,omitempty"`
	SavedCQL       map[string]string  `yaml:"saved_cql,omitempty"`

	// path is the file this config was loaded from (not serialized).
	path string `yaml:"-"`
}

// Default returns an empty config with sane defaults.
func Default() *Config {
	return &Config{
		CurrentProfile: "default",
		Profiles:       map[string]Profile{},
		SavedJQL:       map[string]string{},
		SavedCQL:       map[string]string{},
	}
}

// DefaultPath returns the platform default config file path.
// Windows: %AppData%\atl\config.yaml, otherwise the OS user config dir.
func DefaultPath() (string, error) {
	if appData := os.Getenv("AppData"); appData != "" {
		return filepath.Join(appData, "atl", "config.yaml"), nil
	}
	home, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "atl", "config.yaml"), nil
}

// Load reads the config from path. A missing file yields a Default config
// (not an error) so first-run commands can still operate.
func Load(path string) (*Config, error) {
	if path == "" {
		p, err := DefaultPath()
		if err != nil {
			return nil, err
		}
		path = p
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		c := Default()
		c.path = path
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	c := Default()
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	if c.SavedJQL == nil {
		c.SavedJQL = map[string]string{}
	}
	if c.SavedCQL == nil {
		c.SavedCQL = map[string]string{}
	}
	c.path = path
	return c, nil
}

// Path returns the file the config is bound to.
func (c *Config) Path() string { return c.path }

// SetPath overrides the file the config will save to.
func (c *Config) SetPath(p string) { c.path = p }

// Save writes the config to its bound path, creating parent dirs as needed.
func (c *Config) Save() error {
	if c.path == "" {
		p, err := DefaultPath()
		if err != nil {
			return err
		}
		c.path = p
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, data, 0o600)
}

// ProfileName resolves the active profile name given an optional override.
// Precedence: explicit override > ATL_PROFILE env > config current_profile.
func (c *Config) ProfileName(override string) string {
	if override != "" {
		return override
	}
	if env := os.Getenv("ATL_PROFILE"); env != "" {
		return env
	}
	if c.CurrentProfile != "" {
		return c.CurrentProfile
	}
	return "default"
}

// Active returns the named profile (or the resolved active one if name == "").
func (c *Config) Active(name string) (Profile, string, error) {
	n := c.ProfileName(name)
	p, ok := c.Profiles[n]
	if !ok {
		return Profile{}, n, fmt.Errorf("profile %q not configured (run `atlassian config init`)", n)
	}
	return p, n, nil
}

// TimeoutDuration parses the profile timeout, defaulting to 30s.
func (p Profile) TimeoutDuration() time.Duration {
	if p.Timeout == "" {
		return 30 * time.Second
	}
	d, err := time.ParseDuration(p.Timeout)
	if err != nil {
		return 30 * time.Second
	}
	return d
}

// keyringAccount is the keyring key for a profile's API token.
func keyringAccount(profile, email string) string {
	return profile + ":" + email
}

// SetToken stores the API token for a profile in the OS keyring.
func SetToken(profile, email, token string) error {
	return store.Set(keyringAccount(profile, email), token)
}

// DeleteToken removes a stored API token from the keyring.
func DeleteToken(profile, email string) error {
	return store.Delete(keyringAccount(profile, email))
}

// ResolveToken returns the API token for a profile.
// Precedence: ATL_API_TOKEN env > keyring. Returns ErrNoToken if neither set.
func ResolveToken(profile, email string) (string, error) {
	if env := os.Getenv("ATL_API_TOKEN"); env != "" {
		return env, nil
	}
	tok, err := store.Get(keyringAccount(profile, email))
	if errors.Is(err, secret.ErrNotFound) {
		return "", ErrNoToken
	}
	if err != nil {
		return "", err
	}
	return tok, nil
}

// ErrNoToken indicates no API token is available for the profile.
var ErrNoToken = errors.New("no API token set (use `atlassian config set-token`, ATL_API_TOKEN env, or `atlassian config init`)")
