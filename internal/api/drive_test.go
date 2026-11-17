package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"workctl/internal/config"
)

func TestDriveRejectsMalformedResponsesAndPreservesErrors(t *testing.T) {
	for _, tc := range []struct {
		body string
		code int
	}{{"{", 200}, {"{}", 200}, {`{"error":"denied"}`, 403}} {
		t.Run(fmt.Sprint(tc), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			c, err := NewProfileClient(config.Default(), "p", config.Profile{Kind: "api", URL: srv.URL + "/drive/v3", Auth: "none"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.DriveAbout(context.Background())
			if err == nil || (tc.code == 403 && !IsStatus(err, 403)) {
				t.Fatalf("about: %v", err)
			}
			if _, err := c.DriveList(context.Background(), "root", "", "", 30); err == nil {
				t.Fatal("listing silently accepted malformed/error response")
			}
			if _, err := c.DriveMetadata(context.Background(), "id"); err == nil {
				t.Fatal("metadata silently accepted malformed/error response")
			}
		})
	}
}

func TestDriveListingValidationAndEscaping(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		want := `'root' in parents and trashed = false and name contains 'a\\b\'c'`
		if r.URL.Query().Get("q") != want {
			t.Errorf("query = %q", r.URL.Query().Get("q"))
		}
		fmt.Fprint(w, `{"files":[],"incompleteSearch":true}`)
	}))
	defer srv.Close()
	c, err := NewProfileClient(config.Default(), "p", config.Profile{Kind: "api", URL: srv.URL + "/drive/v3", Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	for _, limit := range []int{0, -1, 1001} {
		if _, err := c.DriveList(context.Background(), "", "", "", limit); err == nil {
			t.Fatal("invalid limit accepted")
		}
	}
	if calls != 0 {
		t.Fatal("invalid limit made network request")
	}
	out, err := c.DriveList(context.Background(), "", `a\b'c`, "", 30)
	if err != nil || !out.Incomplete {
		t.Fatalf("listing = %+v, %v", out, err)
	}
}
