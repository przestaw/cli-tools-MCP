// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"workctl/internal/config"
)

func TestOAuth2RefreshSendsOwnThenBuiltInClientID(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.Form.Get("client_id")
		_, _ = w.Write([]byte(`{"access_token":"t","expires_in":3600}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	host, _, _ := net.SplitHostPort(u.Host)
	defer config.SetBuiltinClientIDs(map[string]string{host: "builtin-id"})()

	cred, _ := (OAuth2Credential{RefreshToken: "r"}).Encode()
	for _, tc := range []struct{ own, want string }{{"", "builtin-id"}, {"own-id", "own-id"}} {
		prof := config.Profile{Kind: "api", URL: srv.URL, Auth: "oauth2", VerifyTLS: false, OAuth2: &config.OAuth2{ClientID: tc.own, TokenURL: srv.URL + "/token"}}
		sess, err := newOAuth2Session("p"+tc.own, prof, cred, srv.Client())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sess.token(context.Background()); err != nil || got != tc.want {
			t.Errorf("own %q: client_id %q (%v), want %q", tc.own, got, err, tc.want)
		}
	}
}

func TestOAuth2SessionWithoutAnyClientIDExplainsTheFix(t *testing.T) {
	prof := config.Profile{Kind: "api", URL: "https://svc.example.test", Auth: "oauth2", OAuth2: &config.OAuth2{TokenURL: "https://svc.example.test/token"}}
	cred, _ := (OAuth2Credential{RefreshToken: "r"}).Encode()
	_, err := newOAuth2Session("svc", prof, cred, http.DefaultClient)
	if err == nil || !strings.Contains(err.Error(), "set client_id: workctl profile edit svc --client-id ID") {
		t.Fatalf("error = %v", err)
	}
}
