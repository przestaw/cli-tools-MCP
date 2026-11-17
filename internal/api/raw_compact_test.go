package api

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

func TestRawCommandOutput(t *testing.T) {
	var calls int
	var ctype, body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", ctype)
		fmt.Fprint(w, body)
	}))
	defer srv.Close()
	cfg := config.Default()
	c, err := NewProfileClient(cfg, "p", config.Profile{Kind: "api", URL: srv.URL, Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, ctype, body string
		args              []string
		want, wantErr     string
		wantNoRequest     bool
	}{
		{name: "default", ctype: "application/json", body: `{ "a": 1 }`, args: []string{"GET", "/"}, want: `{ "a": 1 }`},
		{name: "compact json keeps numbers", ctype: "application/json", body: `{ "id": 9007199254740993, "v": 1.2300 }`, args: []string{"--output", "compact", "GET", "/"}, want: `{"id":9007199254740993,"v":1.2300}`},
		{name: "compact html", ctype: "text/html", body: `<p>Hi</p><script>x()</script>`, args: []string{"--output", "compact", "GET", "/"}, want: "Hi"},
		{name: "original html", ctype: "text/html", body: `<p>Hi</p>`, args: []string{"--output", "original", "GET", "/"}, want: `<p>Hi</p>`},
		{name: "binary stays byte-identical", ctype: "application/octet-stream", body: "\x00{ }\x01", args: []string{"--output", "compact", "GET", "/"}, want: "\x00{ }\x01"},
		{name: "invalid output sends nothing", args: []string{"--output", "toon", "GET", "/"}, wantErr: "output must be", wantNoRequest: true},
		{name: "invalid output beats write policy", args: []string{"--output", "toon", "POST", "/"}, wantErr: "output must be", wantNoRequest: true},
		{name: "invalid json", ctype: "application/json", body: `{`, args: []string{"--output", "compact", "GET", "/"}, wantErr: "do not replay a write"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, ctype, body = 0, tc.ctype, tc.body
			f, err := os.CreateTemp(t.TempDir(), "stdout")
			if err != nil {
				t.Fatal(err)
			}
			old := os.Stdout
			os.Stdout = f
			defer func() { os.Stdout = old; _ = f.Close() }()
			err = RawCommand(context.Background(), c, cfg, tc.args)
			out, _ := os.ReadFile(f.Name())
			switch {
			case tc.wantErr != "":
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || len(out) != 0 {
					t.Fatalf("error = %v, output %q, want %q", err, out, tc.wantErr)
				}
			case err != nil || string(out) != tc.want:
				t.Fatalf("got %q, %v; want %q", out, err, tc.want)
			}
			if tc.wantNoRequest && calls != 0 {
				t.Fatalf("a request was sent (%d calls)", calls)
			}
		})
	}
}
