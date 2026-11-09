// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package issue

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/config"
)

// fakeJira answers the endpoints the issue layer uses, with a localised resolution list, and records writes.
type fakeJira struct {
	writes []string // "METHOD path body"
	reads  []string
}

const issueJSON = `{"key":"ABC-1","fields":{
 "summary":"Refuse opens the ticket","status":{"name":"Review"},"resolution":null,"issuetype":{"name":"Bug"},
 "assignee":{"name":"zareba","displayName":"Z"},"fixVersions":[{"id":"9","name":"TODO"}],"labels":["wats"],
 "description":%s,
 "issuelinks":[{"type":{"inward":"is cloned by","outward":"clones"},"inwardIssue":{"key":"ABC-2","fields":{"summary":"Clone","status":{"name":"Resolved"}}}},
               {"type":{"inward":"is duplicated by","outward":"duplicates"},"outwardIssue":{"key":"ABC-3","fields":{"summary":"Other","status":{"name":"Open"}}}}],
 "comment":{"comments":[{"author":{"name":"a"},"created":"2026-11-09","body":%s}]}}}`

const transitionsJSON = `{"transitions":[
 {"id":"3","name":"Reopen","to":{"name":"Reopened"},"fields":{}},
 {"id":"5","name":"Rozwiąż zgłoszenie","to":{"name":"Rozwiązane"},"fields":{
   "resolution":{"allowedValues":[{"id":"1","name":"Naprawiony"},{"id":"3","name":"Duplikat"},{"id":"10000","name":"Gotowe"}]},
   "fixVersions":{"allowedValues":[{"id":"17563","name":"REJECTED"},{"id":"9","name":"TODO"}]}}},
 {"id":"2","name":"Zamknij","to":{"name":"Zamknięte"},"fields":{"resolution":{"allowedValues":[{"id":"3","name":"Duplikat"}]}}}]}`

