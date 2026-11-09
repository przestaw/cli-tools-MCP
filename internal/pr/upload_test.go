// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package pr

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workctl/internal/config"
)

// pngBytes is the PNG signature plus padding: enough for content sniffing.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 32)...)

type seenUpload struct{ method, path, query, contentType, atlassianToken, body string }

func uploadServer(t *testing.T, seen *[]seenUpload) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*seen = append(*seen, seenUpload{r.Method, r.URL.EscapedPath(), r.URL.RawQuery, r.Header.Get("Content-Type"), r.Header.Get("X-Atlassian-Token"), string(b)})
		switch r.URL.Path {
		case "/repos/acme/app":
			_, _ = io.WriteString(w, `{"id":42}`)
		case "/user-attachments/assets":
			_, _ = io.WriteString(w, `{"url":"https://github.com/user-attachments/assets/abc"}`)
		case "/api/v4/projects/g/p/uploads":
			_, _ = io.WriteString(w, `{"url":"/uploads/h/shot.png","full_path":"/g/p/uploads/h/shot.png","markdown":"![shot](/uploads/h/shot.png)"}`)
		case "/rest/api/2/issue/ABC-1/attachments":
			_, _ = io.WriteString(w, `[{"filename":"shot.png","content":"https://jira.test/secure/attachment/1/shot.png"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func uploadConfig(t *testing.T, kind, url string) *config.Config {
	t.Helper()
	t.Setenv("TEST_UPLOAD_TOKEN", "secret")
	cfg := config.Default()
	cfg.Policy.AllowWrite = true
	p := config.Profile{Kind: kind, URL: url, TokenEnv: "TEST_UPLOAD_TOKEN", VerifyTLS: true}
	switch kind {
	case "github":
		p.DefaultRepo = "acme/app"
	case "gitlab":
		p.DefaultProject = "g/p"
	}
	cfg.Profiles[kind] = p
	return cfg
}

func mediaFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUploadToEachProvider(t *testing.T) {
	var seen []seenUpload
	srv := uploadServer(t, &seen)
	githubUploads[srv.URL] = srv.URL
	t.Cleanup(func() { delete(githubUploads, srv.URL) })
	shot := mediaFile(t, "shot.png", pngBytes)
	cases := []struct {
		kind     string
		args     map[string]any
		markdown string
	}{
		{"github", map[string]any{"path": shot}, "![shot.png](https://github.com/user-attachments/assets/abc)"},
		{"gitlab", map[string]any{"path": shot}, "![shot](/uploads/h/shot.png)"},
		{"jira", map[string]any{"path": shot, "issue": "ABC-1"}, "!shot.png!"},
	}
	for _, c := range cases {
		seen = nil
		out, err := CallTool(context.Background(), uploadConfig(t, c.kind, srv.URL), "upload", c.args)
		if err != nil {
			t.Fatalf("%s: %v", c.kind, err)
		}
		var got map[string]string
		if err := json.Unmarshal(out, &got); err != nil || got["markdown"] != c.markdown {
			t.Fatalf("%s: %s", c.kind, out)
		}
		last := seen[len(seen)-1]
		switch c.kind {
		case "github":
			if last.query != "name=shot.png&repository_id=42" || last.contentType != "image/png" || last.body != string(pngBytes) {
				t.Errorf("github upload: %+v", last)
			}
		case "gitlab":
			if !strings.HasPrefix(last.contentType, "multipart/form-data") || !strings.Contains(last.body, `filename="shot.png"`) {
				t.Errorf("gitlab upload: %+v", last)
			}
		case "jira":
			if last.atlassianToken != "no-check" || !strings.HasPrefix(last.contentType, "multipart/form-data") {
				t.Errorf("jira upload: %+v", last)
			}
		}
	}
}

func TestUploadRefusesWhatIsNotMedia(t *testing.T) {
	var seen []seenUpload
	srv := uploadServer(t, &seen)
	cfg := uploadConfig(t, "gitlab", srv.URL)
	for _, path := range []string{
		mediaFile(t, "notes.txt", []byte("secret")),
		mediaFile(t, "fake.png", []byte("not an image at all")),
		"relative.png",
	} {
		if _, err := CallTool(context.Background(), cfg, "upload", map[string]any{"path": path}); err == nil {
			t.Errorf("%s was uploaded", path)
		}
	}
	cfg.Policy.AllowWrite = false
	if _, err := CallTool(context.Background(), cfg, "upload", map[string]any{"path": mediaFile(t, "shot.png", pngBytes)}); err == nil {
		t.Error("upload without allow_write")
	}
	if len(seen) != 0 {
		t.Errorf("requests sent: %+v", seen)
	}
}
