// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadGoogle(t *testing.T, profiles string) (*Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"profiles":{`+profiles+`}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return Load(path)
}

func TestGoogleProfilePresets(t *testing.T) {
	cfg, err := loadGoogle(t, `
		"gdrive":{"kind":"google","service":"drive","oauth2":{"client_id":"c"}},
		"gcloud":{"kind":"google","service":"cloud","oauth2":{"client_id":"c"}},
		"google":{"kind":"google","service":"all","oauth2":{"client_id":"c","scopes":["https://www.googleapis.com/auth/colaboratory.readonly"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	drive := cfg.Profiles["gdrive"]
	if drive.Kind != "api" || drive.Auth != "oauth2" || !drive.HasDrive() || drive.OAuth2.TokenURL == "" || drive.OAuth2.AuthParams["access_type"] != "offline" {
		t.Fatalf("drive preset not applied: %+v", drive)
	}
	if got := cfg.Profiles["gcloud"].URL; !strings.Contains(got, "cloudresourcemanager") {
		t.Fatalf("cloud URL = %q", got)
	}
	if got := cfg.Profiles["google"].OAuth2.Scopes; len(got) != 1 || !strings.HasSuffix(got[0], "colaboratory.readonly") {
		t.Fatalf("explicit scopes must win, got %v", got)
	}
	for _, host := range []string{"www.googleapis.com", "sheets.googleapis.com", "docs.googleapis.com"} {
		if !HostAllowed(drive.AbsoluteHosts(), host) {
			t.Fatalf("gdrive must allow %s", host)
		}
	}
	if HostAllowed(drive.AbsoluteHosts(), "colaboratory.googleapis.com") {
		t.Fatal("gdrive must not allow colaboratory.googleapis.com")
	}
	if !HostAllowed(cfg.Profiles["google"].AbsoluteHosts(), "colaboratory.googleapis.com") {
		t.Fatal("google must allow colaboratory.googleapis.com")
	}
	if HostAllowed(cfg.Profiles["google"].AbsoluteHosts(), "evilgoogleapis.com") {
		t.Fatal("suffix match must respect the dot")
	}
}

func TestGoogleProfileRejectsBadConfig(t *testing.T) {
	for name, profile := range map[string]string{
		"unknown service": `"p":{"kind":"google","service":"mail","oauth2":{"client_id":"c"}}`,
		"no oauth2":       `"p":{"kind":"google","service":"drive"}`,
		"other auth":      `"p":{"kind":"google","service":"drive","auth":"bearer","oauth2":{"client_id":"c"}}`,
		"foreign url":     `"p":{"kind":"google","service":"drive","url":"https://example.com/drive/v3","oauth2":{"client_id":"c"}}`,
	} {
		if _, err := loadGoogle(t, profile); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
