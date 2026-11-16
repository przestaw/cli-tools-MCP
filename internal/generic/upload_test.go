// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package generic

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

	"workctl/internal/api"
	"workctl/internal/config"
)

func TestUploadGoogleDriveResumableAndColabURL(t *testing.T) {
	var received []byte
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/drive/v3/files":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/upload/drive/v3/files":
			if r.URL.Query().Get("uploadType") != "resumable" {
				http.Error(w, "expected resumable", http.StatusBadRequest)
				return
			}
			metaBody, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(metaBody), "folder-root-123") {
				http.Error(w, "missing parent folder", http.StatusBadRequest)
				return
			}
			w.Header().Set("Location", srvURL+"/upload/drive/v3/files?uploadType=resumable&upload_id=sess-1")
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPut && r.URL.Path == "/upload/drive/v3/files" && r.URL.Query().Get("upload_id") == "sess-1":
			received, _ = io.ReadAll(r.Body)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"nb-file-42","name":"exp.ipynb","mimeType":"application/vnd.google.colaboratory","webViewLink":"https://drive.google.com/file/d/nb-file-42/view"}`))
		default:
			http.Error(w, "unexpected: "+r.Method+" "+r.URL.String(), http.StatusNotFound)
		}
	}))
	defer srv.Close()
	srvURL = srv.URL

	t.Setenv("TEST_API_TOK", "tok")
	cfg := config.Default()
	cfg.Policy.AllowWrite = true
	prof := config.Profile{
		Kind:           "api",
		URL:            srv.URL + "/drive/v3",
		Auth:           "bearer",
		TokenEnv:       "TEST_API_TOK",
		DefaultProject: "folder-root-123",
		VerifyTLS:      false,
	}
	cfg.Profiles["gdrive"] = prof

	localFile := filepath.Join(t.TempDir(), "exp.ipynb")
	payload := []byte(`{"nbformat":4,"cells":[]}`)
	if err := os.WriteFile(localFile, payload, 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := api.NewProfileClient(cfg, "gdrive", prof)
	if err != nil {
		t.Fatal(err)
	}
	out, err := UploadFile(context.Background(), c, prof, localFile, UploadOptions{})
	if err != nil {
		t.Fatalf("UploadFile failed: %v", err)
	}
	if string(received) != string(payload) {
		t.Fatalf("received %q, want %q", received, payload)
	}
	var res UploadResult
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	if res.ID != "nb-file-42" || res.ColabURL != "https://colab.research.google.com/drive/nb-file-42" {
		t.Fatalf("unexpected UploadResult: %+v", res)
	}
}
