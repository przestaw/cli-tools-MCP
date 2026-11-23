// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package generic

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
)

// driveWithExistingFile serves a Drive where "exp.ipynb" already exists in the folder and counts replacements.
func driveWithExistingFile(t *testing.T, patched *bool) (config.Profile, *config.Config) {
	t.Helper()
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v3/files":
			_, _ = w.Write([]byte(`{"files":[{"id":"old-id","name":"exp.ipynb"}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/upload/drive/v3/files/old-id":
			*patched = true
			w.Header().Set("Location", srvURL+"/upload/drive/v3/files?upload_id=s")
		case r.Method == http.MethodPut && r.URL.Query().Get("upload_id") == "s":
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte(`{"id":"old-id","name":"exp.ipynb"}`))
		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL

	t.Setenv("TEST_API_TOK", "tok")
	cfg := config.Default()
	cfg.Policy.AllowWrite = true
	prof := config.Profile{Kind: "api", URL: srv.URL + "/drive/v3", Auth: "bearer", TokenEnv: "TEST_API_TOK", DefaultProject: "folder"}
	cfg.Profiles["gdrive"] = prof
	return prof, cfg
}

func TestUploadRefusesToReplaceWithoutOverwrite(t *testing.T) {
	var patched bool
	prof, cfg := driveWithExistingFile(t, &patched)
	local := filepath.Join(t.TempDir(), "exp.ipynb")
	if err := os.WriteFile(local, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := api.NewProfileClient(cfg, "gdrive", prof)
	if err != nil {
		t.Fatal(err)
	}
	_, err = UploadFile(context.Background(), c, prof, local, UploadOptions{})
	if err == nil || !strings.Contains(err.Error(), "old-id") || patched {
		t.Fatalf("want refusal, got %v patched=%v", err, patched)
	}
	if _, err := UploadFile(context.Background(), c, prof, local, UploadOptions{Overwrite: true}); err != nil || !patched {
		t.Fatalf("overwrite failed: %v patched=%v", err, patched)
	}
}
