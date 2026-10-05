package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"workctl/internal/secret"
)

// tokenStore holds the credentials of the Jira and GitLab modules. Azure DevOps and Atlassian Cloud keep theirs
// under the service names of their original CLIs so that tokens stored earlier still resolve.
var tokenStore = secret.Store{Service: secret.Service}

func keyringAccount(module string, ec EndpointConfig) string {
	return secret.Account(module, normalizeBaseURL(ec.URL))
}

// resolveSecret returns the credential of a module: the environment variable named by token_env, then the OS
// keyring (written by `workctl <module> init`), then a token in the config file (legacy, plaintext).
func resolveSecret(module string, ec EndpointConfig) (string, error) {
	if ec.TokenEnv != "" {
		if v := os.Getenv(ec.TokenEnv); v != "" {
			return v, nil
		}
	}
	v, err := tokenStore.Get(keyringAccount(module, ec))
	if err == nil && v != "" {
		return v, nil
	}
	keyringErr := err
	if ec.Token != "" {
		return os.ExpandEnv(ec.Token), nil
	}
	msg := fmt.Sprintf("no credential for %s: run `workctl %s init`", module, module)
	if ec.TokenEnv != "" {
		msg += fmt.Sprintf(" or set %s", ec.TokenEnv)
	}
	if keyringErr != nil && !errors.Is(keyringErr, secret.ErrNotFound) {
		msg += " (" + strings.TrimSpace(keyringErr.Error()) + ")"
	}
	return "", errors.New(msg)
}

// resolveProfileSecret resolves credentials for a v2 profile without consulting
// endpoint-specific or legacy keyring entries.
func resolveProfileSecret(name string, profile Profile) (string, error) {
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
