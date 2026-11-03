// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/mcp"
	"workctl/internal/modules"
	"workctl/internal/testutil"
)

func TestHelperProcess(t *testing.T) { testutil.ChildHelperProcess() }

func testConfig(base string) *config.Config {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"jira":   {Kind: "jira", URL: base, TokenEnv: "TEST_JIRA_TOKEN", VerifyTLS: true},
		"gitlab": {Kind: "gitlab", URL: base, TokenEnv: "TEST_GITLAB_TOKEN", VerifyTLS: true, DefaultProject: "group/project", Projects: []string{"group/project"}},
	}
	return cfg
}

func TestCommandRegistry(t *testing.T) {
	handlers := commandHandlers()
	for _, name := range []string{"jira", "gitlab", "github", "api", "pr", "mr", "task", "mcp", "doctor", "git-credential", "azure", "atlassian", "bitbucket", "asana"} {
		if handlers[name] == nil {
			t.Errorf("missing handler %s", name)
		}
	}
	cfg := config.Default()
	for _, name := range []string{"pr", "mr", "task", "unknown"} {
		err := dispatchCommand(cfg, name, nil)
		if err == nil {
			t.Errorf("%s should require arguments or reject command", name)
		}
	}
	if err := dispatchCommand(cfg, "unknown", nil); err == nil || !strings.Contains(err.Error(), `unknown command "unknown"`) {
		t.Fatal(err)
	}
}

