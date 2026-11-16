// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"workctl/internal/config"
)

func TestOAuth2RefreshAndCaching(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	var tokenCalls atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" || r.Form.Get("client_id") != "cid-123" || r.Form.Get("client_secret") != "csec-456" || r.Form.Get("refresh_token") != "rtok-789" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-bearer-token","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer tokenSrv.Close()

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer fresh-bearer-token" {
			http.Error(w, "unauthorized: "+got, http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files":[]}`))
	}))
	defer apiSrv.Close()

	cred, err := (OAuth2Credential{ClientSecret: "csec-456", RefreshToken: "rtok-789"}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_GDRIVE_SECRET", cred)

	cfg := config.Default()
	prof := config.Profile{
		Kind:      "api",
		URL:       apiSrv.URL + "/drive/v3",
		Auth:      "oauth2",
		TokenEnv:  "TEST_GDRIVE_SECRET",
		VerifyTLS: false,
		OAuth2: &config.OAuth2{
			ClientID: "cid-123",
			TokenURL: tokenSrv.URL + "/token",
		},
	}
	cfg.Profiles["gdrive"] = prof

	c1, err := NewProfileClient(cfg, "gdrive", prof)
	if err != nil {
		t.Fatal(err)
	}
	out, _, err := c1.Do(context.Background(), http.MethodGet, "/files", nil, nil, "")
	if err != nil || !strings.Contains(string(out), "files") {
		t.Fatalf("first call failed: %v, %s", err, out)
	}

	// A second client instance should reuse the on-disk cached access token without hitting tokenSrv again.
	c2, err := NewProfileClient(cfg, "gdrive", prof)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c2.Do(context.Background(), http.MethodGet, "/files", nil, nil, ""); err != nil {
		t.Fatalf("second call failed: %v", err)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("expected 1 token refresh call, got %d", got)
	}
}

func TestLoginOAuth2LoopbackPKCE(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "auth-code-xyz" || r.Form.Get("code_verifier") == "" {
			http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "initial-access",
			"refresh_token": "new-refresh-token",
			"expires_in":    3600,
		})
	}))
	defer tokenSrv.Close()

	cfg := config.Default()
	prof := config.Profile{
		Kind:      "api",
		URL:       "http://127.0.0.1:1/drive/v3",
		Auth:      "oauth2",
		VerifyTLS: false,
		OAuth2: &config.OAuth2{
			ClientID: "desktop-client",
			AuthURL:  "http://127.0.0.1:1/auth",
			TokenURL: tokenSrv.URL + "/token",
			Scopes:   []string{"https://www.googleapis.com/auth/drive"},
		},
	}

	cred, err := LoginOAuth2(context.Background(), cfg, "gdrive", prof, OAuth2Credential{ClientSecret: "sec"}, func(authURL string) {
		u, err := url.Parse(authURL)
		if err != nil {
			t.Errorf("parse authURL: %v", err)
			return
		}
		cb := u.Query().Get("redirect_uri") + "?state=" + url.QueryEscape(u.Query().Get("state")) + "&code=auth-code-xyz"
		resp, err := http.Get(cb)
		if err == nil {
			_ = resp.Body.Close()
		}
	})
	if err != nil {
		t.Fatalf("LoginOAuth2 failed: %v", err)
	}
	if cred.RefreshToken != "new-refresh-token" || cred.ClientSecret != "sec" {
		t.Fatalf("unexpected credential: %+v", cred)
	}
}
