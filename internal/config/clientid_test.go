// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"strings"
	"testing"
)

func TestResolveClientIDOrder(t *testing.T) {
	defer SetBuiltinClientIDs(map[string]string{"github.com": "builtin-gh"})()
	gh := func(clientID string) Profile {
		return Profile{Kind: "api", URL: "https://api.github.com", Auth: "oauth2", OAuth2: &OAuth2{ClientID: clientID, TokenURL: "https://github.com/login/oauth/access_token"}}
	}
	for _, tc := range []struct {
		name    string
		p       Profile
		wantID  string
		wantSrc string
	}{
		{"own wins over the built-in", gh(" own-id "), "own-id", ClientIDOwn},
		{"built-in when unset", gh(""), "builtin-gh", ClientIDBuiltIn},
	} {
		id, src, err := ResolveClientID("p", tc.p)
		if err != nil || id != tc.wantID || src != tc.wantSrc {
			t.Errorf("%s: got %q %q %v", tc.name, id, src, err)
		}
	}

	other := gh("")
	other.URL = "https://gitlab.example.test"
	_, _, err := ResolveClientID("work", other)
	if err == nil || !strings.Contains(err.Error(), "set client_id: workctl profile edit work --client-id ID") {
		t.Fatalf("missing client_id error = %v", err)
	}
}

func TestValidateAcceptsAMissingClientIDOnlyWithABuiltInDefault(t *testing.T) {
	p := Profile{Kind: "api", URL: "https://gitlab.com", Auth: "oauth2", VerifyTLS: true, OAuth2: &OAuth2{TokenURL: "https://gitlab.com/oauth/token"}}
	cfg := Default()
	cfg.Profiles = map[string]Profile{"gl": p}

	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "workctl profile edit gl --client-id ID") {
		t.Fatalf("empty table: %v", err)
	}
	defer SetBuiltinClientIDs(map[string]string{"gitlab.com": "builtin-gl"})()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("built-in default should satisfy validation: %v", err)
	}
}