// rpcResult mirrors the parts of a JSON-RPC response the protocol tests inspect.
type rpcResult struct {
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func TestMCPProtocolDispatch(t *testing.T) {
	input := strings.Join([]string{
		`{`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":null,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":[]}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"unknown"}}`,
		`{"jsonrpc":"2.0","id":6,"method":"missing"}`,
		`{"jsonrpc":"2.0","id":7,"method":"server/discover"}`,
	}, "\n")
	var output bytes.Buffer
	if err := mcp.Serve(context.Background(), mcpServer(config.Default()), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 8 {
		t.Fatalf("unexpected responses: %s", output.String())
	}
	for index, code := range []int{-32700, 0, 0, 0, -32602, 0, -32601, -32601} {
		var resp rpcResult
		if err := json.Unmarshal([]byte(lines[index]), &resp); err != nil {
			t.Fatal(err)
		}
		if code != 0 && (resp.Error == nil || resp.Error.Code != code) || code == 0 && resp.Error != nil {
			t.Fatalf("response %d: %+v", index, resp)
		}
	}
	if !strings.Contains(lines[1], `"protocolVersion":"2024-11-05"`) || !strings.Contains(lines[5], `"isError":true`) {
		t.Fatal(output.String())
	}
}

// callOverRPC sends one tools/call request through the protocol loop and returns the raw response line.
func callOverRPC(t *testing.T, cfg *config.Config, params string) string {
	t.Helper()
	var output bytes.Buffer
	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":` + params + `}`
	if err := mcp.Serve(context.Background(), mcpServer(cfg), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

func TestMCPTruncationAndPolicy(t *testing.T) {
	calls := testutil.FakeChildProcess(t)
	cfg := config.Default()
	cfg.MCP.MaxOutputBytes = 5
	body := callOverRPC(t, cfg, `{"name":"azure","arguments":{"args":["pr","get",1]}}`)
	if !strings.Contains(body, "ARGS:") || !strings.Contains(body, "output truncated") {
		t.Fatal(body)
	}
	before := len(*calls)
	body = callOverRPC(t, cfg, `{"name":"azure","arguments":{"args":["pr","create"]}}`)
	if len(*calls) != before {
		t.Fatal("a refused write started a process")
	}
	if !strings.Contains(body, `"isError":true`) || !strings.Contains(body, "allow_write") {
		t.Fatal(body)
	}
}

func TestMCPToolCallConvertsNumbersAndUsesTheModule(t *testing.T) {
	testutil.FakeChildProcess(t)
	out, err := callMCPTool(context.Background(), config.Default(), "azure", map[string]any{
		"args": []any{"pr", "get", float64(34438), "--json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "azure|pr|get|34438|--json|--no-color") {
		t.Fatalf("child saw %q", out)
	}
	if _, err := callMCPTool(context.Background(), config.Default(), "azure", map[string]any{"args": []any{"pr", map[string]any{}}}); err == nil {
		t.Fatal("a non-scalar argument must be rejected")
	}
	if _, err := callMCPTool(context.Background(), config.Default(), "azure", map[string]any{}); err == nil {
		t.Fatal("empty args must be rejected")
	}
}

type rpcFailedWriter struct{}

func (rpcFailedWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestMCPPropagatesOutputErrors(t *testing.T) {
	for _, input := range []string{`{`, `{"id":1,"method":"ping"}`, `{"id":2,"method":"unknown"}`, `{"id":3,"method":"server/discover"}`} {
		err := mcp.Serve(context.Background(), mcpServer(config.Default()), strings.NewReader(input), rpcFailedWriter{})
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Fatalf("input=%s err=%v", input, err)
		}
	}
}

func TestJiraAndGitLabAreOptional(t *testing.T) {
	cfg := config.Default()
	if cfg.Has("jira") || cfg.Has("gitlab") {
		t.Fatal("nothing should be configured by default")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("an empty config must validate: %v", err)
	}
	if _, err := api.NewKindClient(cfg, "jira"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("jira client error = %v", err)
	}
	if _, err := api.NewKindClient(cfg, "gitlab"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("gitlab client error = %v", err)
	}
}

func TestOnlyConfiguredBackendsGetMCPTools(t *testing.T) {
	cfg := config.Default()
	names := func() map[string]bool {
		m := map[string]bool{}
		for _, tool := range mcpTools(cfg) {
			m[tool.Name] = true
		}
		return m
	}
	if got := names(); len(got) != 2 || !got["azure"] || !got["atlassian"] {
		t.Fatalf("no Jira/GitLab configured, expected only the embedded azure and atlassian tools, got %v", got)
	}
	cfg.Profiles["jira"] = config.Profile{Kind: "jira", URL: "https://jira.example.test"}
	got := names()
	if !got["jira_get_issue"] || got["gitlab_get_project"] || got["task_get_context"] || got["pr_list"] {
		t.Fatalf("jira only, tools = %v", got)
	}
	cfg.Profiles["gitlab"] = config.Profile{Kind: "gitlab", URL: "https://gitlab.example.test"}
	if got := names(); !got["task_get_context"] || !got["gitlab_get_project"] || !got["pr_list"] {
		t.Fatalf("both configured, tools = %v", got)
	}
	if _, err := callMCPTool(context.Background(), config.Default(), "jira_get_issue", map[string]any{"key": "X-1"}); err == nil {
		t.Fatal("a tool of an unconfigured backend must be refused")
	}
}

func TestGitHubToolsOnlyWhenConfigured(t *testing.T) {
	has := func(cfg *config.Config, name string) bool {
		for _, tool := range mcpTools(cfg) {
			if tool.Name == name {
				return true
			}
		}
		return false
	}
	cfg := config.Default()
	if has(cfg, "github_get_repo") {
		t.Fatal("github tool advertised without configuration")
	}
	cfg.Profiles["gh"] = config.Profile{Kind: "github", URL: "https://api.github.com"}
	if !has(cfg, "github_get_repo") || has(cfg, "github_create_pull_request") {
		t.Fatal("expected read tools only")
	}
	cfg.Policy.AllowWrite = true
	if !has(cfg, "github_create_pull_request") {
		t.Fatal("write tool missing with allow_write")
	}
	if !has(cfg, "github_request_reviewers") || !has(cfg, "github_edit_pull_request") {
		t.Fatal("reviewer and edit tools missing with allow_write")
	}
}

func TestMCPProfileToolsRegisteredAndCallable(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"gh": {
			Kind:        "github",
			URL:         "https://api.github.test",
			DefaultRepo: "owner/repo",
		},
	}
	tools := mcpTools(cfg)
	hasTool := func(name string) bool {
		for _, tool := range tools {
			if tool.Name == name {
				return true
			}
		}
		return false
	}
	if !hasTool("pr_list") || !hasTool("pr_show") {
		t.Fatalf("expected pr_list and pr_show tools when profiles exist, got %v", tools)
	}
}

func TestTaskContextAggregatesJiraBranchesAndMRs(t *testing.T) {
	t.Setenv("TEST_JIRA_TOKEN", "jira-secret")
	t.Setenv("TEST_GITLAB_TOKEN", "gl-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/rest/api/2/issue/"):
			_, _ = w.Write([]byte(`{"key":"DEMO-1","fields":{"summary":"x"}}`))
		case strings.HasSuffix(r.URL.Path, "/repository/branches"):
			_, _ = w.Write([]byte(`[{"name":"agent/DEMO-1"}]`))
		case strings.HasSuffix(r.URL.Path, "/merge_requests"):
			_, _ = w.Write([]byte(`[{"iid":7,"title":"DEMO-1 fix"}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	b, err := taskContextJSON(context.Background(), cfg, "DEMO-1")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["key"] != "DEMO-1" {
		t.Fatalf("key = %#v", got["key"])
	}
	if _, ok := got["jira"]; !ok {
		t.Fatal("missing jira")
	}
	if _, ok := got["gitlab"]; !ok {
		t.Fatal("missing gitlab")
	}
}

func TestPlannedModulesSayTheyAreNotImplemented(t *testing.T) {
	t.Setenv("WORKCTL_CONFIG", t.TempDir()+"\\absent.json")
	runnable := map[string]bool{}
	for _, name := range modules.Names() {
		runnable[name] = true
	}
	for _, name := range []string{"bitbucket", "asana"} {
		err := run([]string{name, "list"})
		if err == nil || !strings.Contains(err.Error(), "not implemented yet") || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
		if runnable[name] {
			t.Errorf("%s is a placeholder and must not be a runnable module", name)
		}
	}
	for _, tool := range mcpTools(config.Default()) {
		if _, planned := plannedModules[tool.Name]; planned || strings.HasPrefix(tool.Name, "bitbucket") || strings.HasPrefix(tool.Name, "asana") {
			t.Errorf("a placeholder must not advertise the MCP tool %q", tool.Name)
		}
	}
}
