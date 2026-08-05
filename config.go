package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Config struct {
	Version        int    `json:"version"`
	URL            string `json:"url"`
	TokenEnv       string `json:"token_env,omitempty"`
	TokenAccount   string `json:"token_account,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	VerifyTLS      bool   `json:"verify_tls"`
	path           string
}

func defaultConfig() *Config {
	return &Config{Version: 1, TokenAccount: "default", TimeoutSeconds: 30, VerifyTLS: true}
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
	cfg := defaultConfig()
	if path == "" {
		var err error
		path, err = defaultConfigPath()
		if err != nil {
			return nil, err
		}
	}
	cfg.path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	cfg.URL = strings.TrimRight(strings.TrimSpace(os.ExpandEnv(cfg.URL)), "/")
	if cfg.TimeoutSeconds <= 0 {
		cfg.TimeoutSeconds = 30
	}
	if cfg.TokenAccount == "" {
		cfg.TokenAccount = "default"
	}
	return cfg, nil
}
