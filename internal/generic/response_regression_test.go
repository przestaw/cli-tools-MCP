package generic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFullURLAndBinaryMetadata(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(map[bool]string{false: "http", true: "https"}[tls], func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.RequestURI() != "/base/file?x=1;y=2" {
					t.Errorf("unexpected target %s", r.URL.RequestURI())
				}
				w.Header().Set("Content-Type", "application/octet-stream")
				_, _ = w.Write([]byte("UTF-8 binary\x00"))
			})
			srv := httptest.NewUnstartedServer(handler)
			if tls {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			defer srv.Close()
			cfg := testConfig(srv.URL + "/base")
			p := cfg.Profiles["svc"]
			p.VerifyTLS = false
			cfg.Profiles["svc"] = p
			for _, metadata := range []bool{false, true} {
				_, err := CallTool(context.Background(), cfg, "request", map[string]any{
					"path": srv.URL + "/base/file?x=1;y=2", "response_metadata": metadata,
				})
				if err == nil {
					t.Fatalf("binary accepted with metadata=%v", metadata)
				}
			}
			t.Setenv("XDG_CACHE_HOME", t.TempDir())
			out, err := CallTool(context.Background(), cfg, "download", map[string]any{"path": srv.URL + "/base/file?x=1;y=2"})
			if err != nil {
				t.Fatal(err)
			}
			var got downloaded
			if err := json.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			defer os.Remove(got.Path)
			b, err := os.ReadFile(got.Path)
			if err != nil || string(b) != "UTF-8 binary\x00" {
				t.Fatalf("download changed bytes: %q, %v", b, err)
			}
		})
	}
}
