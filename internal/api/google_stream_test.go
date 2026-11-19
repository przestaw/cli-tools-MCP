package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"workctl/internal/config"
)

func TestGoogleAllowedURLWorksForStreamAndUpload(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RequestURI() != "/v4/items?x=1" {
			t.Errorf("wrong URL: %s", r.URL)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, "content")
	}))
	defer srv.Close()
	c, err := NewProfileClient(config.Default(), "p", config.Profile{Kind: "api", URL: "https://www.googleapis.com/drive/v3", Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(srv.URL)
	c.hosts = []string{u.Hostname()}
	c.client = srv.Client()
	var out bytes.Buffer
	if _, err := c.Stream(context.Background(), srv.URL+"/v4/items", url.Values{"x": {"1"}}, &out); err != nil || out.String() != "content" {
		t.Fatalf("stream: %q %v", out.String(), err)
	}
	if _, _, err := c.StreamReader(context.Background(), "PUT", srv.URL+"/v4/items?x=1", nil, bytes.NewBufferString("x"), 1, "text/plain", nil); err != nil {
		t.Fatal(err)
	}
}
