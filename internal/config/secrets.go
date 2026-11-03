// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

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
	if err := NoTokenError(name, profile); err != nil {
		return "", err
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

// NoTokenError explains why a profile has no workctl token to set, read or remove: gh or glab owns its login, or it
// uses auth "none" (an ssh-exec profile is authenticated by its SSH key). It is nil for any other profile.
func NoTokenError(name string, profile Profile) error {
	if profile.UsesCLI() {
		tool := CLITools[profile.Kind]
		return fmt.Errorf("profile %q is logged in through %s: use `workctl auth login --profile %s` (to log out run `%s auth logout`)", name, tool, name, tool)
	}
	if profile.UsesSSHExec() {
		return fmt.Errorf("profile %q is authenticated by its SSH key (ssh-agent and ~/.ssh/config for %s): workctl keeps no token for it", name, profile.SSHDestination)
	}
	if strings.EqualFold(profile.Auth, "none") {
		return fmt.Errorf("profile %q uses auth \"none\": workctl keeps no token for it", name)
	}
	return nil
}
