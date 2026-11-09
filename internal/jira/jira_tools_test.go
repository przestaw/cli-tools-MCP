// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package jira

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/testutil"
)

func testConfig(t *testing.T, base string) *config.Config {
	t.Helper()
	t.Setenv("TEST_JIRA_TOKEN", "jira-secret")
	cfg := config.Default()
	cfg.Profiles["jira"] = config.Profile{Kind: "jira", URL: base, TokenEnv: "TEST_JIRA_TOKEN", VerifyTLS: true}
	return cfg
}

func TestSearchToolSendsJQLAndPaging(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `{}`)
	cfg := testConfig(t, base)

	c, err := api.NewKindClient(cfg, "jira")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := search(context.Background(), c, "project = A AND status = Open", 5, 10); err != nil {
		t.Fatal(err)
	}
	if err := Command(cfg, []string{"search", "--jql", "x"}); err != nil {
		t.Fatal(err)
	}

	first, _ := url.ParseQuery((*seen)[0].Query)
	if first.Get("jql") != "project = A AND status = Open" || first.Get("startAt") != "5" || first.Get("maxResults") != "10" {
		t.Errorf("first query = %v", first)
	}
	second, _ := url.ParseQuery((*seen)[1].Query)
	if second.Get("startAt") != "0" || second.Get("maxResults") != "50" {
		t.Errorf("defaults not applied: %v", second)
	}
}

func TestIssueKeyIsEscaped(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `{}`)
	c, err := api.NewKindClient(testConfig(t, base), "jira")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GetIssue(context.Background(), c, "A B/1", []string{"summary", "status"}); err != nil {
		t.Fatal(err)
	}
	got := (*seen)[0]
	if got.Path != "/rest/api/2/issue/A%20B%2F1" || got.Query != "fields=summary%2Cstatus" {
		t.Errorf("request = %+v", got)
	}
}

func TestCommentWriteNeedsPolicy(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `{}`)
	cfg := testConfig(t, base)
	args := []string{"comment", "add", "--body", "hi", "DEMO-1"}
	if err := Command(cfg, args); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("write without policy: %v", err)
	}
	if len(*seen) != 0 {
		t.Fatalf("a refused write sent a request: %v", *seen)
	}
	cfg.Policy.AllowWrite = true
	if err := Command(cfg, args); err != nil {
		t.Fatal(err)
	}
	got := (*seen)[0]
	if got.Method != "POST" || got.Path != "/rest/api/2/issue/DEMO-1/comment" || got.Body != `{"body":"hi"}` {
		t.Errorf("request = %+v", got)
	}
}

func TestDoTransitionAnswersOKForAnEmptyResponse(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 204, "")
	c, err := api.NewKindClient(testConfig(t, base), "jira")
	if err != nil {
		t.Fatal(err)
	}
	out, err := doTransition(context.Background(), c, "DEMO-1", "31")
	if err != nil || string(out) != "{\"ok\":true}\n" {
		t.Fatalf("out = %q, err = %v", out, err)
	}
	if got := (*seen)[0]; got.Path != "/rest/api/2/issue/DEMO-1/transitions" || got.Body != `{"transition":{"id":"31"}}` {
		t.Errorf("request = %+v", got)
	}
}

func TestCommandUsageErrorsSendNothing(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `{}`)
	cfg := testConfig(t, base)
	cases := map[string][]string{
		"is required":     {"search"},
		"usage: jira":     {"comments"},
		"unknown jira":    {"bogus"},
		"allow_write":     {"transition", "--id", "1", "DEMO-1"},
		"jira commands: ": {},
	}
	for want, args := range cases {
		if err := Command(cfg, args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: err = %v, want it to contain %q", args, err, want)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("usage errors sent requests: %v", *seen)
	}
}
