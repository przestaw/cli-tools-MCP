// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package google

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
)

func TestDriveRootLookupRejectsMalformedAndAmbiguousResults(t *testing.T) {
	for _, answer := range []string{`not json`, `{"files":[{}]}`, `{"files":[{"id":"a"},{"id":"b"}]}`, `{"files":[],"nextPageToken":"more"}`} {
		t.Run(answer, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.Contains(r.URL.Query().Get("q"), "'root' in parents") {
					t.Errorf("root lookup missing: %s", r.URL.RawQuery)
				}
				_, _ = io.WriteString(w, answer)
			}))
			defer srv.Close()
			c, err := api.NewProfileClient(config.Default(), "p", config.Profile{Kind: "api", URL: srv.URL, Auth: "none"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := findFileInFolder(context.Background(), c, "file", ""); err == nil {
				t.Fatal("invalid lookup accepted")
			}
		})
	}
}

func TestDriveUploadRejectsInvalidFinalResponse(t *testing.T) {
	for _, answer := range []string{`not json`, `{}`} {
		t.Run(answer, func(t *testing.T) {
			var serverURL string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method {
				case http.MethodGet:
					_, _ = io.WriteString(w, `{"files":[]}`)
				case http.MethodPost:
					w.Header().Set("Location", serverURL+"/session")
				case http.MethodPut:
					_, _ = io.Copy(io.Discard, r.Body)
					_, _ = io.WriteString(w, answer)
				}
			}))
			defer srv.Close()
			serverURL = srv.URL
			p := config.Profile{Kind: "api", URL: srv.URL + "/drive/v3", Auth: "none"}
			c, err := api.NewProfileClient(config.Default(), "p", p)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Upload(context.Background(), c, p, strings.NewReader("x"), 1, "x", "text/plain", "", false); err == nil {
				t.Fatal("invalid completion reported as successful upload")
			}
		})
	}
}
