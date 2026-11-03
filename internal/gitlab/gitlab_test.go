// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package gitlab

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/vcs"
)

func testConfig(base string) *config.Config {
	cfg := config.Default()
	cfg.Profiles["gitlab"] = config.Profile{Kind: "gitlab", URL: base, TokenEnv: "TEST_GITLAB_TOKEN", VerifyTLS: true, DefaultProject: "group/project", Projects: []string{"group/project"}}
	return cfg
}

func TestPrivateTokenAndEscapedProject(t *testing.T) {
	t.Setenv("TEST_GITLAB_TOKEN", "gl-secret")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Go decodes %2F into URL.Path; EscapedPath lets us verify the actual request target.
		if got := r.URL.EscapedPath(); got != "/api/v4/projects/group%2Fproject/merge_requests/42" {
			t.Errorf("escaped path = %s", got)
		}
		if got := r.Header.Get("PRIVATE-TOKEN"); got != "gl-secret" {
			t.Errorf("token = %q", got)
		}
		_, _ = w.Write([]byte(`{"iid":42}`))
	}))
	defer srv.Close()
	c, err := api.NewKindClient(testConfig(srv.URL), "gitlab")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := getMR(context.Background(), c, "group/project", 42); err != nil {
		t.Fatal(err)
	}
}

func TestToolsUseTheProfileOfTheCall(t *testing.T) {
	t.Setenv("TEST_GL_A", "token-a")
	t.Setenv("TEST_GL_B", "token-b")
	var hits []string
	server := func(name string) string {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits = append(hits, name+" "+r.Header.Get("PRIVATE-TOKEN")+" "+r.URL.EscapedPath())
			_, _ = w.Write([]byte(`{}`))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	cfg := config.Default()
	cfg.Profiles["a"] = config.Profile{Kind: "gitlab", URL: server("A"), TokenEnv: "TEST_GL_A", VerifyTLS: true, DefaultProject: "g/a"}
	cfg.Profiles["b"] = config.Profile{Kind: "gitlab", URL: server("B"), TokenEnv: "TEST_GL_B", VerifyTLS: true, DefaultProject: "g/b"}
	cfg.Defaults["gitlab"] = "a"

	ctx := context.Background()
	for _, args := range []map[string]any{{}, {"profile": "b"}} {
		if _, err := CallTool(ctx, cfg, "gitlab_get_project", args); err != nil {
			t.Fatal(err)
		}
	}
	if want := "A token-a /api/v4/projects/g%2Fa|B token-b /api/v4/projects/g%2Fb"; strings.Join(hits, "|") != want {
		t.Fatalf("requests = %v", hits)
	}
	if _, err := CallTool(ctx, cfg, "gitlab_get_project", map[string]any{"profile": "nope"}); err == nil {
		t.Fatal("unknown profile accepted")
	}
	for _, tool := range Tools(cfg) {
		if strings.HasPrefix(tool.Name, "gitlab_") && tool.InputSchema["properties"].(map[string]any)["profile"] == nil {
			t.Errorf("%s has no profile argument", tool.Name)
		}
	}
}

func TestNormalizePullRequest(t *testing.T) {
	res, err := vcs.Normalize("gitlab", "group/example", []byte(`[{"iid":7,"title":"Fix","description":"MR desc text","state":"opened","draft":false,"web_url":"https://gitlab.example/mr/7","author":{"username":"octo"},"source_branch":"fix","target_branch":"main"}]`), fillPullRequest)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].ID != 7 || res.Items[0].TargetBranch != "main" || res.Items[0].Description != "MR desc text" {
		t.Fatalf("items = %#v", res.Items)
	}
	for _, body := range []string{`[]`, `null`, `not json`, `{}`, `{"number":1.5,"iid":1.5}`} {
		if _, err := vcs.Single("gitlab", "o/r", "not found", []byte(body), nil, fillPullRequest); err == nil {
			t.Errorf("accepted %s", body)
		}
	}
}
