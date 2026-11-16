// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"net/http"
	"testing"

	"workctl/internal/config"
)

func TestGoogleAbsoluteURLsStayOnAllowedHosts(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	cred, _ := (OAuth2Credential{ClientSecret: "s", RefreshToken: "r"}).Encode()
	t.Setenv("TEST_G_SECRET", cred)
	newClient := func(service string) *Client {
		p := config.Profile{Kind: "api", Service: service, URL: "https://www.googleapis.com/drive/v3", Auth: "oauth2", VerifyTLS: true, TokenEnv: "TEST_G_SECRET",
			OAuth2: &config.OAuth2{ClientID: "c", TokenURL: "https://oauth2.googleapis.com/token"}}
		cfg := config.Default()
		c, err := NewProfileClient(cfg, "p", p)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	colab := "https://colaboratory.googleapis.com/v1beta/runtimes"
	all, drive := newClient("all"), newClient("drive")
	if err := all.ValidateRequest(http.MethodGet, colab, nil, false); err != nil {
		t.Errorf("service all must reach colab: %v", err)
	}
	if err := drive.ValidateRequest(http.MethodGet, colab, nil, false); err == nil {
		t.Error("service drive must refuse colab")
	}
	for _, evil := range []string{"https://evil.example.com/x", "http://colaboratory.googleapis.com/x", "https://user@colaboratory.googleapis.com/x", "https://googleapis.com.evil.com/x"} {
		if err := all.ValidateRequest(http.MethodGet, evil, nil, false); err == nil {
			t.Errorf("%s must be refused", evil)
		}
	}
}
