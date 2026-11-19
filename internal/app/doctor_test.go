// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
)

func TestDoctorCheckAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v3/about":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"user":{"emailAddress":"me@example.test","displayName":"Me"}}`))
		case "/denied/":
			http.Error(w, "no", http.StatusForbidden)
		default:
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()

	tests := []struct {
		name       string
		path       string
		wantDetail map[string]string
		wantCode   string
	}{
		{"plain api", "/plain", map[string]string{"kind": "api", "content_type": "text/plain"}, ""},
		{"drive reports the user", "/drive/v3", map[string]string{"kind": "api", "content_type": "application/json", "user": "me@example.test"}, ""},
		{"forbidden", "/denied", nil, "forbidden"},
	}
	for _, tc := range tests {
		profile := config.Profile{Kind: "api", URL: srv.URL + tc.path, Auth: "none", VerifyTLS: true}
		cfg := config.Default()
		cfg.Profiles = map[string]config.Profile{"p": profile}
		c, err := api.NewProfileClient(cfg, "p", profile)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got, err := doctorCheckAPI(context.Background(), c, profile)
		if code := classifyError(err); code != tc.wantCode {
			t.Errorf("%s: code %q (%v), want %q", tc.name, code, err, tc.wantCode)
			continue
		}
		if tc.wantDetail != nil && fmt.Sprint(got) != fmt.Sprint(tc.wantDetail) {
			t.Errorf("%s: detail %v, want %v", tc.name, got, tc.wantDetail)
		}
	}
}

func TestDoctorSkills(t *testing.T) {
	tests := []struct {
		name     string
		prepare  func(home string)
		wantOK   bool
		wantCode string
		wantErr  string
	}{
		{"no agent installed", func(string) {}, true, "", ""},
		{"up to date", func(string) { mustSkills(t, "install", "--target", "claude") }, true, "", ""},
		{"outdated copy", func(home string) {
			mustSkills(t, "install", "--target", "all")
			if err := os.WriteFile(filepath.Join(home, ".codex/skills/workctl/SKILL.md"), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, false, "outdated", "workctl skills install --target codex"},
	}
	for _, tc := range tests {
		home := skillsHome(t)
		tc.prepare(home)
		got := doctorSkills()
		if got.OK != tc.wantOK || got.Code != tc.wantCode || !strings.Contains(got.Error, tc.wantErr) {
			t.Errorf("%s: %+v", tc.name, got)
		}
		if tc.name == "outdated copy" && strings.Contains(got.Error, "claude") {
			t.Errorf("%s: current copies must not be listed: %s", tc.name, got.Error)
		}
	}
}

func mustSkills(t *testing.T, args ...string) {
	t.Helper()
	if _, _, err := runSkillsCmd(args...); err != nil {
		t.Fatal(err)
	}
}
