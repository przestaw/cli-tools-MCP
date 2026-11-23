// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package setup

import (
	"path/filepath"
	"strings"
	"testing"

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/testutil"
)

func TestProfileAddAsksForClientIDAndOffersTheBuiltInDefault(t *testing.T) {
	oauthArgs := []string{"add", "svc", "--kind", "api", "--host", "https://example.test", "--auth", "oauth2", "--token-url", "https://example.test/token"}
	run := func(answer string, table map[string]string) (string, config.Profile, error) {
		defer config.SetBuiltinClientIDs(table)()
		stdin, terminal := cliutil.Stdin, stdinIsTerminal
		defer func() { cliutil.Stdin, stdinIsTerminal = stdin, terminal }()
		cliutil.Stdin, stdinIsTerminal = strings.NewReader(answer), func() bool { return true }
		path := filepath.Join(t.TempDir(), "c.json")
		var err error
		out := testutil.CaptureStdout(t, func() { err = ProfileCommand(path, oauthArgs) })
		cfg, _ := config.Load(path)
		var p config.Profile
		if cfg != nil {
			p = cfg.Profiles["svc"]
		}
		return out, p, err
	}

	out, p, err := run("\n", map[string]string{"example.test": "builtin-id"})
	if err != nil || !strings.Contains(out, "OAuth client_id [Enter = built-in: builtin-id]:") || !strings.Contains(out, "OAuth client_id: built-in") {
		t.Fatalf("Enter keeps the built-in: %v\n%s", err, out)
	}
	if p.OAuth2.ClientID != "" {
		t.Fatalf("a built-in client_id must not be written to config: %q", p.OAuth2.ClientID)
	}

	out, p, err = run("mine\n", map[string]string{"example.test": "builtin-id"})
	if err != nil || p.OAuth2.ClientID != "mine" || !strings.Contains(out, "OAuth client_id: own") {
		t.Fatalf("own answer: %v %+v\n%s", err, p.OAuth2, out)
	}

	out, _, err = run("\n", nil)
	if err == nil || !strings.Contains(err.Error(), "workctl profile edit svc --client-id ID") || strings.Contains(out, "built-in:") {
		t.Fatalf("empty table, empty answer: %v\n%s", err, out)
	}
}

func TestProfileEditSetsClientID(t *testing.T) {
	defer config.SetBuiltinClientIDs(map[string]string{"example.test": "builtin-id"})()
	path := filepath.Join(t.TempDir(), "c.json")
	if err := ProfileCommand(path, []string{"add", "svc", "--kind", "api", "--host", "https://example.test", "--auth", "oauth2", "--token-url", "https://example.test/token"}); err != nil {
		t.Fatal(err)
	}
	if err := ProfileCommand(path, []string{"edit", "svc", "--client-id", "mine"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if id, src, _ := config.ResolveClientID("svc", cfg.Profiles["svc"]); id != "mine" || src != config.ClientIDOwn {
		t.Fatalf("resolved %q %q", id, src)
	}
}
