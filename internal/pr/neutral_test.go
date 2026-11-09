// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package pr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"workctl/internal/config"
)

// fakeForge answers the GitHub and GitLab endpoints the neutral tools reach and records every request.
func fakeForge(t *testing.T, seen *[]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		*seen = append(*seen, r.Method+" "+r.URL.EscapedPath()+"?"+r.URL.RawQuery+" "+string(body))
		w.Header().Set("Content-Type", "application/json")
		switch p := r.URL.EscapedPath(); {
		case p == "/repos/acme/app/pulls/5":
			_, _ = io.WriteString(w, `{"number":5,"title":"T","state":"open","html_url":"https://github.test/acme/app/pull/5","user":{"login":"me"},"head":{"ref":"f","sha":"abc"},"base":{"ref":"main"}}`)
		case p == "/repos/acme/app/pulls/5/files":
			_, _ = io.WriteString(w, `[{"filename":"a.go"}]`)
		case p == "/repos/acme/app/commits/abc/check-runs":
			_, _ = io.WriteString(w, `{"check_runs":[{"name":"build","conclusion":"success"}]}`)
		case p == "/repos/acme/app/actions/jobs/7/logs":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "step 1\nok\n")
		case p == "/repos/acme/app/actions/runs/9", p == "/api/v4/projects/g%2Fp/pipelines/9":
			_, _ = io.WriteString(w, `{"id":9}`)
		case p == "/repos/acme/app/actions/runs/9/jobs", p == "/api/v4/projects/g%2Fp/pipelines/9/jobs":
			_, _ = io.WriteString(w, `{"jobs":[{"id":7}]}`)
		case p == "/api/v4/projects/g%2Fp/repository/files/a%2Fb.txt/raw":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = io.WriteString(w, "content")
		case p == "/api/v4/projects/g%2Fp/merge_requests/3":
			_, _ = io.WriteString(w, `{"iid":3,"state":"closed"}`)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func forgeConfig(t *testing.T, kind string, seen *[]string) *config.Config {
	t.Helper()
	t.Setenv("TEST_FORGE_TOKEN", "secret")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := fakeForge(t, seen)
	cfg := config.Default()
	p := config.Profile{Kind: kind, URL: srv.URL, TokenEnv: "TEST_FORGE_TOKEN", VerifyTLS: true}
	if kind == "github" {
		p.DefaultRepo = "acme/app"
	} else {
		p.DefaultProject = "g/p"
	}
	cfg.Profiles[kind] = p
	return cfg
}

func TestPRShowIncludeAddsParts(t *testing.T) {
	var seen []string
	cfg := forgeConfig(t, "github", &seen)
	out, err := CallTool(context.Background(), cfg, "pr_show", map[string]any{"number": float64(5), "include": []any{"files", "checks", "nope"}})
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("unknown part: %v", err)
	}
	out, err = CallTool(context.Background(), cfg, "pr_show", map[string]any{"number": float64(5), "include": []any{"files", "checks"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if !strings.Contains(string(got["pull_request"]), `"T"`) || !strings.Contains(string(got["files"]), "a.go") || !strings.Contains(string(got["checks"]), "build") {
		t.Fatalf("parts = %s", out)
	}
}

func TestCIForBothProviders(t *testing.T) {
	var seen []string
	cfg := forgeConfig(t, "github", &seen)
	out, err := CallTool(context.Background(), cfg, "ci", map[string]any{"job": float64(7)})
	if err != nil {
		t.Fatal(err)
	}
	var file struct{ Path string }
	if err := json.Unmarshal(out, &file); err != nil || !strings.HasSuffix(file.Path, "job-7.log") {
		t.Fatalf("github job log: %s %v", out, err)
	}
	if data, _ := os.ReadFile(file.Path); string(data) != "step 1\nok\n" {
		t.Fatalf("log file = %q", data)
	}
	out, err = CallTool(context.Background(), cfg, "ci", map[string]any{"run": float64(9), "include": []any{"jobs"}})
	if err != nil || !strings.Contains(string(out), `"run"`) || !strings.Contains(string(out), `"jobs"`) {
		t.Fatalf("github run with jobs: %s %v", out, err)
	}
	if _, err := CallTool(context.Background(), cfg, "ci", map[string]any{"ref": "main"}); err != nil || !strings.Contains(seen[len(seen)-1], "/repos/acme/app/actions/runs?branch=main") {
		t.Fatalf("github list: %v %s", err, seen[len(seen)-1])
	}

	seen = nil
	cfg = forgeConfig(t, "gitlab", &seen)
	if out, err := CallTool(context.Background(), cfg, "ci", map[string]any{"run": float64(9), "include": []any{"jobs"}}); err != nil || !strings.Contains(string(out), `"jobs"`) {
		t.Fatalf("gitlab run with jobs: %s %v", out, err)
	}
	if _, err := CallTool(context.Background(), cfg, "ci", map[string]any{"job": float64(7)}); err != nil || !strings.Contains(seen[len(seen)-1], "/api/v4/projects/g%2Fp/jobs/7/trace") {
		t.Fatalf("gitlab job trace: %v %v", err, seen)
	}
}

func TestFileAndWritesOnGitLab(t *testing.T) {
	var seen []string
	cfg := forgeConfig(t, "gitlab", &seen)
	out, err := CallTool(context.Background(), cfg, "file", map[string]any{"path": "a/b.txt", "ref": "main"})
	if err != nil || string(out) != "content" {
		t.Fatalf("file: %q %v", out, err)
	}
	edit := map[string]any{"number": float64(3), "state": "closed", "title": "New"}
	if _, err := CallTool(context.Background(), cfg, "pr_edit", edit); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("pr_edit without allow_write: %v", err)
	}
	cfg.Policy.AllowWrite = true
	if _, err := CallTool(context.Background(), cfg, "pr_edit", edit); err != nil {
		t.Fatal(err)
	}
	last := seen[len(seen)-1]
	if !strings.HasPrefix(last, "PUT /api/v4/projects/g%2Fp/merge_requests/3?") || !strings.Contains(last, "state_event=close") || !strings.Contains(last, "title=New") {
		t.Fatalf("gitlab edit = %s", last)
	}
	if _, err := CallTool(context.Background(), cfg, "pr_edit", map[string]any{"number": float64(3), "add_reviewers": []any{"x"}}); err == nil || !strings.Contains(err.Error(), "github only") {
		t.Fatalf("gitlab reviewers: %v", err)
	}
	if _, err := CallTool(context.Background(), cfg, "comment", map[string]any{"number": float64(3), "body": "hi"}); err != nil || !strings.Contains(seen[len(seen)-1], "/merge_requests/3/notes?body=hi") {
		t.Fatalf("gitlab comment: %v %s", err, seen[len(seen)-1])
	}
	if _, err := CallTool(context.Background(), cfg, "pr_create", map[string]any{"head": "f", "base": "main", "title": "T", "draft": true}); err != nil ||
		!strings.Contains(seen[len(seen)-1], "source_branch=f") || !strings.Contains(seen[len(seen)-1], "title=Draft%3A+T") {
		t.Fatalf("gitlab create: %v %s", err, seen[len(seen)-1])
	}
}

func TestNeutralToolsRefuseOtherKinds(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["ado"] = config.Profile{Kind: "azure", URL: "https://dev.azure.test/org", DefaultProject: "P"}
	if _, err := CallTool(context.Background(), cfg, "file", map[string]any{"path": "a"}); err == nil || !strings.Contains(err.Error(), "cli tool") {
		t.Fatalf("azure: %v", err)
	}
	if _, err := CallTool(context.Background(), cfg, "comment", map[string]any{"issue": "ABC-1", "body": "x"}); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("issue comment without allow_write: %v", err)
	}
}
