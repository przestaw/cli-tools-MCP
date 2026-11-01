package pr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/config"
	"workctl/internal/vcs"
)

func TestStateMappingForEveryProvider(t *testing.T) {
	t.Setenv("TEST_PR_TOKEN", "token")
	for _, provider := range []string{"github", "gitlab", "azure"} {
		for _, state := range []string{"open", "closed", "merged", "all"} {
			t.Run(provider+"/"+state, func(t *testing.T) {
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					query := r.URL.Query()
					switch provider {
					case "github":
						want := state
						if state == "merged" {
							want = "closed"
						}
						if query.Get("state") != want || query.Get("per_page") != "100" {
							t.Error(query)
						}
						_, _ = w.Write([]byte(`[{"number":1,"merged_at":"date"},{"number":2,"merged_at":null}]`))
					case "gitlab":
						want := state
						if state == "open" {
							want = "opened"
						}
						if query.Get("state") != want || query.Get("scope") != "all" || query.Get("per_page") != "100" {
							t.Error(query)
						}
						_, _ = w.Write([]byte(`[{"iid":3,"description":"body","author":{"username":"author"}}]`))
					case "azure":
						want := map[string]string{"open": "active", "closed": "abandoned", "merged": "completed", "all": "all"}[state]
						if query.Get("searchCriteria.status") != want || !strings.Contains(r.URL.Path, "/Project/_apis/") {
							t.Errorf("%s %v", r.URL.Path, query)
						}
						_, _ = w.Write([]byte(`{"value":[{"pullRequestId":4,"title":"Title","description":"body","sourceRefName":"refs/heads/feat","targetRefName":"refs/heads/main","repository":{"name":"repo"}}]}`))
					}
				}))
				defer srv.Close()
				profile := config.Profile{Kind: provider, URL: srv.URL, TokenEnv: "TEST_PR_TOKEN", VerifyTLS: true, DefaultRepo: "o/r", DefaultProject: "Project"}
				cfg := config.Default()
				cfg.Profiles[provider] = profile
				result, err := Fetch(context.Background(), cfg, provider, profile, "", "", "", state, 0)
				if err != nil || calls != 1 {
					t.Fatalf("result=%v calls=%d err=%v", result, calls, err)
				}
				if provider == "github" && state == "closed" && (len(result.Items) != 1 || result.Items[0].ID != 2) {
					t.Fatal(result)
				}
				if provider == "github" && state == "merged" && (len(result.Items) != 1 || result.Items[0].ID != 1) {
					t.Fatal(result)
				}
				if provider == "azure" && (result.Items[0].SourceBranch != "feat" || result.Items[0].TargetBranch != "main" || result.Items[0].Description != "body") {
					t.Fatal(result)
				}
			})
		}
	}
}

func TestGitHubSearchEnvelopeAndStateFilter(t *testing.T) {
	t.Setenv("TEST_PR_TOKEN", "token")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/issues" || !strings.Contains(r.URL.Query().Get("q"), "repo:o/r is:pr fix") {
			t.Error(r.URL)
		}
		_, _ = w.Write([]byte(`{"items":[{"number":1,"pull_request":{"merged_at":"date"}},{"number":2,"pull_request":{"merged_at":null}}]}`))
	}))
	defer srv.Close()
	cfg := config.Default()
	profile := config.Profile{Kind: "github", URL: srv.URL, TokenEnv: "TEST_PR_TOKEN", VerifyTLS: true, DefaultRepo: "o/r"}
	for state, id := range map[string]int{"merged": 1, "closed": 2} {
		result, err := Fetch(context.Background(), cfg, "gh", profile, "", "", "fix", state, 0)
		if err != nil || len(result.Items) != 1 || result.Items[0].ID != id {
			t.Fatalf("%s: %v %v", state, result, err)
		}
	}
}

func TestAzurePRRequiresProjectAndFiltersSearch(t *testing.T) {
	t.Setenv("TEST_PR_TOKEN", "token")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"value":[{"pullRequestId":1,"title":"One","description":"MATCH"},{"pullRequestId":2,"title":"Two"}]}`))
	}))
	defer srv.Close()
	cfg := config.Default()
	profile := config.Profile{Kind: "azure", URL: srv.URL, TokenEnv: "TEST_PR_TOKEN", VerifyTLS: true}
	if _, err := Fetch(context.Background(), cfg, "az", profile, "", "", "", "all", 0); err == nil || calls != 0 {
		t.Fatalf("missing project: %v calls=%d", err, calls)
	}
	result, err := Fetch(context.Background(), cfg, "az", profile, "", "Explicit", "match", "all", 0)
	if err != nil || len(result.Items) != 1 || result.Items[0].ID != 1 {
		t.Fatalf("search: %v %v", result, err)
	}
}

func TestPullRequestRequestsPerProvider(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case strings.Contains(r.URL.Path, "/_apis/git/pullrequests/"):
			_, _ = w.Write([]byte(`{"pullRequestId":5,"title":"one"}`))
		case strings.HasSuffix(r.URL.Path, "/_apis/git/pullrequests"):
			_, _ = w.Write([]byte(`{"value":[{"pullRequestId":5,"title":"one"}]}`))
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_, _ = w.Write([]byte(`[{"number":1,"merged_at":"2026-01-01T00:00:00Z"},{"number":2,"merged_at":null}]`))
		default:
			_, _ = w.Write([]byte(`[]`))
		}
	}))
	defer srv.Close()
	t.Setenv("TEST_TOKEN", "x")
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{
		"az": {Kind: "azure", URL: srv.URL + "/org", TokenEnv: "TEST_TOKEN", VerifyTLS: true, DefaultProject: "Def"},
		"gh": {Kind: "github", URL: srv.URL, TokenEnv: "TEST_TOKEN", VerifyTLS: true, DefaultRepo: "o/r"},
		"gl": {Kind: "gitlab", URL: srv.URL, TokenEnv: "TEST_TOKEN", VerifyTLS: true, DefaultProject: "g/p"},
	}
	fetch := func(name, project, state string, id int) []vcs.PullRequest {
		t.Helper()
		res, err := Fetch(context.Background(), cfg, name, cfg.Profiles[name], "", project, "", state, id)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return res.Items
	}
	fetch("az", "", "merged", 0)
	fetch("az", "Other", "", 5)
	if items := fetch("gh", "", "closed", 0); len(items) != 1 || items[0].ID != 2 {
		t.Fatalf("closed must exclude merged: %#v", items)
	}
	fetch("gl", "", "open", 0)
	want := []string{
		"/org/Def/_apis/git/pullrequests?", "searchCriteria.status=completed",
		"/org/Other/_apis/git/pullrequests/5?",
		"/repos/o/r/pulls?", "state=closed",
		"/api/v4/projects/g/p/merge_requests?", "state=opened",
	}
	all := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("missing %q in requests:\n%s", w, all)
		}
	}
	if _, err := Fetch(context.Background(), cfg, "gl", cfg.Profiles["gl"], "", "", "", "opened", 0); err == nil {
		t.Fatal("provider-specific state must be rejected")
	}
}
