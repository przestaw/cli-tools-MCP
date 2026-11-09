// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"workctl/internal/config"
)

// accountServer answers GET /user with login and records the other requests.
func accountServer(t *testing.T, login string) (string, *[]string, *int) {
	t.Helper()
	var writes []string
	asked := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user" {
			asked++
			_, _ = io.WriteString(w, `{"login":"`+login+`"}`)
			return
		}
		writes = append(writes, r.Method+" "+r.URL.Path)
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &writes, &asked
}

func githubProfile(t *testing.T, url, username string) config.Profile {
	t.Helper()
	t.Setenv("TEST_ACCOUNT_TOKEN", "secret")
	return config.Profile{Kind: "github", URL: url, TokenEnv: "TEST_ACCOUNT_TOKEN", VerifyTLS: true, Username: username}
}

func TestWriteIsRefusedWithAnotherAccountsToken(t *testing.T) {
	url, writes, _ := accountServer(t, "cads-bot")
	c, err := NewProfileClient(config.Default(), "p", githubProfile(t, url, "przestaw"))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Do(context.Background(), http.MethodPost, "/repos/o/r/issues/1/comments", nil, []byte(`{}`), ""); err == nil || len(*writes) != 0 {
		t.Fatalf("write with another account's token: err=%v writes=%v", err, *writes)
	}
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/repos/o/r", nil, nil, ""); err != nil {
		t.Fatalf("reads stay allowed: %v", err)
	}
}

func TestWriteChecksTheAccountOnce(t *testing.T) {
	url, writes, asked := accountServer(t, "Przestaw")
	c, err := NewProfileClient(config.Default(), "p", githubProfile(t, url, "przestaw"))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, _, err := c.Do(context.Background(), http.MethodPost, "/repos/o/r/issues/1/comments", nil, []byte(`{}`), ""); err != nil {
			t.Fatal(err)
		}
	}
	if len(*writes) != 2 || *asked != 1 {
		t.Fatalf("writes=%v account checks=%d", *writes, *asked)
	}
}

func TestBindTokenBindsOrRefuses(t *testing.T) {
	url, _, _ := accountServer(t, "przestaw")
	bound, err := BindToken(config.Default(), "p", githubProfile(t, url, ""))
	if err != nil || bound.Username != "przestaw" {
		t.Fatalf("bind: %+v %v", bound, err)
	}
	if _, err := BindToken(config.Default(), "p", githubProfile(t, url, "cads-bot")); !errors.Is(err, ErrOtherAccount) {
		t.Fatalf("rebind to another account: %v", err)
	}
}
