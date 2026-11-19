package pr

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"workctl/internal/config"
)

const detailsItem = `{"iid":1,"title":"Example","description":"Keep description","merge_status":"can_be_merged","extra_provider_field":"sentinel","author":{"avatar_url":"AVATAR"}}`

func gitlabConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("TEST_RAW_TOOL_TOKEN", "test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/merge_requests/1") {
			fmt.Fprint(w, detailsItem)
			return
		}
		fmt.Fprintf(w, "[%s]", detailsItem)
	}))
	t.Cleanup(srv.Close)
	cfg := config.Default()
	cfg.Profiles["gl"] = config.Profile{Kind: "gitlab", URL: srv.URL, TokenEnv: "TEST_RAW_TOOL_TOKEN", DefaultProject: "group/project"}
	return cfg
}

func TestToolDetailsCompactOrRaw(t *testing.T) {
	cfg := gitlabConfig(t)
	for _, tool := range []string{"pr_list", "pr_show"} {
		for _, tc := range []struct {
			format        string
			raw, wantFail bool
		}{
			{"", false, false}, {"json", false, false}, {"raw", true, false}, {"RAW", true, false}, {"compact", false, false}, {"bogus", false, true},
		} {
			t.Run(tool+"/"+tc.format, func(t *testing.T) {
				out, err := CallTool(context.Background(), cfg, tool, map[string]any{"profile": "gl", "number": 1, "format": tc.format})
				if tc.wantFail {
					if err == nil {
						t.Fatalf("accepted: %s", out)
					}
					return
				}
				got := string(out)
				if err != nil || strings.Contains(got, "sentinel") != tc.raw || strings.Contains(got, "AVATAR") != tc.raw ||
					!strings.Contains(got, "can_be_merged") || !strings.Contains(got, "Keep description") {
					t.Fatalf("raw=%v: %s %v", tc.raw, got, err)
				}
			})
		}
	}
}

func TestCLIShowRawRetainsProviderDetails(t *testing.T) {
	cfg := gitlabConfig(t)
	for _, tc := range []struct {
		args []string
		raw  bool
	}{
		{[]string{"show", "--profile", "gl", "--format", "raw", "1"}, true},
		{[]string{"show", "--profile", "gl", "--format", "json", "1"}, false},
		{[]string{"show", "--profile", "gl", "--json", "1"}, false},
		{[]string{"show", "--profile", "gl", "--format", "raw", "--include", "checks", "1"}, true},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stdout
			os.Stdout = f
			defer func() { os.Stdout = old; _ = f.Close() }()
			if err := Command(cfg, tc.args); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(f.Name())
			if strings.Contains(string(b), "sentinel") != tc.raw {
				t.Fatalf("raw=%v: %s", tc.raw, b)
			}
		})
	}
}
