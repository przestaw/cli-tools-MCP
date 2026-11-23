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
	"unicode/utf8"

	"workctl/internal/config"
)

const githubDiff = "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1 +1 @@\n-old\n+new\n"

const gitlabChanges = `{"changes":[
 {"old_path":"m.go","new_path":"m.go","a_mode":"100644","b_mode":"100644","diff":"@@ -1 +1 @@\n-a\n+b\n"},
 {"old_path":"n.sh","new_path":"n.sh","a_mode":"0","b_mode":"100755","new_file":true,"diff":"@@ -0,0 +1 @@\n+x\n"},
 {"old_path":"d.go","new_path":"d.go","a_mode":"100644","b_mode":"0","deleted_file":true,"diff":"@@ -1 +0,0 @@\n-y\n"},
 {"old_path":"o.go","new_path":"r.go","a_mode":"100644","b_mode":"100644","renamed_file":true,"diff":""},
 {"old_path":"run","new_path":"run","a_mode":"100644","b_mode":"100755","diff":""},
 {"old_path":"img.png","new_path":"img.png","a_mode":"100644","b_mode":"100644","diff":""}]}`

const gitlabWant = `diff --git a/m.go b/m.go
--- a/m.go
+++ b/m.go
@@ -1 +1 @@
-a
+b
diff --git a/n.sh b/n.sh
new file mode 100755
--- /dev/null
+++ b/n.sh
@@ -0,0 +1 @@
+x
diff --git a/d.go b/d.go
deleted file mode 100644
--- a/d.go
+++ /dev/null
@@ -1 +0,0 @@
-y
diff --git a/o.go b/r.go
rename from o.go
rename to r.go
diff --git a/run b/run
old mode 100644
new mode 100755
diff --git a/img.png b/img.png
# diff not available (binary or too large file)
`

func diffForge(t *testing.T, kind string, failDiff bool, accept *string) *config.Config {
	t.Helper()
	t.Setenv("TEST_FORGE_TOKEN", "secret")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/repos/acme/app/pulls/5":
			if strings.Contains(r.Header.Get("Accept"), "diff") {
				*accept = r.Header.Get("Accept")
				if failDiff {
					http.Error(w, `{"message":"too big"}`, http.StatusNotAcceptable)
					return
				}
				w.Header().Set("Content-Type", "application/vnd.github.diff")
				_, _ = io.WriteString(w, githubDiff)
				return
			}
			_, _ = io.WriteString(w, `{"number":5,"title":"T","state":"open","user":{"login":"me"},"head":{"ref":"f"},"base":{"ref":"main"}}`)
		case "/repos/acme/app/pulls/5/files":
			_, _ = io.WriteString(w, `[{"filename":"a.go"}]`)
		case "/api/v4/projects/g%2Fp/merge_requests/3":
			_, _ = io.WriteString(w, `{"iid":3,"title":"T","state":"opened"}`)
		case "/api/v4/projects/g%2Fp/merge_requests/3/changes":
			if failDiff {
				http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
				return
			}
			_, _ = io.WriteString(w, gitlabChanges)
		default:
			_, _ = io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(srv.Close)
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

func showDiff(t *testing.T, cfg *config.Config, n int, extra map[string]any) map[string]json.RawMessage {
	t.Helper()
	a := map[string]any{"number": float64(n), "include": []any{"diff"}}
	for k, v := range extra {
		a[k] = v
	}
	out, err := CallTool(context.Background(), cfg, "pr_show", a)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	return got
}

func TestPRShowDiffPerProvider(t *testing.T) {
	for _, tc := range []struct {
		kind   string
		number int
		raw    bool
		want   string
	}{
		{"github", 5, false, githubDiff},
		{"github", 5, true, githubDiff},
		{"gitlab", 3, false, gitlabWant},
		{"gitlab", 3, true, gitlabWant},
	} {
		var accept string
		cfg := diffForge(t, tc.kind, false, &accept)
		extra := map[string]any{}
		if tc.raw {
			extra["format"] = "raw"
		}
		got := showDiff(t, cfg, tc.number, extra)
		var diff string
		if err := json.Unmarshal(got["diff"], &diff); err != nil {
			t.Fatalf("%s: diff is not a string: %s", tc.kind, got["diff"])
		}
		if diff != tc.want {
			t.Errorf("%s raw=%v diff:\n%s\nwant:\n%s", tc.kind, tc.raw, diff, tc.want)
		}
		if tc.kind == "github" && accept != "application/vnd.github.diff" {
			t.Errorf("Accept = %q", accept)
		}
	}
}

func TestPRShowDiffErrorKeepsOtherParts(t *testing.T) {
	var accept string
	cfg := diffForge(t, "github", true, &accept)
	a := map[string]any{"number": float64(5), "include": []any{"files", "diff"}}
	out, err := CallTool(context.Background(), cfg, "pr_show", a)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got["diff"]), `"error"`) || !strings.Contains(string(got["files"]), "a.go") || !strings.Contains(string(got["pull_request"]), `"T"`) {
		t.Fatalf("parts = %s", out)
	}
}

