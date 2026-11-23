// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
	"workctl/internal/config"
)

func TestConfigSetThenValidateThroughTheCommandLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if _, code := captureRun(t, "--config", path, "config", "set", "allow_write", "true"); code != 0 {
		t.Fatalf("config set exit %d", code)
	}
	cfg, err := config.Load(path)
	if err != nil || !cfg.Policy.AllowWrite {
		t.Fatalf("policy not saved: %+v, %v", cfg, err)
	}
	// Setting an inconsistent pair is refused and leaves the file alone.
	captureRun(t, "--config", path, "config", "set", "confirm_writes", "true")
	if _, code := captureRun(t, "--config", path, "config", "set", "confirm_session_writes", "true"); code == 0 {
		t.Fatal("both confirm modes accepted")
	}
	if _, code := captureRun(t, "--config", path, "config", "set", "nonsense", "1"); code == 0 {
		t.Fatal("an unknown key was accepted")
	}
	out, code := captureRun(t, "--config", path, "config", "validate", "--json")
	var ok struct{ OK bool }
	if err := json.Unmarshal([]byte(out), &ok); code != 0 || err != nil || !ok.OK {
		t.Fatalf("validate: exit %d, %v\n%s", code, err, out)
	}
}

func TestConfigValidateFailsAndListsEveryProblem(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"version":2,"policy":{"confirm_writes":true},"profiles":{"x":{"kind":"github","url":"nope"}},"defaults":{"jira":"ghost"}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := captureRun(t, "--config", path, "config", "validate", "--json")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	var res struct {
		OK       bool
		Problems []string
	}
	if err := json.Unmarshal([]byte(out), &res); err != nil || res.OK || len(res.Problems) != 2 {
		t.Fatalf("%+v, %v\n%s", res, err, out)
	}
	if _, code := captureRun(t, "--config", filepath.Join(t.TempDir(), "missing.json"), "config", "validate"); code != 1 {
		t.Fatalf("a missing file exits %d, want 1", code)
	}
}

func TestDoctorFixHints(t *testing.T) {
	keyring.MockInit()
	cfg := config.Default()
	oauth := config.Profile{Kind: "api", URL: "https://x.example", Auth: "oauth2", VerifyTLS: true}
	gh := config.Profile{Kind: "github", URL: "https://api.github.com", VerifyTLS: true}
	if got := doctorFix(cfg, "drive", oauth, "unauthorized"); got != "workctl auth login --profile drive" {
		t.Errorf("oauth: %q", got)
	}
	if got := doctorFix(cfg, "gh", gh, "other"); got != "workctl auth set-token --profile gh" {
		t.Errorf("no credential: %q", got)
	}
	t.Setenv("TEST_DOCTOR_TOKEN", "x")
	gh.TokenEnv = "TEST_DOCTOR_TOKEN"
	for code, want := range map[string]string{
		"unauthorized": "workctl auth set-token --profile gh",
		"dns":          "workctl profile edit gh --host URL",
		"tls":          "workctl profile edit gh --ca CA_FILE",
		"other":        "",
	} {
		if got := doctorFix(cfg, "gh", gh, code); got != want {
			t.Errorf("%s: %q, want %q", code, got, want)
		}
	}
}
