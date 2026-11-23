// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"strings"
	"testing"
)

func TestSetChangesPolicyTimeoutsAndDefaults(t *testing.T) {
	c := Default()
	for key, value := range map[string]string{
		"policy.allow_write":      "true",
		"confirm_writes":          "true", // the prefix is optional
		"timeout_seconds":         "45",
		"timeouts.jira":           "90",
		"mcp.max_output_bytes":    "1000",
		"modules.timeout_seconds": "10",
		"defaults.github":         "work",
	} {
		if err := c.Set(key, value); err != nil {
			t.Fatalf("Set(%s): %v", key, err)
		}
	}
	if !c.Policy.AllowWrite || !c.Policy.ConfirmWrites || c.TimeoutSeconds != 45 || c.Timeouts["jira"] != 90 ||
		c.MCP.MaxOutputBytes != 1000 || c.Modules.TimeoutSeconds != 10 || c.Defaults["github"] != "work" {
		t.Fatalf("config = %+v", c)
	}
	if err := c.Set("defaults.github", ""); err != nil || c.Defaults["github"] != "" {
		t.Fatalf("an empty value must remove the default: %v %v", err, c.Defaults)
	}
	for _, bad := range [][2]string{{"policy.allow_write", "maybe"}, {"timeout_seconds", "-1"}, {"timeouts.jira", "x"}, {"profiles.x.url", "y"}, {"defaults.", "p"}} {
		if err := c.Set(bad[0], bad[1]); err == nil {
			t.Errorf("Set(%s, %s) must fail", bad[0], bad[1])
		}
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	c := Default()
	c.Policy.ConfirmWrites, c.Policy.ConfirmSessionWrites = true, true
	c.Profiles["bad"] = Profile{Kind: "github", URL: "not a url"}
	c.Defaults["gitlab"] = "ghost"
	err := c.Validate()
	if err == nil {
		t.Fatal("Validate accepted a broken config")
	}
	problems := Problems(err)
	if len(problems) != 3 {
		t.Fatalf("problems = %v", problems)
	}
	if !strings.Contains(problems[0].Error(), "confirm_writes") || !strings.Contains(problems[1].Error(), "profiles.bad.url") || !strings.Contains(problems[2].Error(), "ghost") {
		t.Fatalf("problems = %v", problems)
	}
	if Problems(nil) != nil {
		t.Fatal("no error, no problems")
	}
}
