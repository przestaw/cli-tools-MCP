package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
)

func driveTestClient(t *testing.T, mediaRequests *int) *api.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v3/files/folder":
			fmt.Fprint(w, `{"id":"folder","name":"f","mimeType":"application/vnd.google-apps.folder"}`)
		case "/drive/v3/files/file":
			if r.URL.Query().Get("alt") == "media" {
				*mediaRequests++
				fmt.Fprint(w, "hello")
				return
			}
			fmt.Fprint(w, `{"id":"file","name":"x.txt","mimeType":"text/plain","md5Checksum":"5d41402abc4b2a76b9719d911017c592"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := api.NewProfileClient(config.Default(), "drive", config.Profile{Kind: "api", URL: srv.URL + "/drive/v3", Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestDriveDownloadRefusalsLeaveNothingBehind(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	media := 0
	c := driveTestClient(t, &media)
	ctx := context.Background()
	for name, tc := range map[string]struct {
		path   string
		q      url.Values
		export string
	}{
		"folder":              {path: "folder"},
		"extra query":         {path: "file", q: url.Values{"fields": {"*"}}},
		"extra embedded":      {path: "file?supportsAllDrives=false"},
		"export on a file":    {path: "file", export: "text/plain"},
		"invalid file URL id": {path: "files/a%2Fb"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := download(ctx, c, tc.path, tc.q, "", "", tc.export); err == nil {
				t.Fatal("download accepted")
			}
		})
	}
	if media != 0 {
		t.Fatalf("%d media requests despite refusal", media)
	}
	if entries, _ := filepath.Glob(filepath.Join(cache, "workctl", "downloads", "*")); len(entries) != 0 {
		t.Fatalf("leftover files: %v", entries)
	}
}

func TestDriveDownloadAcceptsIDAndURLFormsWithAltMedia(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	media := 0
	c := driveTestClient(t, &media)
	for _, path := range []string{"file", "files/file", "files/file?alt=media"} {
		out, err := download(context.Background(), c, path, nil, "", "", "")
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		var got downloaded
		if err := json.Unmarshal(out, &got); err != nil {
			t.Fatal(err)
		}
		if got.MD5Verified == nil || !*got.MD5Verified {
			t.Fatalf("%s: not verified: %s", path, out)
		}
		_ = os.Remove(got.Path)
	}
}

func TestExportMIMERequiresDriveFile(t *testing.T) {
	media := 0
	c := driveTestClient(t, &media)
	if _, err := download(context.Background(), c, "https://example.com/files/file", nil, "", "", "text/plain"); err == nil {
		t.Fatal("export_mime accepted for a non-Drive URL")
	}
}

func TestExportMIMERefusedOnNonDriveProfile(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer srv.Close()
	c, err := api.NewProfileClient(config.Default(), "plain", config.Profile{Kind: "api", URL: srv.URL + "/api", Auth: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := download(context.Background(), c, "abc", nil, "", "", "text/plain"); err == nil || requests != 0 {
		t.Fatalf("err = %v, requests = %d", err, requests)
	}
}
