package main

import (
	"errors"
	"fmt"
	"os"

	"workctl/internal/secret"
)

var credentialStore = secret.Store{Service: "workctl"}

func resolveSecret(cfg *Config) (string, error) {
	if cfg.TokenEnv != "" {
		if value := os.Getenv(cfg.TokenEnv); value != "" {
			return value, nil
		}
	}
	value, err := credentialStore.Get(cfg.TokenAccount)
	if err == nil {
		return value, nil
	}
	if errors.Is(err, secret.ErrNotFound) {
		return "", nil
	}
	return "", fmt.Errorf("resolve credential: %w", err)
}
