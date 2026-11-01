package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"workctl/internal/secret"
)

// TokenStore holds profile credentials under account "<kind>:<profile>".
var TokenStore = secret.Store{Service: secret.Service}

// ResolveProfileSecret returns token_env if set, else the keyring entry of the profile.
func ResolveProfileSecret(name string, profile Profile) (string, error) {
	if profile.UsesCLI() {
		return "", fmt.Errorf("profile %q uses the %s backend: workctl holds no token for it", name, CLITools[profile.Kind])
	}
	if profile.TokenEnv != "" {
		if value := os.Getenv(profile.TokenEnv); value != "" {
			return value, nil
		}
	}
	value, err := TokenStore.Get(secret.Account(profile.Kind, name))
	if err == nil && value != "" {
		return value, nil
	}
	message := fmt.Sprintf("no credential for profile %q: run `workctl auth set-token --profile %s --stdin`", name, name)
	if profile.TokenEnv != "" {
		message += fmt.Sprintf(" or set %s", profile.TokenEnv)
	}
	if err != nil && !errors.Is(err, secret.ErrNotFound) {
		message += " (" + strings.TrimSpace(err.Error()) + ")"
	}
	return "", errors.New(message)
}

// CLIManagedError explains why a cli profile has no workctl token to set or remove; nil for any other profile.
func CLIManagedError(name string, profile Profile) error {
	if !profile.UsesCLI() {
		return nil
	}
	tool := CLITools[profile.Kind]
	return fmt.Errorf("profile %q is logged in through %s: use `workctl auth login --profile %s` (to log out run `%s auth logout`)", name, tool, name, tool)
}