func TestCapDiff(t *testing.T) {
	const max = 20
	for _, tc := range []struct {
		name      string
		text      string
		truncated bool
		wantDiff  string
	}{
		{"below", "line1\nline2\n", false, "line1\nline2\n"},
		{"exactly at cap", "123456789\n123456789\n", false, "123456789\n123456789\n"},
		{"above cap cuts at a line", "123456789\n123456789\nrest\n", true, "123456789\n123456789\n"},
		{"cut mid line goes back to the previous line", "12345\n1234567890123456789\n", true, "12345\n"},
		{"no newline cuts at a rune", strings.Repeat("ż", 15), true, strings.Repeat("ż", 10)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := capDiff(tc.text, max)
			if !tc.truncated {
				var s string
				if err := json.Unmarshal(data, &s); err != nil || s != tc.wantDiff {
					t.Fatalf("got %s (%v)", data, err)
				}
				return
			}
			var obj struct {
				Diff          string `json:"diff"`
				Truncated     bool   `json:"truncated"`
				BytesTotal    int    `json:"bytes_total"`
				BytesReturned int    `json:"bytes_returned"`
				Hint          string `json:"hint"`
			}
			if err := json.Unmarshal(data, &obj); err != nil {
				t.Fatalf("%s: %v", data, err)
			}
			if !obj.Truncated || obj.Diff != tc.wantDiff || obj.BytesTotal != len(tc.text) || obj.BytesReturned != len(tc.wantDiff) || obj.Hint == "" {
				t.Fatalf("got %+v", obj)
			}
			if !utf8.ValidString(obj.Diff) || len(obj.Diff) > max {
				t.Fatalf("diff invalid or over the cap: %q", obj.Diff)
			}
		})
	}
}

func TestPRShowDiffAboveCap(t *testing.T) {
	old := diffCap
	diffCap = 30
	t.Cleanup(func() { diffCap = old })
	var accept string
	cfg := diffForge(t, "github", false, &accept)
	got := showDiff(t, cfg, 5, nil)
	var obj struct {
		Truncated  bool `json:"truncated"`
		BytesTotal int  `json:"bytes_total"`
	}
	if err := json.Unmarshal(got["diff"], &obj); err != nil || !obj.Truncated || obj.BytesTotal != len(githubDiff) {
		t.Fatalf("diff = %s (%v)", got["diff"], err)
	}
}

func TestCLIShowIncludeDiff(t *testing.T) {
	var accept string
	cfg := diffForge(t, "github", false, &accept)
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	err = Command(cfg, []string{"show", "--provider", "github", "--include", "files,diff", "5"})
	os.Stdout = old
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(f.Name())
	var got map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("%s: %v", data, err)
	}
	var diff string
	if err := json.Unmarshal(got["diff"], &diff); err != nil || diff != githubDiff || !strings.Contains(string(got["files"]), "a.go") {
		t.Fatalf("output = %s", data)
	}
}

func TestUnknownPartListsDiff(t *testing.T) {
	var accept string
	cfg := diffForge(t, "github", false, &accept)
	_, err := CallTool(context.Background(), cfg, "pr_show", map[string]any{"number": float64(5), "include": []any{"nope"}})
	if err == nil || !strings.Contains(err.Error(), "files, diff,") {
		t.Fatalf("error = %v", err)
	}
}
