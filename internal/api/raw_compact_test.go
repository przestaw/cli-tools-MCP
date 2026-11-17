package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"workctl/internal/config"
)

func TestRawCompactCLI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{ "id": 9007199254740993 }`)
	}))
	defer srv.Close()
	cfg := config.Default()
	c, err := NewProfileClient(cfg, "p", config.Profile{Kind: "api", URL: srv.URL, Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	defer func() { os.Stdout = old; _ = f.Close() }()
	if err := RawCommand(context.Background(), c, cfg, []string{"--output", "compact", "GET", "/"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil || string(b) != `{"id":9007199254740993}` {
		t.Fatalf("CLI output: %q %v", b, err)
	}
}
