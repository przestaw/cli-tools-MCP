package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"workctl/internal/config"
)

func TestRawCommandKeepsUTF8BinaryBytes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(`{"not":"text"}`))
	}))
	defer srv.Close()
	c, err := NewProfileClient(config.Default(), "svc", config.Profile{Kind: "api", URL: srv.URL, Auth: "none"})
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
	if err := RawCommand(context.Background(), c, config.Default(), []string{"GET", "/file"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(f.Name())
	if err != nil || string(b) != `{"not":"text"}` {
		t.Fatalf("raw modified binary JSON-like bytes: %q %v", b, err)
	}
}
