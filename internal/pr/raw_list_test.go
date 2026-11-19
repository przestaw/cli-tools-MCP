package pr

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"workctl/internal/config"
)

func TestCLIListRawRetainsProviderDetails(t *testing.T) {
	t.Setenv("TEST_RAW_LIST_TOKEN", "test")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"iid":1,"title":"Example","description":"Keep description","extra_provider_field":"sentinel"}]`)
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.Profiles["gl"] = config.Profile{Kind: "gitlab", URL: srv.URL, TokenEnv: "TEST_RAW_LIST_TOKEN", DefaultProject: "group/project"}
	for _, format := range []string{"raw", "json"} {
		t.Run(format, func(t *testing.T) {
			f, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stdout
			os.Stdout = f
			defer func() { os.Stdout = old; _ = f.Close() }()
			if err := Command(cfg, []string{"list", "--profile", "gl", "--format", format}); err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(f.Name())
			if err != nil || strings.Contains(string(b), "sentinel") != (format == "raw") || !strings.Contains(string(b), "Keep description") {
				t.Fatalf("wrong CLI %s output: %s %v", format, b, err)
			}
		})
	}
}
