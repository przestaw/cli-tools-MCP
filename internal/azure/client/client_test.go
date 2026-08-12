package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	c := New(Options{OrgURL: srv.URL, Token: "pat", Timeout: 5 * time.Second, VerifyTLS: true})
	return c, srv
}

func TestProjectsAndAuth(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// PAT auth: empty username, token as password.
		u, p, ok := r.BasicAuth()
		if !ok || u != "" || p != "pat" {
			t.Errorf("bad basic auth: user=%q pass=%q ok=%v", u, p, ok)
		}
		if r.URL.Query().Get("api-version") == "" {
			t.Error("api-version not set on request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"count":1,"value":[{"id":"1","name":"Web","state":"wellFormed"}]}`))
	})
	defer srv.Close()

	ps, err := c.Projects(context.Background(), false)
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(ps) != 1 || ps[0].Name != "Web" {
		t.Errorf("unexpected projects: %+v", ps)
	}
}

func TestContinuationPaging(t *testing.T) {
	calls := 0
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("continuationToken") == "" {
			calls++
			w.Header().Set("x-ms-continuationtoken", "next")
			w.Write([]byte(`{"count":1,"value":[{"id":"1","name":"A"}]}`))
			return
		}
		calls++
		w.Write([]byte(`{"count":1,"value":[{"id":"2","name":"B"}]}`))
	})
	defer srv.Close()

	ps, err := c.Projects(context.Background(), true) // all=true follows the token
	if err != nil {
		t.Fatalf("Projects: %v", err)
	}
	if len(ps) != 2 || calls != 2 {
		t.Errorf("expected 2 projects over 2 calls, got %d projects / %d calls", len(ps), calls)
	}
}

func TestErrorMessageParsing(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"The repository does not exist","typeKey":"GitRepositoryNotFoundException"}`))
	})
	defer srv.Close()

	_, err := c.Repository(context.Background(), "proj", "missing")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !IsNotFound(err) {
		t.Errorf("expected IsNotFound, got %v", err)
	}
	if got := err.Error(); got != "azure-devops 404: The repository does not exist" {
		t.Errorf("error message = %q", got)
	}
}

func TestHTMLSignInDetected(t *testing.T) {
	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!DOCTYPE html><html><body>Sign In</body></html>`))
	})
	defer srv.Close()

	_, err := c.Projects(context.Background(), false)
	if err == nil {
		t.Fatal("expected an auth error for an HTML body")
	}
}

func TestVsrmBase(t *testing.T) {
	cases := map[string]string{
		"https://dev.azure.com/acme":    "https://vsrm.dev.azure.com/acme",
		"https://acme.visualstudio.com": "https://acme.vsrm.visualstudio.com",
		"https://on-prem.example/tfs":   "https://on-prem.example/tfs",
	}
	for in, want := range cases {
		if got := vsrmBase(in); got != want {
			t.Errorf("vsrmBase(%q) = %q, want %q", in, got, want)
		}
	}
}
