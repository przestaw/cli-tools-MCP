package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

func TestDriveSizeIsReportedAsANumber(t *testing.T) {
	var f DriveFile
	if err := json.Unmarshal([]byte(`{"id":"a","name":"n","mimeType":"m","size":"6679"}`), &f); err != nil {
		t.Fatal(err)
	}
	out, _ := json.Marshal(f)
	if !strings.Contains(string(out), `"size":6679`) {
		t.Fatalf("size not numeric: %s", out)
	}
	if err := json.Unmarshal([]byte(`{"size":"x"}`), &f); err == nil {
		t.Fatal("invalid size accepted")
	}
}

func driveTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := NewProfileClient(config.Default(), "p", config.Profile{Kind: "api", URL: srv.URL + "/drive/v3", Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDriveFileID(t *testing.T) {
	c := driveTestClient(t, nil)
	base := c.baseURL
	for path, want := range map[string]string{
		"abc_-1":                               "abc_-1",
		"files/abc":                            "abc",
		"/files/abc":                           "abc",
		base + "/files/abc":                    "abc",
		base + "/files/abc?alt=media":          "abc",
		"files/abc?fields=*":                   "abc",
		"files/abc/export":                     "",
		"files/a b":                            "",
		"files/":                               "",
		"https://example.com/files/abc":        "",
		"http://other.test/drive/v3/files/abc": "",
		"../abc":                               "",
		"":                                     "",
	} {
		id, ok := c.DriveFileID(path)
		if id != want || ok != (want != "") {
			t.Errorf("DriveFileID(%q) = %q, %v; want %q", path, id, ok, want)
		}
	}
	plain, err := NewProfileClient(config.Default(), "p", config.Profile{Kind: "api", URL: "https://example.com/api", Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := plain.DriveFileID("abc"); ok {
		t.Fatal("non-Drive profile recognized a Drive ID")
	}
}

func TestDriveListPageTokenAndLimitBoundaries(t *testing.T) {
	var got url.Values
	c := driveTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		fmt.Fprint(w, `{"files":[]}`)
	})
	ctx := context.Background()
	for _, limit := range []int{1, 1000} {
		if _, err := c.DriveList(ctx, "root", "", "", limit); err != nil || got.Get("pageSize") != fmt.Sprint(limit) {
			t.Fatalf("limit %d: %v %v", limit, err, got)
		}
		if _, set := got["pageToken"]; set {
			t.Fatal("empty page token sent")
		}
	}
	if _, err := c.DriveList(ctx, "root", "", "tok/+=", 5); err != nil || got.Get("pageToken") != "tok/+=" {
		t.Fatalf("page token not passed through: %v %v", err, got)
	}
	if _, err := c.DriveList(ctx, "a' or '1'='1", "", "", 5); err == nil {
		t.Fatal("folder id injected into the query")
	}
}

func TestDriveDownloadChoosesEndpoint(t *testing.T) {
	c := driveTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v3/about":
			fmt.Fprint(w, `{"exportFormats":{"application/vnd.google-apps.document":["text/plain"]}}`)
		case "/drive/v3/files/folder":
			fmt.Fprint(w, `{"id":"folder","mimeType":"application/vnd.google-apps.folder"}`)
		case "/drive/v3/files/short":
			fmt.Fprint(w, `{"id":"short","mimeType":"application/vnd.google-apps.shortcut"}`)
		default:
			fmt.Fprint(w, `{"id":"x","mimeType":"application/vnd.google-apps.document"}`)
		}
	})
	ctx := context.Background()
	if _, _, _, err := c.DriveDownload(ctx, "folder", ""); err == nil {
		t.Fatal("folder download accepted")
	}
	_, _, _, err := c.DriveDownload(ctx, "short", "text/plain")
	if err == nil || strings.Contains(err.Error(), "choose export_mime") {
		t.Fatalf("shortcut error is misleading: %v", err)
	}
	_, target, q, err := c.DriveDownload(ctx, "x", "text/plain")
	if err != nil || target != "/files/x/export" || q.Get("mimeType") != "text/plain" {
		t.Fatalf("export request = %s %v %v", target, q, err)
	}
}

func TestDriveSizeNull(t *testing.T) {
	var f DriveFile
	if err := json.Unmarshal([]byte(`{"size":null}`), &f); err != nil || f.Size != nil {
		t.Fatalf("null size: %v %v", f.Size, err)
	}
}
