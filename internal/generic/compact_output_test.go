package generic

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRequestPresentation(t *testing.T) {
	var calls int
	var ctype, body string
	var status int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", ctype)
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	const page = `<html><head><script>x()</script></head><body><p>Hi</p><input type="hidden" value="SECRET"></body></html>`
	for _, tc := range []struct {
		name, ctype, body string
		status            int
		args              map[string]any
		want, wantErr     string
		wantNoRequest     bool
	}{
		{name: "default keeps json", ctype: "application/json", body: `{ "v": 1.2300 }`, args: map[string]any{}, want: `{ "v": 1.2300 }`},
		{name: "output compact json", ctype: "application/json", body: `{ "id": 9007199254740993, "v": 1.2300 }`, args: map[string]any{"output": "compact"}, want: `{"id":9007199254740993,"v":1.2300}`},
		{name: "output original html", ctype: "text/html", body: page, args: map[string]any{"output": "original"}, want: page},
		{name: "output compact html", ctype: "text/html", body: page, args: map[string]any{"output": "compact"}, want: "Hi"},
		{name: "legacy compact leaves json", ctype: "application/json", body: `{ "a": 1 }`, args: map[string]any{"compact": true}, want: `{ "a": 1 }`},
		{name: "legacy compact html", ctype: "text/html", body: page, args: map[string]any{"compact": true}, want: "Hi"},
		{name: "compact flag with output compact", ctype: "application/json", body: `{ "a": 1 }`, args: map[string]any{"compact": true, "output": "compact"}, want: `{"a":1}`},
		{name: "compact flag with output original", args: map[string]any{"compact": true, "output": "original"}, wantErr: "conflict", wantNoRequest: true},
		{name: "unknown output", args: map[string]any{"output": "toon"}, wantErr: "output must be", wantNoRequest: true},
		{name: "binary is refused even when compacting", ctype: "application/octet-stream", body: "\x00\x01\x02", args: map[string]any{"output": "compact"}, wantErr: "binary"},
		{name: "invalid json is an error", ctype: "application/json", body: `{`, args: map[string]any{"output": "compact"}, wantErr: "do not replay a write"},
		{name: "html error body is shown as text", ctype: "text/html", body: page, status: 500, args: map[string]any{}, wantErr: "HTTP 500: Hi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, ctype, body, status = 0, tc.ctype, tc.body, cmp.Or(tc.status, 200)
			args := map[string]any{"path": "/p"}
			maps.Copy(args, tc.args)
			out, err := CallTool(context.Background(), cfg, "request", args)
			switch {
			case tc.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || strings.ContainsAny(err.Error(), "<>") || strings.Contains(err.Error(), "SECRET") {
					t.Fatalf("error = %v, want %q", err, tc.wantErr)
				}
			case err != nil || string(out) != tc.want:
				t.Fatalf("got %q, %v; want %q", out, err, tc.want)
			}
			if tc.wantNoRequest && calls != 0 {
				t.Fatalf("a request was sent (%d calls)", calls)
			}
			if tc.wantNoRequest {
				if err := ValidateRequest(cfg, args); err == nil {
					t.Fatal("ValidateRequest accepted the options")
				}
			}
		})
	}
}
