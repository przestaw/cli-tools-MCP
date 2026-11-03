// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
	"workctl/internal/secret"
)

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	c := Default()
	c.SetPath(path)
	c.Profiles["default"] = Profile{OrgURL: "https://dev.azure.com/acme", Project: "Web", Timeout: "30s", VerifyTLS: true}
	c.CurrentProfile = "default"
	if err := c.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	p, name, err := got.Active("")
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if name != "default" || p.OrgURL != "https://dev.azure.com/acme" || p.Project != "Web" {
		t.Errorf("round-trip mismatch: %+v (name=%s)", p, name)
	}
}

func TestProfileNamePrecedence(t *testing.T) {
	c := Default()
	c.CurrentProfile = "cfg"
	if got := c.ProfileName("override"); got != "override" {
		t.Errorf("override should win, got %q", got)
	}
	t.Setenv("ADO_PROFILE", "env")
	if got := c.ProfileName(""); got != "env" {
		t.Errorf("env should win over config, got %q", got)
	}
}

func TestResolveTokenEnv(t *testing.T) {
	t.Setenv("ADO_PAT", "secret")
	tok, err := ResolveToken("default", "https://dev.azure.com/acme")
	if err != nil || tok != "secret" {
		t.Errorf("ResolveToken env = %q, %v", tok, err)
	}
}

func TestResolveTokenUsesSharedStore(t *testing.T) {
	keyring.MockInit()
	if err := store.Set(secret.Account("azure", "default"), "shared"); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveToken("default", "https://dev.azure.com/acme")
	if err != nil || got != "shared" {
		t.Fatalf("shared token = %q, %v", got, err)
	}
}

func TestLoadMissingFileIsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if c.Profiles == nil {
		t.Error("default config should have a profiles map")
	}
}
