package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"workctl/internal/config"
)

func TestGoogle2bThroughExistingTools(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/drive/v3/about":
			if r.URL.Query().Get("fields") == "exportFormats" {
				fmt.Fprint(w, `{"exportFormats":{"application/vnd.google-apps.document":["text/plain"]}}`)
			} else {
				fmt.Fprint(w, `{"user":{"displayName":"Tester"},"storageQuota":{"usage":"9007199254740993"}}`)
			}
		case "/drive/v3/files":
			if !strings.Contains(r.URL.Query().Get("q"), "'root' in parents") {
				t.Errorf("missing folder filter: %s", r.URL.RawQuery)
			}
			if r.URL.Query().Get("pageToken") == "next" {
				fmt.Fprint(w, `{"files":[]}`)
			} else {
				fmt.Fprint(w, `{"files":[{"id":"binary","name":"x","mimeType":"text/plain"}],"nextPageToken":"next"}`)
			}
		case "/drive/v3/files/binary", "/drive/v3/files/badmd5":
			if r.URL.Query().Get("alt") == "media" {
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, "hello")
			} else {
				hash := "5d41402abc4b2a76b9719d911017c592"
				if strings.HasSuffix(r.URL.Path, "badmd5") {
					hash = "00000000000000000000000000000000"
				}
				fmt.Fprintf(w, `{"id":"binary","name":"x","mimeType":"text/plain","size":"5","md5Checksum":"%s"}`, hash)
			}
		case "/drive/v3/files/doc":
			fmt.Fprint(w, `{"id":"doc","name":"document","mimeType":"application/vnd.google-apps.document"}`)
		case "/drive/v3/files/doc/export":
			if r.URL.Query().Get("mimeType") != "text/plain" {
				t.Error("missing export MIME")
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "document text")
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer srv.Close()
	cfg := config.Default()
	cfg.Profiles["drive"] = config.Profile{Kind: "api", URL: srv.URL + "/drive/v3", Auth: "none"}
	if !toolAvailable(cfg, "file") {
		t.Fatal("file not available with Drive-only configuration")
	}
	call := func(tool string, args map[string]any) []byte {
		t.Helper()
		args["profile"] = "drive"
		b, err := callMCPTool(context.Background(), cfg, tool, args)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	call("accounts", map[string]any{})
	if requests != 0 {
		t.Fatal("accounts fetched Drive without check")
	}
	about := call("accounts", map[string]any{"check": true})
	if !strings.Contains(string(about), `"9007199254740993"`) || strings.Contains(string(about), `"limit"`) {
		t.Fatalf("quota precision or absent limit lost: %s", about)
	}
	list := call("file", map[string]any{"query": "a'b"})
	if !strings.Contains(string(list), `"nextPageToken":"next"`) {
		t.Fatal("pagination lost")
	}
	call("file", map[string]any{"page_token": "next"})
	call("file", map[string]any{"path": "binary"})
	for _, tc := range []struct{ id, export, body string }{{"binary", "", "hello"}, {"doc", "text/plain", "document text"}} {
		b := call("download", map[string]any{"path": tc.id, "export_mime": tc.export})
		var result struct {
			Path  string         `json:"path"`
			MD5   *bool          `json:"md5_verified"`
			Drive map[string]any `json:"drive"`
		}
		if err := json.Unmarshal(b, &result); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(result.Path)
		if err != nil || string(data) != tc.body || result.Drive == nil {
			t.Fatalf("download result %s: %s %v", b, data, err)
		}
		if tc.export == "" && (result.MD5 == nil || !*result.MD5) {
			t.Fatal("MD5 not verified")
		}
		if tc.export != "" && result.MD5 != nil {
			t.Fatal("export claims original MD5")
		}
		if err := os.Remove(result.Path); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range []map[string]any{{"path": "doc"}, {"path": "doc", "export_mime": "image/png"}, {"path": "badmd5"}} {
		args["profile"] = "drive"
		if _, err := callMCPTool(context.Background(), cfg, "download", args); err == nil {
			t.Fatalf("invalid download accepted: %v", args)
		}
	}
}
