package pr

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"workctl/internal/config"
)

// The profile picked by --provider must be the one used by show, show --include and show --format raw.
func TestProviderSelectionSurvivesIncludeAndRaw(t *testing.T) {
	t.Setenv("TEST_PROVIDER_TOKEN", "token")
	var githubHits, gitlabHits atomic.Int32
	github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		githubHits.Add(1)
		_, _ = w.Write([]byte(`{"number":1,"title":"gh","state":"open","head":{"sha":"abc"},"check_runs":[]}`))
	}))
	defer github.Close()
	gitlab := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gitlabHits.Add(1)
		_, _ = w.Write([]byte(`{"iid":1,"title":"gl","state":"opened"}`))
	}))
	defer gitlab.Close()

	cfg := config.Default()
	cfg.Profiles["gh"] = config.Profile{Kind: "github", URL: github.URL, TokenEnv: "TEST_PROVIDER_TOKEN", DefaultRepo: "o/r"}
	cfg.Profiles["gl"] = config.Profile{Kind: "gitlab", URL: gitlab.URL, TokenEnv: "TEST_PROVIDER_TOKEN", DefaultProject: "g/p"}

	for _, args := range [][]string{
		{"show", "--provider", "github", "1"},
		{"show", "--provider", "github", "--include", "checks", "1"},
		{"show", "--provider", "github", "--format", "raw", "1"},
	} {
		f, err := os.CreateTemp(t.TempDir(), "stdout")
		if err != nil {
			t.Fatal(err)
		}
		old := os.Stdout
		os.Stdout = f
		before := githubHits.Load()
		err = Command(cfg, args)
		os.Stdout = old
		_ = f.Close()
		if err != nil {
			t.Fatalf("%s: %v", strings.Join(args, " "), err)
		}
		if githubHits.Load() == before {
			t.Errorf("%s: the GitHub profile was not used", strings.Join(args, " "))
		}
	}
	if gitlabHits.Load() != 0 {
		t.Fatalf("--provider github reached the GitLab server %d times", gitlabHits.Load())
	}
}
