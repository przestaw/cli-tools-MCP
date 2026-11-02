package generic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIDownloadWritesAFileAndReportsIt(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache) // Linux
	t.Setenv("LocalAppData", cache)   // Windows
	payload := "line 1\nline 2"       // no trailing newline: it must stay that way
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("FileID") != "3" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Disposition", `attachment; filename="../../etc/service log.txt"`)
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()
	cfg := testConfig(srv.URL)
	ctx := context.Background()

	out, err := CallTool(ctx, cfg, "api_download", map[string]any{"path": "/attachment", "query": map[string]any{"FileID": "3"}})
	if err != nil {
		t.Fatal(err)
	}
	var got downloaded
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(got.Path)
	if err != nil || string(b) != payload || got.Bytes != int64(len(payload)) {
		t.Fatalf("file %s = %q, %v, meta %+v", got.Path, b, err, got)
	}
	if !strings.HasPrefix(got.Path, cache) || !strings.HasSuffix(got.Path, "-service_log.txt") {
		t.Fatalf("path %s: want a file in the downloads directory named after the server's file, without its directories", got.Path)
	}

	if _, err := CallTool(ctx, cfg, "api_download", map[string]any{"path": "/attachment", "query": map[string]any{"FileID": "9"}}); err == nil {
		t.Fatal("missing file reported as downloaded")
	}
	entries, _ := os.ReadDir(filepath.Dir(got.Path))
	if len(entries) != 1 {
		t.Fatalf("a failed download left files behind: %v", entries)
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"omsservice.log.gz":           "omsservice.log.gz",
		"../../.bashrc":               "bashrc",
		`C:\Users\x\report (1).pdf`:   "report_1_.pdf",
		"/index.pl?Action=Attachment": "index.pl",
		"":                            "file",
	} {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}
