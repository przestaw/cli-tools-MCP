package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompactOutputWithMetadataAndValidation(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Location", "/queue/1")
		fmt.Fprint(w, "<div>Queued</div>")
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	for _, args := range []map[string]any{
		{"path": "/", "output": "toon"},
		{"path": "/", "output": "original", "compact": true},
	} {
		if _, err := CallTool(context.Background(), cfg, "request", args); err == nil {
			t.Fatal("invalid option accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid output option sent request")
	}
	out, err := CallTool(context.Background(), cfg, "request", map[string]any{"path": "/", "output": "compact", "response_metadata": true})
	if err != nil {
		t.Fatal(err)
	}
	var got struct{ Body, Location string }
	if err := json.Unmarshal(out, &got); err != nil || got.Body != "Queued" || got.Location != "/queue/1" {
		t.Fatalf("metadata/compaction mismatch: %s %v", out, err)
	}
}
