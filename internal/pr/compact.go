// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package pr

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Compact output: the few fields an agent needs, the same for GitHub and GitLab. format "raw" keeps the provider
// JSON. A shape that cannot be read is passed through unchanged rather than failing the call.

// field reads a nested value by a dotted path ("user.login").
func field(v any, path string) any {
	for _, key := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[key]
	}
	return v
}

func str(v any, paths ...string) string {
	for _, p := range paths {
		switch x := field(v, p).(type) {
		case string:
			if x != "" {
				return x
			}
		case float64:
			return fmt.Sprintf("%.0f", x)
		case bool:
			return fmt.Sprint(x)
		}
	}
	return ""
}

func num(v any, path string) any {
	if x, ok := field(v, path).(float64); ok {
		return int64(x)
	}
	return nil
}

// list returns the items of a JSON array, or of the array under key when the answer is an object.
func list(v any, key string) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	if a, ok := field(v, key).([]any); ok {
		return a
	}
	return nil
}

// row drops empty values so the compact output stays small.
func row(kv ...any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		switch v := kv[i+1].(type) {
		case nil:
		case string:
			if v != "" {
				out[kv[i].(string)] = v
			}
		default:
			out[kv[i].(string)] = v
		}
	}
	return out
}

func each(items []any, fn func(any) map[string]any) []map[string]any {
	out := []map[string]any{}
	for _, it := range items {
		if r := fn(it); r != nil {
			out = append(out, r)
		}
	}
	return out
}

// compactPart shrinks one pr_show include part.
func compactPart(kind, part string, data []byte) []byte {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return data
	}
	var out any
	switch kind + "/" + part {
	case "github/files":
		out = each(list(v, ""), func(f any) map[string]any {
			return row("path", str(f, "filename"), "status", str(f, "status"), "additions", num(f, "additions"), "deletions", num(f, "deletions"))
		})
	case "gitlab/files":
		out = each(list(v, "changes"), func(f any) map[string]any {
			status := "modified"
			switch {
			case field(f, "new_file") == true:
				status = "added"
			case field(f, "deleted_file") == true:
				status = "removed"
			case field(f, "renamed_file") == true:
				status = "renamed"
			}
			return row("path", str(f, "new_path"), "status", status)
		})
	case "github/reviews":
		out = each(list(v, ""), func(r any) map[string]any {
			return row("author", str(r, "user.login"), "state", strings.ToLower(str(r, "state")), "submitted", str(r, "submitted_at"))
		})
	case "gitlab/reviews", "gitlab/reviewers":
		out = each(list(v, "approved_by"), func(a any) map[string]any {
			if part == "reviewers" {
				return row("name", str(a, "user.username"), "kind", "user")
			}
			return row("author", str(a, "user.username"), "state", "approved")
		})
	case "github/reviewers":
		users := each(list(v, "users"), func(u any) map[string]any { return row("name", str(u, "login"), "kind", "user") })
		out = append(users, each(list(v, "teams"), func(t any) map[string]any { return row("name", str(t, "slug"), "kind", "team") })...)
	case "github/comments":
		out = each(list(v, ""), func(c any) map[string]any {
			return row("author", str(c, "user.login"), "path", str(c, "path"), "line", num(c, "line"), "body", str(c, "body"), "created", str(c, "created_at"))
		})
	case "gitlab/comments":
		out = each(list(v, ""), func(n any) map[string]any {
			if field(n, "position") == nil {
				return nil
			}
			return row("author", str(n, "author.username"), "path", str(n, "position.new_path", "position.old_path"), "line", num(n, "position.new_line"),
				"body", str(n, "body"), "created", str(n, "created_at"), "resolved", field(n, "resolved"))
		})
	case "github/discussion":
		out = each(list(v, ""), func(c any) map[string]any {
			return row("author", str(c, "user.login"), "body", str(c, "body"), "created", str(c, "created_at"))
		})
	case "gitlab/discussion":
		out = each(list(v, ""), func(n any) map[string]any {
			if field(n, "system") == true || field(n, "position") != nil {
				return nil
			}
			return row("author", str(n, "author.username"), "body", str(n, "body"), "created", str(n, "created_at"))
		})
	case "github/checks":
		out = each(list(v, "check_runs"), func(c any) map[string]any {
			return row("name", str(c, "name"), "status", str(c, "status"), "conclusion", str(c, "conclusion"), "url", str(c, "html_url", "details_url"))
		})
	case "gitlab/checks":
		out = each(list(v, ""), func(p any) map[string]any {
			return row("name", "pipeline "+str(p, "id"), "status", str(p, "status"), "url", str(p, "web_url"))
		})
	default:
		return data
	}
	b, err := json.Marshal(out)
	if err != nil {
		return data
	}
	return b
}

func compactRun(r any) map[string]any {
	return row("id", num(r, "id"), "name", str(r, "name"), "url", str(r, "html_url", "web_url"), "ref", str(r, "head_branch", "ref"),
		"status", str(r, "status"), "conclusion", str(r, "conclusion"), "created", str(r, "created_at"))
}

// compactCI shrinks a ci answer: a list of runs, one run, or {run, jobs}.
func compactCI(data []byte) []byte {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return data
	}
	var out any
	switch {
	case field(v, "run") != nil && field(v, "jobs") != nil:
		out = map[string]any{"run": compactRun(field(v, "run")), "jobs": each(list(field(v, "jobs"), "jobs"), func(j any) map[string]any {
			return row("id", num(j, "id"), "name", str(j, "name"), "status", str(j, "status"), "conclusion", str(j, "conclusion"))
		})}
	case field(v, "workflow_runs") != nil:
		out = each(list(v, "workflow_runs"), compactRun)
	case func() bool { _, ok := v.([]any); return ok }():
		out = each(list(v, ""), compactRun)
	case field(v, "id") != nil:
		out = compactRun(v)
	default:
		return data
	}
	b, err := json.Marshal(out)
	if err != nil {
		return data
	}
	return b
}

// compactWrite shrinks the answer of pr_create, pr_edit and comment to what identifies the result.
func compactWrite(data []byte) []byte {
	var v any
	if json.Unmarshal(data, &v) != nil {
		return data
	}
	out := row("number", num(v, "number"), "id", num(v, "id"), "url", str(v, "html_url", "web_url"), "state", str(v, "state"))
	if n := num(v, "iid"); n != nil {
		out["number"] = n
		delete(out, "id")
	}
	if len(out) == 0 {
		return data
	}
	b, _ := json.Marshal(out)
	return b
}
