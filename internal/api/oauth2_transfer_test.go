package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestOAuthTransfersInvalidateWithoutReplay(t *testing.T) {
	for _, operation := range []string{"download", "upload", "post"} {
		t.Run(operation, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.Copy(io.Discard, r.Body)
				http.Error(w, "expired", http.StatusUnauthorized)
			}))
			defer srv.Close()
			cfg, p := oauth2Profile(t, srv.URL, srv.URL)
			c, err := NewProfileClient(cfg, "p", p)
			if err != nil {
				t.Fatal(err)
			}
			c.oauth2.accessToken, c.oauth2.expiresAt = "cached", time.Now().Add(time.Hour)
			c.oauth2.saveCached()
			switch operation {
			case "download":
				_, err = c.Stream(context.Background(), "/file", nil, io.Discard)
			case "upload":
				_, _, err = c.StreamReader(context.Background(), "PUT", "/file", nil, strings.NewReader("x"), 1, "text/plain", nil)
			case "post":
				_, _, err = c.Do(context.Background(), "POST", "/file", nil, []byte("x"), "text/plain")
			}
			var httpErr *Error
			if !errors.As(err, &httpErr) || httpErr.Status != http.StatusUnauthorized || calls != 1 || c.oauth2.accessToken != "" {
				t.Fatalf("status=%v calls=%d token retained=%v", err, calls, c.oauth2.accessToken != "")
			}
			if _, err := os.Stat(c.oauth2.cacheFile); !os.IsNotExist(err) {
				t.Fatalf("rejected token cache still present: %v", err)
			}
		})
	}
}

func TestTokenExpiryDoesNotExtendShortLifetime(t *testing.T) {
	now := time.Now()
	for _, seconds := range []int{0, 10, 3600} {
		got := (tokenResponse{ExpiresIn: seconds}).expiry(now)
		if !got.Equal(now.Add(time.Duration(seconds) * time.Second)) {
			t.Fatalf("expiry changed for %d seconds", seconds)
		}
	}
}
