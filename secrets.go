package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"workctl/internal/secret"
)

// tokenStore holds profile credentials under account "<kind>:<profile>".
var tokenStore = secret.Store{Service: secret.Service}

// resolveProfileSecret returns token_env if set, else the keyring entry of the profile.
func resolveProfileSecret(name string, profile Profile) (string, error) {
	if profile.usesCLI() {
		return "", fmt.Errorf("profile %q uses the %s backend: workctl holds no token for it", name, cliTools[profile.Kind])
	}
	if profile.TokenEnv != "" {
		if value := os.Getenv(profile.TokenEnv); value != "" {
			return value, nil
		}
	}
	value, err := tokenStore.Get(secret.Account(profile.Kind, name))
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
