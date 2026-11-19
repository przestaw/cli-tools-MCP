package generic

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
)

func TestMD5Needed(t *testing.T) {
	for name, tc := range map[string]struct {
		drive  *api.DriveFile
		export string
		want   bool
	}{
		"drive file with md5":       {&api.DriveFile{MD5: "abc"}, "", true},
		"drive file without md5":    {&api.DriveFile{}, "", false},
		"native document, exported": {&api.DriveFile{}, "text/plain", false},
		"export ignores md5":        {&api.DriveFile{MD5: "abc"}, "text/plain", false},
		"not a drive download":      {nil, "", false},
	} {
		if got := md5Needed(tc.drive, tc.export); got != tc.want {
			t.Errorf("%s: md5Needed = %v, want %v", name, got, tc.want)
		}
	}
}

func TestDownloadMD5Outcomes(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/drive/v3/files/good", "/drive/v3/files/bad":
			if r.URL.Query().Get("alt") == "media" {
				fmt.Fprint(w, "hello")
				return
			}
			sum := "5d41402abc4b2a76b9719d911017c592"
			if strings.HasSuffix(r.URL.Path, "bad") {
				sum = "00000000000000000000000000000000"
			}
			fmt.Fprintf(w, `{"id":"x","name":"x.txt","mimeType":"text/plain","md5Checksum":%q}`, sum)
		case "/drive/v3/about":
			fmt.Fprint(w, `{"exportFormats":{"application/vnd.google-apps.document":["text/plain"]}}`)
		case "/drive/v3/files/doc":
			fmt.Fprint(w, `{"id":"doc","name":"d","mimeType":"application/vnd.google-apps.document"}`)
		case "/drive/v3/files/doc/export":
			fmt.Fprint(w, "text")
		case "/api/plain.log":
			fmt.Fprint(w, "log")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client := func(name, base string) *api.Client {
		c, err := api.NewProfileClient(config.Default(), name, config.Profile{Kind: "api", URL: srv.URL + base, Auth: "none"})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	drive, plain := client("drive", "/drive/v3"), client("plain", "/api")
	dir := t.TempDir()
	run := func(c *api.Client, path, export string) (downloaded, error) {
		out, err := download(context.Background(), c, path, nil, filepath.Join(dir, "out"), "", export)
		var got downloaded
		if err == nil {
			err = json.Unmarshal(out, &got)
		}
		return got, err
	}

	if got, err := run(drive, "good", ""); err != nil || got.MD5Verified == nil || !*got.MD5Verified {
		t.Fatalf("matching sum: %+v %v", got, err)
	}
	if _, err := run(drive, "bad", ""); err == nil {
		t.Fatal("mismatching sum accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "out")); !os.IsNotExist(err) {
		t.Fatalf("file left after mismatch: %v", err)
	}
	if got, err := run(drive, "doc", "text/plain"); err != nil || got.MD5Verified != nil {
		t.Fatalf("export: %+v %v", got, err)
	}
	if got, err := run(plain, "plain.log", ""); err != nil || got.MD5Verified != nil || got.Drive != nil {
		t.Fatalf("generic file: %+v %v", got, err)
	}
}
