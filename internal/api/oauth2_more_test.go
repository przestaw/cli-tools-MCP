// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"workctl/internal/config"
)

// oauth2Profile returns a config and an oauth2 profile whose token endpoint is tokenURL and API is apiURL.
func oauth2Profile(t *testing.T, apiURL, tokenURL string) (*config.Config, config.Profile) {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cred, err := (OAuth2Credential{ClientSecret: "csec", RefreshToken: "rtok"}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_OAUTH2_SECRET", cred)
	cfg := config.Default()
	prof := config.Profile{
		Kind:     "api",
		URL:      apiURL + "/drive/v3",
		Auth:     "oauth2",
		TokenEnv: "TEST_OAUTH2_SECRET",
		OAuth2:   &config.OAuth2{ClientID: "cid", TokenURL: tokenURL + "/token", Scopes: []string{"s1"}},
	}
	cfg.Profiles["p"] = prof
	return cfg, prof
}

func TestOAuth2RetriesOnceAfter401(t *testing.T) {
	var tokenCalls atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := tokenCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": fmt.Sprintf("tok-%d", n), "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	var apiCalls atomic.Int32
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		if r.Header.Get("Authorization") != "Bearer tok-2" {
			http.Error(w, "revoked", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer apiSrv.Close()

	cfg, prof := oauth2Profile(t, apiSrv.URL, tokenSrv.URL)
	c, err := NewProfileClient(cfg, "p", prof)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/files", nil, nil, ""); err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if tokenCalls.Load() != 2 || apiCalls.Load() != 2 {
		t.Fatalf("token calls %d, api calls %d; want 2 and 2", tokenCalls.Load(), apiCalls.Load())
	}
}

func TestOAuth2GivesUpAfterOneRetry(t *testing.T) {
	var tokenCalls atomic.Int32
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		_, _ = w.Write([]byte(`{"access_token":"tok","expires_in":3600}`))
	}))
	defer tokenSrv.Close()
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer apiSrv.Close()

	cfg, prof := oauth2Profile(t, apiSrv.URL, tokenSrv.URL)
	c, err := NewProfileClient(cfg, "p", prof)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/files", nil, nil, ""); err == nil {
		t.Fatal("expected the 401 to be returned")
	}
	if tokenCalls.Load() != 2 {
		t.Fatalf("token calls = %d, want 2 (one refresh, one retry)", tokenCalls.Load())
	}
}

func TestOAuth2InvalidGrantTellsToLoginAgain(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`, http.StatusBadRequest)
	}))
	defer tokenSrv.Close()

	cfg, prof := oauth2Profile(t, "http://127.0.0.1:1", tokenSrv.URL)
	c, err := NewProfileClient(cfg, "p", prof)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Do(context.Background(), http.MethodGet, "/files", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "workctl auth login --profile p") {
		t.Fatalf("error %v should point at `workctl auth login --profile p`", err)
	}
}

func TestOAuth2CacheKeyDependsOnScopes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	_, a := oauth2Profile(t, "http://x", "http://t")
	b := a
	o := *a.OAuth2
	o.Scopes = []string{"s1", "s2"}
	b.OAuth2 = &o
	if oauth2CacheFile("p", a) == oauth2CacheFile("p", b) {
		t.Fatal("profiles with different scopes share one access-token cache file")
	}
}

func TestLoginOAuth2StrayCallbackDoesNotAbortAndAuthParamsAreSent(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"a","refresh_token":"r","expires_in":3600}`))
	}))
	defer tokenSrv.Close()

	prof := config.Profile{
		Kind: "api", URL: "http://127.0.0.1:1/drive/v3", Auth: "oauth2",
		OAuth2: &config.OAuth2{
			ClientID: "c", AuthURL: "http://127.0.0.1:1/auth", TokenURL: tokenSrv.URL + "/token",
			AuthParams: map[string]string{"access_type": "offline", "prompt": "consent"},
		},
	}
	var sawParams bool
	cred, err := LoginOAuth2(context.Background(), config.Default(), "p", prof, OAuth2Credential{}, func(authURL string) {
		u, _ := url.Parse(authURL)
		q := u.Query()
		sawParams = q.Get("access_type") == "offline" && q.Get("prompt") == "consent"
		redirect := q.Get("redirect_uri")
		if resp, err := http.Get(redirect + "?state=wrong&error=access_denied"); err == nil {
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("stray callback answered %d, want 400", resp.StatusCode)
			}
			_ = resp.Body.Close()
		}
		if resp, err := http.Get(redirect + "?state=" + url.QueryEscape(q.Get("state")) + "&code=ok"); err == nil {
			_ = resp.Body.Close()
		}
	})
	if err != nil {
		t.Fatalf("login aborted by a stray callback: %v", err)
	}
	if !sawParams {
		t.Error("auth_params missing from the authorization URL")
	}
	if cred.RefreshToken != "r" {
		t.Fatalf("credential %+v", cred)
	}
}