func (f *fakeJira) server(t *testing.T, cloud bool) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodGet && !strings.Contains(r.URL.Path, "/search") {
			f.writes = append(f.writes, r.Method+" "+r.URL.Path+" "+string(body))
			w.WriteHeader(http.StatusNoContent)
			return
		}
		f.reads = append(f.reads, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery+" "+string(body))
		desc, cbody := `"plain text"`, `"hi"`
		if cloud {
			desc = `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"adf text"}]}]}`
		}
		switch r.URL.Path {
		case "/rest/api/2/issue/ABC-1":
			_, _ = io.WriteString(w, strings.Replace(strings.Replace(issueJSON, "%s", desc, 1), "%s", cbody, 1))
		case "/rest/api/2/issue/ABC-1/transitions":
			_, _ = io.WriteString(w, transitionsJSON)
		case "/rest/api/2/issue/ABC-1/editmeta":
			_, _ = io.WriteString(w, `{"fields":{"fixVersions":{"allowedValues":[{"id":"17563","name":"REJECTED"}]}}}`)
		case "/rest/api/2/issueLinkType":
			_, _ = io.WriteString(w, `{"issueLinkTypes":[{"name":"Duplicate","inward":"is duplicated by","outward":"duplicates"},{"name":"Relates","inward":"relates to","outward":"relates to"}]}`)
		case "/rest/api/2/search":
			_, _ = io.WriteString(w, `{"total":3,"startAt":0,"issues":[{"key":"ABC-1","fields":{"summary":"s","status":{"name":"Open"}}}]}`)
		case "/rest/api/2/search/jql":
			_, _ = io.WriteString(w, `{"nextPageToken":"tok","issues":[{"key":"ABC-1","fields":{"summary":"s","status":{"name":"Open"}}}]}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setup(t *testing.T, kind string) (*config.Config, *fakeJira) {
	t.Helper()
	t.Setenv("TEST_ISSUE_TOKEN", "secret")
	f := &fakeJira{}
	srv := f.server(t, kind == "atlassian")
	cfg := config.Default()
	p := config.Profile{Kind: kind, URL: srv.URL, TokenEnv: "TEST_ISSUE_TOKEN", VerifyTLS: true}
	if kind == "atlassian" {
		p.Auth, p.Username = "basic", "me@example.test"
	}
	cfg.Profiles[kind] = p
	return cfg, f
}

func forBoth(t *testing.T, fn func(t *testing.T, kind string, cfg *config.Config, f *fakeJira)) {
	for _, kind := range Kinds {
		t.Run(kind, func(t *testing.T) {
			cfg, f := setup(t, kind)
			fn(t, kind, cfg, f)
		})
	}
}

func open(t *testing.T, cfg *config.Config) *Backend {
	t.Helper()
	b, err := Open(cfg, "", "ABC-1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestShowNormalisesBothProviders(t *testing.T) {
	forBoth(t, func(t *testing.T, kind string, cfg *config.Config, _ *fakeJira) {
		is, err := open(t, cfg).Show(context.Background(), "ABC-1", Include{Comments: true, Transitions: true})
		if err != nil {
			t.Fatal(err)
		}
		wantDesc := map[string]string{"jira": "plain text", "atlassian": "adf text"}[kind]
		if is.Status != "Review" || is.Assignee != "zareba" || strings.Join(is.FixVersions, ",") != "TODO" ||
			!strings.Contains(is.Description, wantDesc) || !strings.HasSuffix(is.URL, "/browse/ABC-1") {
			t.Fatalf("issue = %+v", is)
		}
		if len(is.Links) != 2 || is.Links[0] != (Link{"is cloned by", "ABC-2", "Clone", "Resolved"}) || is.Links[1].Relation != "duplicates" || is.Links[1].Key != "ABC-3" {
			t.Fatalf("links = %+v", is.Links)
		}
		if len(is.Comments) != 1 || is.Comments[0].Body != "hi" || len(is.Transitions) != 3 || is.Transitions[1].To != "Rozwiązane" {
			t.Fatalf("comments = %+v transitions = %+v", is.Comments, is.Transitions)
		}
	})
}

func TestDuplicatePlanResolvesLocalisedNamesAndLinkDirection(t *testing.T) {
	forBoth(t, func(t *testing.T, _ string, cfg *config.Config, f *fakeJira) {
		b := open(t, cfg)
		u := Update{Resolve: true, Resolution: "Duplicate", FixVersions: []string{"rejected"}, Comment: "dup", Link: &LinkSpec{Type: "duplicates", Key: "ABC-9"}}
		steps, err := b.Plan(context.Background(), "ABC-1", u)
		if err != nil {
			t.Fatal(err)
		}
		if len(f.writes) != 0 {
			t.Fatalf("planning wrote: %v", f.writes)
		}
		if _, err := b.Apply(context.Background(), steps); err != nil {
			t.Fatal(err)
		}
		if len(f.writes) != 2 {
			t.Fatalf("writes = %v", f.writes)
		}
		var link, tr map[string]any
		_ = json.Unmarshal([]byte(strings.SplitN(f.writes[0], " ", 3)[2]), &link)
		_ = json.Unmarshal([]byte(strings.SplitN(f.writes[1], " ", 3)[2]), &tr)
		if !strings.HasPrefix(f.writes[0], "POST /rest/api/2/issueLink ") || link["inwardIssue"].(map[string]any)["key"] != "ABC-1" ||
			link["outwardIssue"].(map[string]any)["key"] != "ABC-9" || link["type"].(map[string]any)["name"] != "Duplicate" {
			t.Fatalf("link = %s", f.writes[0])
		}
		want := `{"fields":{"fixVersions":[{"id":"17563"}],"resolution":{"id":"3"}},"transition":{"id":"5"},"update":{"comment":[{"add":{"body":"dup"}}]}}`
		if got, _ := json.Marshal(tr); string(got) != want || !strings.HasPrefix(f.writes[1], "POST /rest/api/2/issue/ABC-1/transitions ") {
			t.Fatalf("transition = %s\nwant %s", got, want)
		}
	})
}

func TestPlanEdgeCases(t *testing.T) {
	forBoth(t, func(t *testing.T, kind string, cfg *config.Config, _ *fakeJira) {
		b := open(t, cfg)
		ctx := context.Background()
		steps, err := b.Plan(ctx, "ABC-1", Update{Link: &LinkSpec{Type: "is duplicated by", Key: "ABC-9"}})
		if err != nil || steps[0].Describe != "link ABC-1 is duplicated by ABC-9" ||
			steps[0].body.(map[string]any)["inwardIssue"].(map[string]string)["key"] != "ABC-9" {
			t.Fatalf("inward link: %+v %v", steps, err)
		}
		if _, err := b.Plan(ctx, "ABC-1", Update{Transition: "Fly"}); err == nil || !strings.Contains(err.Error(), `5 "Rozwiąż zgłoszenie" -> Rozwiązane`) {
			t.Fatalf("unknown transition: %v", err)
		}
		if _, err := b.Plan(ctx, "ABC-1", Update{Resolution: "Done"}); err == nil || !strings.Contains(err.Error(), "transition") {
			t.Fatalf("resolution without transition: %v", err)
		}
		if _, err := b.Plan(ctx, "ABC-1", Update{Transition: "Zamknięte", Resolution: "Nope"}); err == nil || !strings.Contains(err.Error(), "Duplikat") {
			t.Fatalf("bad resolution: %v", err)
		}
		steps, err = b.Plan(ctx, "ABC-1", Update{FixVersions: []string{"REJECTED"}, Assignee: "u1", Comment: "c"})
		if err != nil || len(steps) != 3 || steps[0].path != "/rest/api/2/issue/ABC-1" || steps[2].path != "/rest/api/2/issue/ABC-1/comment" {
			t.Fatalf("edit steps: %+v %v", steps, err)
		}
		wantAssignee := map[string]string{"jira": "name", "atlassian": "accountId"}[kind]
		if steps[1].body.(map[string]string)[wantAssignee] != "u1" {
			t.Fatalf("assignee body = %v", steps[1].body)
		}
		if _, err := b.Plan(ctx, "ABC-1", Update{}); err == nil {
			t.Fatal("empty update must fail")
		}
	})
}

func TestSearchPaginationPerProvider(t *testing.T) {
	forBoth(t, func(t *testing.T, kind string, cfg *config.Config, f *fakeJira) {
		b := open(t, cfg)
		r, err := b.Search(context.Background(), "project = ABC", 0, 10, "")
		if err != nil || len(r.Issues) != 1 || r.Issues[0].Key != "ABC-1" {
			t.Fatalf("search: %+v %v", r, err)
		}
		last := f.reads[len(f.reads)-1]
		if kind == "jira" && (!strings.Contains(last, "/rest/api/2/search?") || !strings.Contains(last, `"startAt":0`) || r.Total != 3) {
			t.Fatalf("server search: %s", last)
		}
		if kind == "atlassian" {
			if !strings.Contains(last, "/rest/api/2/search/jql") || r.NextPageToken != "tok" {
				t.Fatalf("cloud search: %s %+v", last, r)
			}
			if _, err := b.Search(context.Background(), "x", 5, 10, ""); err == nil {
				t.Fatal("cloud start_at must fail")
			}
		}
	})
}

func TestUpdateToolNeedsAllowWriteUnlessDryRun(t *testing.T) {
	cfg, f := setup(t, "jira")
	args := map[string]any{"key": "ABC-1", "resolve": true, "resolution": "Duplicate", "link_type": "duplicates", "link_key": "ABC-9"}
	if _, err := CallTool(context.Background(), cfg, "issue_update", args); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("err = %v", err)
	}
	args["dry_run"] = true
	out, err := CallTool(context.Background(), cfg, "issue_update", args)
	if err != nil || !strings.Contains(string(out), `"dry_run": true`) || !strings.Contains(string(out), "link ABC-1 duplicates ABC-9") || len(f.writes) != 0 {
		t.Fatalf("dry run: %s %v writes=%v", out, err, f.writes)
	}
	cfg.Policy.AllowWrite = true
	delete(args, "dry_run")
	out, err = CallTool(context.Background(), cfg, "issue_update", args)
	if err != nil || len(f.writes) != 2 || !strings.Contains(string(out), `"key": "ABC-1"`) {
		t.Fatalf("apply: %s %v writes=%v", out, err, f.writes)
	}
	out, err = CallTool(context.Background(), cfg, "issue_show", map[string]any{"key": "ABC-1", "format": "raw"})
	if err != nil || !strings.Contains(string(out), `"issuelinks"`) {
		t.Fatalf("raw: %s %v", out, err)
	}
}

func runCLI(t *testing.T, cfg *config.Config, terminal bool, stdin string, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	oldIn, oldOut, oldTerm := in, out, isTerminal
	in, out, isTerminal = strings.NewReader(stdin), &buf, func() bool { return terminal }
	defer func() { in, out, isTerminal = oldIn, oldOut, oldTerm }()
	err := Command(cfg, args)
	return buf.String(), err
}

func TestCLIShortcutsConfirmBeforeWriting(t *testing.T) {
	cfg, f := setup(t, "jira")
	cfg.Policy.AllowWrite = true
	text, err := runCLI(t, cfg, false, "", "ABC-1")
	if err != nil || !strings.Contains(text, "ABC-1  Review  Bug") || !strings.Contains(text, "duplicates ABC-3") {
		t.Fatalf("show: %q %v", text, err)
	}
	if _, err := runCLI(t, cfg, false, "", "dup", "ABC-1", "ABC-9", "--fix", "REJECTED"); err == nil || !strings.Contains(err.Error(), "--yes") || len(f.writes) != 0 {
		t.Fatalf("no terminal, no --yes: %v writes=%v", err, f.writes)
	}
	if text, err := runCLI(t, cfg, true, "n\n", "close", "ABC-1", "--as", "Duplicate"); err == nil || len(f.writes) != 0 || !strings.Contains(text, `transition "Rozwiąż zgłoszenie" (5) to Rozwiązane with resolution Duplikat`) {
		t.Fatalf("declined: %q %v writes=%v", text, err, f.writes)
	}
	if text, err := runCLI(t, cfg, false, "", "dup", "ABC-1", "--dry-run", "ABC-9"); err != nil || len(f.writes) != 0 || !strings.Contains(text, "link ABC-1 duplicates ABC-9") {
		t.Fatalf("dry run: %q %v", text, err)
	}
	if _, err := runCLI(t, cfg, false, "", "dup", "ABC-1", "ABC-9", "--fix", "REJECTED", "--yes"); err != nil || len(f.writes) != 2 {
		t.Fatalf("dup --yes: %v writes=%v", err, f.writes)
	}
	if _, err := runCLI(t, cfg, false, "", "link", "ABC-1", "relates", "to", "ABC-7", "--yes"); err != nil || !strings.Contains(f.writes[2], `"name":"Relates"`) {
		t.Fatalf("multi-word link type: %v writes=%v", err, f.writes)
	}
	cfg.Policy.AllowWrite = false
	if _, err := runCLI(t, cfg, false, "", "comment", "ABC-1", "hello", "--yes"); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("policy: %v", err)
	}
}

func TestResolveProfileWithBothKinds(t *testing.T) {
	cfg := config.Default()
	cfg.Profiles["server"] = config.Profile{Kind: "jira", URL: "https://jira.example.test", Projects: []string{"ARGOS"}}
	cfg.Profiles["cloud"] = config.Profile{Kind: "atlassian", URL: "https://x.atlassian.net", Projects: []string{"WEB"}}
	for key, want := range map[string]string{"ARGOS-1": "server", "WEB-2": "cloud"} {
		if name, _, err := ResolveProfile(cfg, "", key); err != nil || name != want {
			t.Errorf("%s: %s %v", key, name, err)
		}
	}
	if _, _, err := ResolveProfile(cfg, "", "OTHER-1"); err == nil {
		t.Error("an unmatched project with both kinds must ask for a profile")
	}
	if _, _, err := ResolveProfile(cfg, "nope", "ARGOS-1"); err == nil {
		t.Error("an unknown profile must fail")
	}
}
