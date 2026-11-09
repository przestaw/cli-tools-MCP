// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package pr

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Recorded answers (trimmed) of GitHub and GitLab, and the compact shape each must become.
var compactCases = []struct {
	kind, part, answer, want string
}{
	{"github", "files", `[{"filename":"a.go","status":"modified","additions":3,"deletions":1,"patch":"@@ -1 +1 @@"}]`,
		`[{"additions":3,"deletions":1,"path":"a.go","status":"modified"}]`},
	{"gitlab", "files", `{"changes":[{"new_path":"a.go","old_path":"a.go","diff":"@@"},{"new_path":"b.go","new_file":true},{"new_path":"c.go","deleted_file":true},{"new_path":"d.go","renamed_file":true}]}`,
		`[{"path":"a.go","status":"modified"},{"path":"b.go","status":"added"},{"path":"c.go","status":"removed"},{"path":"d.go","status":"renamed"}]`},
	{"github", "reviews", `[{"user":{"login":"ann"},"state":"APPROVED","submitted_at":"2026-11-09T10:00:00Z","body":"lgtm"}]`,
		`[{"author":"ann","state":"approved","submitted":"2026-11-09T10:00:00Z"}]`},
	{"gitlab", "reviews", `{"approved":true,"approved_by":[{"user":{"username":"bob","id":1}}]}`,
		`[{"author":"bob","state":"approved"}]`},
	{"github", "reviewers", `{"users":[{"login":"ann"}],"teams":[{"slug":"core"}]}`,
		`[{"kind":"user","name":"ann"},{"kind":"team","name":"core"}]`},
	{"gitlab", "reviewers", `{"approved_by":[{"user":{"username":"bob"}}]}`,
		`[{"kind":"user","name":"bob"}]`},
	{"github", "comments", `[{"user":{"login":"ann"},"path":"a.go","line":7,"body":"nit","created_at":"t1","diff_hunk":"@@"}]`,
		`[{"author":"ann","body":"nit","created":"t1","line":7,"path":"a.go"}]`},
	{"gitlab", "comments", `[{"author":{"username":"bob"},"body":"why?","created_at":"t1","position":{"new_path":"a.go","new_line":3},"resolved":false},
		{"author":{"username":"bob"},"body":"general","created_at":"t2","position":null},{"system":true,"body":"added 1 commit","author":{"username":"bot"}}]`,
		`[{"author":"bob","body":"why?","created":"t1","line":3,"path":"a.go","resolved":false}]`},
	{"github", "discussion", `[{"user":{"login":"ann"},"body":"ping","created_at":"t1","reactions":{}}]`,
		`[{"author":"ann","body":"ping","created":"t1"}]`},
	{"gitlab", "discussion", `[{"author":{"username":"bob"},"body":"general","created_at":"t2"},{"system":true,"body":"added 1 commit","author":{"username":"bot"}},
		{"author":{"username":"bob"},"body":"inline","position":{"new_path":"a.go"}}]`,
		`[{"author":"bob","body":"general","created":"t2"}]`},
	{"github", "checks", `{"total_count":1,"check_runs":[{"name":"build","status":"completed","conclusion":"success","html_url":"https://g/c/1","output":{}}]}`,
		`[{"conclusion":"success","name":"build","status":"completed","url":"https://g/c/1"}]`},
	{"gitlab", "checks", `[{"id":42,"status":"failed","web_url":"https://l/p/42","sha":"abc"}]`,
		`[{"name":"pipeline 42","status":"failed","url":"https://l/p/42"}]`},
}

func TestCompactPartsHaveTheSameShapeForBothProviders(t *testing.T) {
	for _, c := range compactCases {
		got := string(compactPart(c.kind, c.part, []byte(c.answer)))
		if got != c.want {
			t.Errorf("%s %s:\n got  %s\n want %s", c.kind, c.part, got, c.want)
		}
	}
	if got := string(compactPart("github", "files", []byte("not json"))); got != "not json" {
		t.Errorf("unreadable answers pass through, got %q", got)
	}
}

func TestCompactCIAndWrites(t *testing.T) {
	cases := map[string]string{
		`{"workflow_runs":[{"id":9,"name":"ci","html_url":"https://g/r/9","head_branch":"main","status":"completed","conclusion":"success","created_at":"t","actor":{}}]}`: `[{"conclusion":"success","created":"t","id":9,"name":"ci","ref":"main","status":"completed","url":"https://g/r/9"}]`,
		`[{"id":9,"web_url":"https://l/p/9","ref":"main","status":"running","created_at":"t","sha":"x"}]`:                                                                  `[{"created":"t","id":9,"ref":"main","status":"running","url":"https://l/p/9"}]`,
		`{"run":{"id":9,"status":"completed"},"jobs":{"jobs":[{"id":7,"name":"build","status":"completed","conclusion":"failure","steps":[]}]}}`:                           `{"jobs":[{"conclusion":"failure","id":7,"name":"build","status":"completed"}],"run":{"id":9,"status":"completed"}}`,
		`{"run":{"id":9,"status":"failed"},"jobs":[{"id":7,"name":"build","status":"failed"}]}`:                                                                            `{"jobs":[{"id":7,"name":"build","status":"failed"}],"run":{"id":9,"status":"failed"}}`,
	}
	for answer, want := range cases {
		if got := string(compactCI([]byte(answer))); got != want {
			t.Errorf("ci %s:\n got  %s\n want %s", answer, got, want)
		}
	}
	writes := map[string]string{
		`{"number":5,"html_url":"https://g/p/5","state":"open","id":123,"user":{}}`: `{"id":123,"number":5,"state":"open","url":"https://g/p/5"}`,
		`{"iid":3,"id":999,"web_url":"https://l/mr/3","state":"opened"}`:            `{"number":3,"state":"opened","url":"https://l/mr/3"}`,
		`{"id":77,"html_url":"https://g/c/77","body":"hi"}`:                         `{"id":77,"url":"https://g/c/77"}`,
	}
	for answer, want := range writes {
		if got := string(compactWrite([]byte(answer))); got != want {
			t.Errorf("write %s:\n got  %s\n want %s", answer, got, want)
		}
	}
}

func TestToolsReturnCompactByDefaultAndRawOnRequest(t *testing.T) {
	var seen []string
	cfg := forgeConfig(t, "github", &seen)
	ctx := context.Background()
	out, err := CallTool(ctx, cfg, "pr_show", map[string]any{"number": float64(5), "include": []any{"files", "checks"}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if compactJSON(got["files"]) != `[{"path":"a.go"}]` || compactJSON(got["checks"]) != `[{"conclusion":"success","name":"build"}]` {
		t.Fatalf("compact parts = %s / %s", got["files"], got["checks"])
	}
	out, err = CallTool(ctx, cfg, "pr_show", map[string]any{"number": float64(5), "include": []any{"files"}, "format": "raw"})
	if err != nil || !strings.Contains(string(out), `"filename": "a.go"`) {
		t.Fatalf("raw parts = %s %v", out, err)
	}
	if out, err := CallTool(ctx, cfg, "ci", map[string]any{"run": float64(9), "include": []any{"jobs"}}); err != nil || string(out) != `{"jobs":[{"id":7}],"run":{"id":9}}` {
		t.Fatalf("compact ci = %s %v", out, err)
	}
	if out, err := CallTool(ctx, cfg, "ci", map[string]any{"run": float64(9), "format": "raw"}); err != nil || string(out) != `{"id":9}` {
		t.Fatalf("raw ci = %s %v", out, err)
	}
}

// compactJSON re-encodes JSON without indentation, so tests compare content, not layout.
func compactJSON(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
