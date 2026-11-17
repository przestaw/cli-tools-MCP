package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOAuthCacheFailureIsNotHiddenByRetry(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "revoked", http.StatusUnauthorized)
	}))
	defer srv.Close()
	cfg, p := oauth2Profile(t, srv.URL, srv.URL)
	c, err := NewProfileClient(cfg, "p", p)
	if err != nil {
		t.Fatal(err)
	}
	c.oauth2.accessToken, c.oauth2.expiresAt = "cached", time.Now().Add(time.Hour)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "block-remove"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	c.oauth2.cacheFile = dir
	_, _, err = c.Do(context.Background(), "GET", "/file", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "remove rejected OAuth token cache") || calls != 1 {
		t.Fatalf("cache failure hidden or request replayed: %v calls=%d", err, calls)
	}
}
