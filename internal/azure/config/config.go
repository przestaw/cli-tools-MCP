// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package config loads and persists ado-cli configuration (YAML) and resolves
// the Azure DevOps Personal Access Token via environment, OS keyring, or
// interactive prompt.
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

var store = secret.Store{Service: secret.Service}

// Profile holds the connection settings for one Azure DevOps organization.
type Profile struct {
	OrgURL    string `yaml:"org_url"` // e.g. https://dev.azure.com/your-org
	Project   string `yaml:"project"` // default team project (optional)
	Timeout   string `yaml:"timeout"`
	VerifyTLS bool   `yaml:"verify_tls"`
}

// Config is the on-disk configuration document.
type Config struct {
	CurrentProfile string             `yaml:"current_profile"`
	Profiles       map[string]Profile `yaml:"profiles"`

	// path is the file this config was loaded from (not serialized).
	path string `yaml:"-"`
}

// Default returns an empty config with sane defaults.
func Default() *Config {
	return &Config{
		CurrentProfile: "default",
		Profiles:       map[string]Profile{},
	}
}

// DefaultPath returns the platform default config file path.
// Windows: %AppData%\ado\config.yaml, otherwise the OS user config dir.
func DefaultPath() (string, error) {
	if appData := os.Getenv("AppData"); appData != "" {
		return filepath.Join(appData, "ado", "config.yaml"), nil
	}
	home, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "ado", "config.yaml"), nil
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
// Precedence: explicit override > ADO_PROFILE env > config current_profile.
func (c *Config) ProfileName(override string) string {
	if override != "" {
		return override
	}
	if env := os.Getenv("ADO_PROFILE"); env != "" {
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
		return Profile{}, n, fmt.Errorf("profile %q not configured (run `azure config init`)", n)
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

// keyringAccount is the keyring key for a profile's PAT.
func keyringAccount(profile string) string {
	return secret.Account("azure", profile)
}

// SetToken stores the PAT for a profile in the OS keyring.
func SetToken(profile, orgURL, token string) error {
	return store.Set(keyringAccount(profile), token)
}

// DeleteToken removes a stored PAT from the keyring.
func DeleteToken(profile, orgURL string) error {
	return store.Delete(keyringAccount(profile))
}

// ResolveToken returns the PAT for a profile.
// Precedence: ADO_PAT env > AZURE_DEVOPS_EXT_PAT env > keyring.
// Returns ErrNoToken if none is set.
func ResolveToken(profile, orgURL string) (string, error) {
	if env := os.Getenv("ADO_PAT"); env != "" {
		return env, nil
	}
	if env := os.Getenv("AZURE_DEVOPS_EXT_PAT"); env != "" {
		return env, nil
	}
	tok, err := store.Get(keyringAccount(profile))
	if errors.Is(err, secret.ErrNotFound) {
		return "", ErrNoToken
	}
	if err != nil {
		return "", err
	}
	return tok, nil
}

// ErrNoToken indicates no PAT is available for the profile.
var ErrNoToken = errors.New("no PAT set (use `azure config set-token`, ADO_PAT env, or `azure config init`)")
