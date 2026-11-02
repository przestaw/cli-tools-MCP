package generic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/config"
)

func testConfig(url string) *config.Config {
	cfg := config.Default()
	cfg.Profiles = map[string]config.Profile{"svc": {Kind: Kind, URL: url, Auth: "none", VerifyTLS: true}}
	return cfg
}

func TestAPIRequestReadsAndGuardsWrites(t *testing.T) {
	var method, path, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		method, path, body = r.Method, r.URL.Path, string(b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL + "/base")
	ctx := context.Background()

	out, err := CallTool(ctx, cfg, "api_request", map[string]any{"path": "/job/x/api/json"})
	if err != nil || string(out) != `{"ok":true}` || method != "GET" || path != "/base/job/x/api/json" {
		t.Fatalf("GET: %s %v %s %s", out, err, method, path)
	}
	if _, err := CallTool(ctx, cfg, "api_request", map[string]any{"method": "post", "path": "/build"}); err == nil || !strings.Contains(err.Error(), "policy") {
		t.Fatalf("POST without allow_write: %v", err)
	}
	if _, err := CallTool(ctx, cfg, "api_request", map[string]any{"path": "https://elsewhere.test/"}); err == nil {
		t.Fatal("absolute URL accepted")
	}
	cfg.Policy.AllowWrite = true
	if _, err := CallTool(ctx, cfg, "api_request", map[string]any{"method": "POST", "path": "/build", "body": `{"a":1}`}); err != nil || method != "POST" || body != `{"a":1}` {
		t.Fatalf("POST: %v %s %q", err, method, body)
	}
}
