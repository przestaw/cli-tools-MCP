// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package gitlab

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"workctl/internal/config"
	"workctl/internal/testutil"
)

const projectPath = "/api/v4/projects/group%2Fproject"

func writeConfig(t *testing.T, base string) *config.Config {
	t.Helper()
	t.Setenv("TEST_GITLAB_TOKEN", "gl-secret")
	cfg := testConfig(base)
	cfg.Policy.AllowWrite = true
	return cfg
}

func query(t *testing.T, raw string) url.Values {
	t.Helper()
	values, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	return values
}

func TestCreateMergeRequestAddsDraftPrefixOnce(t *testing.T) {
	cases := []struct {
		title string
		draft bool
		want  string
	}{
		{"Fix login", true, "Draft: Fix login"},
		{"Draft: Fix login", true, "Draft: Fix login"}, // already a draft
		{"WIP: Fix login", true, "WIP: Fix login"},     // older GitLab spelling
		{"Fix login", false, "Fix login"},
	}
	for _, c := range cases {
		base, seen := testutil.RecordingServer(t, 200, `{}`)
		cfg := writeConfig(t, base)
		args := map[string]any{"source": "feat", "target": "main", "title": c.title, "draft": c.draft}
		if _, err := CallTool(context.Background(), cfg, "gitlab_create_merge_request", args); err != nil {
			t.Fatal(err)
		}
		got := (*seen)[0]
		q := query(t, got.Query)
		if got.Method != "POST" || got.Path != projectPath+"/merge_requests" {
			t.Errorf("request = %+v", got)
		}
		if q.Get("title") != c.want || q.Get("source_branch") != "feat" || q.Get("target_branch") != "main" {
			t.Errorf("title %q (draft=%v): query = %v, want title %q", c.title, c.draft, q, c.want)
		}
	}
}

func TestMergeRequestNoteGoesInTheQuery(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `{}`)
	cfg := writeConfig(t, base)
	if _, err := CallTool(context.Background(), cfg, "gitlab_add_merge_request_note", map[string]any{"iid": 3, "body": "hello world"}); err != nil {
		t.Fatal(err)
	}
	got := (*seen)[0]
	if got.Method != "POST" || got.Path != projectPath+"/merge_requests/3/notes" || query(t, got.Query).Get("body") != "hello world" {
		t.Errorf("request = %+v", got)
	}
}

func TestWritesNeedPolicy(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `{}`)
	cfg := writeConfig(t, base)
	cfg.Policy.AllowWrite = false
	for _, tool := range []string{"gitlab_add_merge_request_note", "gitlab_create_merge_request"} {
		args := map[string]any{"iid": 1, "body": "x", "source": "a", "target": "b", "title": "t"}
		if _, err := CallTool(context.Background(), cfg, tool, args); err == nil || !strings.Contains(err.Error(), "allow_write") {
			t.Errorf("%s without policy: %v", tool, err)
		}
	}
	if len(*seen) != 0 {
		t.Fatalf("a refused write sent a request: %v", *seen)
	}
}

func TestListToolsApplyDefaultsAndFilters(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `[]`)
	cfg := writeConfig(t, base)
	ctx := context.Background()
	calls := []struct {
		tool string
		args map[string]any
	}{
		{"gitlab_list_merge_requests", nil},
		{"gitlab_list_merge_requests", map[string]any{"search": "DEMO-1", "state": "opened"}},
		{"gitlab_list_branches", map[string]any{"search": "DEMO"}},
	}
	for _, c := range calls {
		if _, err := CallTool(ctx, cfg, c.tool, c.args); err != nil {
			t.Fatal(err)
		}
	}
	defaults := query(t, (*seen)[0].Query)
	if defaults.Get("state") != "all" || defaults.Get("scope") != "all" || defaults.Get("per_page") != "100" || defaults.Has("search") {
		t.Errorf("default MR query = %v", defaults)
	}
	filtered := query(t, (*seen)[1].Query)
	if filtered.Get("state") != "opened" || filtered.Get("search") != "DEMO-1" {
		t.Errorf("filtered MR query = %v", filtered)
	}
	if got := (*seen)[2]; got.Path != projectPath+"/repository/branches" || query(t, got.Query).Get("search") != "DEMO" {
		t.Errorf("branches request = %+v", got)
	}
}

func TestFileDefaultsToHEADAndEscapesThePath(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `content`)
	cfg := writeConfig(t, base)
	if _, err := CallTool(context.Background(), cfg, "gitlab_get_file", map[string]any{"path": "docs/a b.md"}); err != nil {
		t.Fatal(err)
	}
	got := (*seen)[0]
	if got.Path != projectPath+"/repository/files/docs%2Fa%20b.md/raw" || query(t, got.Query).Get("ref") != "HEAD" {
		t.Errorf("request = %+v", got)
	}
}

func TestPipelineCommands(t *testing.T) {
	base, seen := testutil.RecordingServer(t, 200, `{}`)
	cfg := writeConfig(t, base)

	if err := Command(cfg, []string{"pipelines", "list", "--ref", "main", "--per-page", "5"}); err != nil {
		t.Fatal(err)
	}
	if err := Command(cfg, []string{"pipeline", "jobs", "12"}); err != nil {
		t.Fatal(err)
	}
	list, jobs := (*seen)[0], (*seen)[1]
	if q := query(t, list.Query); list.Path != projectPath+"/pipelines" || q.Get("ref") != "main" || q.Get("per_page") != "5" {
		t.Errorf("list request = %+v", list)
	}
	if jobs.Path != projectPath+"/pipelines/12/jobs" {
		t.Errorf("jobs request = %+v", jobs)
	}

	before := len(*seen)
	if err := Command(cfg, []string{"pipeline", "bogus", "12"}); err == nil || !strings.Contains(err.Error(), "unknown pipeline action") {
		t.Errorf("unknown action: %v", err)
	}
	if len(*seen) != before {
		t.Error("an unknown action sent a request")
	}
}

func TestToolsHideWritesWithoutPolicy(t *testing.T) {
	cfg := config.Default()
	count := func() (reads, writes int) {
		for _, tool := range Tools(cfg) {
			if strings.Contains(tool.Description, "policy.allow_write") {
				writes++
			} else {
				reads++
			}
		}
		return reads, writes
	}
	reads, writes := count()
	if reads == 0 || writes != 0 {
		t.Errorf("without policy: %d reads, %d writes", reads, writes)
	}
	cfg.Policy.AllowWrite = true
	if _, writes := count(); writes != 2 {
		t.Errorf("with policy: %d write tools, want 2", writes)
	}
}
