package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJSON(t *testing.T) {
	for _, body := range []any{json.RawMessage(`{"name":"one"}`), []byte(`{"name":"one"}`), map[string]string{"name": "one"}} {
		t.Run("body", func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, _ := io.ReadAll(r.Body)
				if string(data) != `{"name":"one"}` || r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Content-Type") != "application/json" {
					t.Errorf("unexpected request: %s %v", data, r.Header)
				}
				w.Header().Set("x-ms-continuationtoken", "next")
				w.Write([]byte(`{"name":"two"}`))
			}))
			defer srv.Close()
			headers := http.Header{"Authorization": {"Bearer token"}}
			var out struct{ Name string }
			hdr, err := JSON(context.Background(), srv.Client(), "POST", srv.URL, headers, body, &out, nil, nil)
			if err != nil || out.Name != "two" || hdr.Get("x-ms-continuationtoken") != "next" {
				t.Fatalf("out=%v headers=%v err=%v", out, hdr, err)
			}
			if headers.Get("Content-Type") != "" {
				t.Fatal("caller headers mutated")
			}
		})
	}
}

func TestJSONResponses(t *testing.T) {
	wantErr := errors.New("provider error")
	for _, tc := range []struct {
		name     string
		status   int
		body     string
		validate func([]byte) error
		want     string
	}{
		{"empty", 204, "", nil, ""},
		{"invalid", 200, "not json", nil, "decode "},
		{"http error", 403, "denied", nil, "provider error"},
		{"auth html", 200, "<html>", func([]byte) error { return wantErr }, "provider error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			var out any
			_, err := JSON(context.Background(), srv.Client(), "GET", srv.URL, nil, nil, &out, func(status int, url string, data []byte) error {
				if status != tc.status || string(data) != tc.body || url != srv.URL {
					t.Error("error parser arguments changed")
				}
				return wantErr
			}, tc.validate)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("err=%v want=%q", err, tc.want)
			}
		})
	}
}

type brokenReader struct{}

func (brokenReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (brokenReader) Close() error             { return nil }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return fn(req) }

func TestDoReadError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: brokenReader{}, Header: http.Header{}}, nil
	})}
	req, _ := http.NewRequest("GET", "http://example.test", nil)
	_, _, err := Do(client, req, nil)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("read error lost: %v", err)
	}
}

func TestClientTLS(t *testing.T) {
	for _, verify := range []bool{true, false} {
		client := NewClient(0, verify)
		cfg := client.Transport.(*http.Transport).TLSClientConfig
		if client.Timeout != 30*time.Second || cfg.InsecureSkipVerify == verify {
			t.Fatalf("timeout/TLS changed: %v %+v", client.Timeout, cfg)
		}
	}
	if _, err := Transport(true, "missing-ca.pem"); err == nil {
		t.Fatal("missing CA accepted")
	}
}

func TestJSONMarshalErrorDoesNotSendRequest(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("invalid body sent")
		return nil, nil
	})}
	_, err := JSON(context.Background(), client, "POST", "http://example.test", nil, make(chan int), nil, nil, nil)
	if err == nil {
		t.Fatal("unsupported JSON body accepted")
	}
}

func TestErrorFormats(t *testing.T) {
	if got := ErrorMessage("provider", 404, "", "/path"); got != "provider 404 for /path" {
		t.Fatal(got)
	}
	if got := ErrorMessage("provider", 403, "denied", "/path"); got != "provider 403: denied" {
		t.Fatal(got)
	}
	if got := RawError("GET", "/path", 400, "  denied\n"); got != "GET /path: HTTP 400: denied" {
		t.Fatal(got)
	}
	if got := RawError("GET", "/path", 400, strings.Repeat("x", 1201)); !strings.HasSuffix(got, strings.Repeat("x", 1200)+"...") {
		t.Fatal("raw error truncation changed")
	}
}
