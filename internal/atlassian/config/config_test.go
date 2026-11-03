// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
	"workctl/internal/secret"
)

func TestLoadMissingFileYieldsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "nope", "config.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.CurrentProfile != "default" || c.Profiles == nil {
		t.Fatalf("unexpected default config: %+v", c)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atl", "config.yaml")
	c := Default()
	c.SetPath(path)
	c.Profiles["default"] = Profile{URL: "https://x.atlassian.net", Email: "a@b.c", Timeout: "10s", VerifyTLS: true}
	c.SavedJQL["mine"] = "assignee = currentUser()"
	c.SavedCQL["docs"] = "space = AB"
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	p := got.Profiles["default"]
	if p.URL != "https://x.atlassian.net" || p.Email != "a@b.c" || !p.VerifyTLS {
		t.Fatalf("profile mismatch: %+v", p)
	}
	if got.SavedJQL["mine"] == "" || got.SavedCQL["docs"] == "" {
		t.Fatalf("saved queries lost: %+v", got)
	}
}

func TestProfileNamePrecedence(t *testing.T) {
	c := Default()
	c.CurrentProfile = "filecfg"
	if got := c.ProfileName("explicit"); got != "explicit" {
		t.Fatalf("override ignored: %s", got)
	}
	t.Setenv("ATL_PROFILE", "fromenv")
	if got := c.ProfileName(""); got != "fromenv" {
		t.Fatalf("env ignored: %s", got)
	}
	os.Unsetenv("ATL_PROFILE")
	if got := c.ProfileName(""); got != "filecfg" {
		t.Fatalf("config ignored: %s", got)
	}
}

func TestResolveTokenEnvWins(t *testing.T) {
	t.Setenv("ATL_API_TOKEN", "tok-from-env")
	got, err := ResolveToken("default", "a@b.c")
	if err != nil || got != "tok-from-env" {
		t.Fatalf("ResolveToken = %q, %v", got, err)
	}
}

func TestResolveTokenUsesSharedStore(t *testing.T) {
	keyring.MockInit()
	if err := store.Set(secret.Account("atlassian", "default"), "shared"); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveToken("default", "a@b.c")
	if err != nil || got != "shared" {
		t.Fatalf("shared token = %q, %v", got, err)
	}
}

func TestActiveMissingProfileErrors(t *testing.T) {
	c := Default()
	if _, _, err := c.Active("ghost"); err == nil {
		t.Fatal("expected error for missing profile")
	}
}

func TestTimeoutDuration(t *testing.T) {
	if d := (Profile{Timeout: "5s"}).TimeoutDuration().Seconds(); d != 5 {
		t.Fatalf("timeout = %v", d)
	}
	if d := (Profile{}).TimeoutDuration().Seconds(); d != 30 {
		t.Fatalf("default timeout = %v", d)
	}
	if d := (Profile{Timeout: "junk"}).TimeoutDuration().Seconds(); d != 30 {
		t.Fatalf("fallback timeout = %v", d)
	}
}
