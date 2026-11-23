// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package auth

import (
	"encoding/json"
	"testing"

	"github.com/zalando/go-keyring"

	"workctl/internal/config"
	"workctl/internal/testutil"
)

func TestAuthStatusReportsClientIDSource(t *testing.T) {
	keyring.MockInit()
	defer config.SetBuiltinClientIDs(map[string]string{"gitlab.com": "builtin-gl"})()
	oauth := func(url, id string) config.Profile {
		return config.Profile{Kind: "api", URL: url, Auth: "oauth2", VerifyTLS: true, OAuth2: &config.OAuth2{ClientID: id, TokenURL: url + "/token"}}
	}
	path := saveConfig(t, map[string]config.Profile{
		"own":     oauth("https://gitlab.com", "mine"),
		"builtin": oauth("https://gitlab.com", ""),
		"plain":   {Kind: "github", URL: "https://api.github.com", VerifyTLS: true},
	})
	var runErr error
	out := testutil.CaptureStdout(t, func() { runErr = Command(path, "", []string{"status", "--json"}) })
	if runErr != nil {
		t.Fatal(runErr)
	}
	var entries []statusEntry
	if err := json.Unmarshal([]byte(out), &entries); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range entries {
		got[e.Name] = e.ClientIDSource
	}
	if got["own"] != "own" || got["builtin"] != "built-in" || got["plain"] != "" {
		t.Fatalf("client_id_source = %v", got)
	}
}
