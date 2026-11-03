// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
)

func TestBearerAuthAndIssuePath(t *testing.T) {
	t.Setenv("TEST_JIRA_TOKEN", "jira-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/2/issue/DEMO-123" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer jira-secret" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"key":"DEMO-123"}`))
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.Profiles["jira"] = config.Profile{Kind: "jira", URL: srv.URL, TokenEnv: "TEST_JIRA_TOKEN", VerifyTLS: true}
	c, err := api.NewKindClient(cfg, "jira")
	if err != nil {
		t.Fatal(err)
	}
	b, err := GetIssue(context.Background(), c, "DEMO-123", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "DEMO-123") {
		t.Fatalf("body = %s", b)
	}
}
