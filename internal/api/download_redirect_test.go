// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/config"
)

func TestDownloadFollowsAllowedStorageRedirectsWithoutTheCredential(t *testing.T) {
	var storageHeaders http.Header
	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storageHeaders = r.Header.Clone()
		if r.URL.Path == "/page" {
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<html>sign in</html>")
			return
		}
		_, _ = io.WriteString(w, "log bytes")
	}))
	defer storage.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v4/log":
			http.Redirect(w, r, storage.URL+"/blob", http.StatusFound)
		case "/api/v4/page":
			http.Redirect(w, r, storage.URL+"/page", http.StatusFound)
		default:
			http.Redirect(w, r, "https://elsewhere.invalid/x", http.StatusFound)
		}
	}))
	defer api.Close()
	t.Setenv("TEST_DL_TOKEN", "secret-token")
	c, err := NewProfileClient(config.Default(), "gl", config.Profile{Kind: "gitlab", URL: api.URL, TokenEnv: "TEST_DL_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	old := allowedRedirectHost
	allowedRedirectHost = func(host string) bool { return host == "127.0.0.1" }
	defer func() { allowedRedirectHost = old }()

	var buf bytes.Buffer
	meta, err := c.Stream(context.Background(), "log", nil, &buf)
	if err != nil || buf.String() != "log bytes" || meta.RedirectedTo != "127.0.0.1" {
		t.Fatalf("stream: %q %+v %v", buf.String(), meta, err)
	}
	for name, values := range storageHeaders {
		for _, v := range values {
			if strings.Contains(v, "secret-token") {
				t.Fatalf("credential forwarded to storage in %s", name)
			}
		}
	}
	buf.Reset()
	if meta, err := c.Stream(context.Background(), "page", nil, &buf); err != nil || meta.Warning == "" {
		t.Fatalf("html answer must carry a warning: %+v %v", meta, err)
	}
	if _, err := c.Stream(context.Background(), "other", nil, &buf); err == nil || !strings.Contains(err.Error(), "refusing cross-host redirect") {
		t.Fatalf("redirect to a host off the list: %v", err)
	}
	if _, _, err := c.Do(context.Background(), http.MethodGet, "log", nil, nil, ""); err == nil {
		t.Fatal("API requests (not downloads) keep refusing cross-host redirects")
	}
}

func TestStreamStorageURLSendsNoCredential(t *testing.T) {
	var auth string
	storage := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization") + r.Header.Get("PRIVATE-TOKEN")
		if r.URL.Query().Get("jwt") != "signed" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "\x89PNG")
	}))
	defer storage.Close()
	t.Setenv("TEST_DL_TOKEN", "secret-token")
	c, err := NewProfileClient(config.Default(), "gh", config.Profile{Kind: "github", URL: "https://api.github.test", TokenEnv: "TEST_DL_TOKEN"})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := c.StreamStorageURL(context.Background(), storage.URL+"/a.png?jwt=signed", &buf); err == nil {
		t.Fatal("a host off the storage list must be refused")
	}
	old := allowedRedirectHost
	allowedRedirectHost = func(host string) bool { return host == "127.0.0.1" }
	defer func() { allowedRedirectHost = old }()
	meta, err := c.StreamStorageURL(context.Background(), storage.URL+"/a.png?jwt=signed", &buf)
	if err != nil || buf.String() != "\x89PNG" || meta.ContentType != "image/png" || auth != "" {
		t.Fatalf("meta=%+v err=%v auth=%q", meta, err, auth)
	}
	if _, err := c.StreamStorageURL(context.Background(), strings.Replace(storage.URL, "https", "http", 1)+"/a.png", &buf); err == nil {
		t.Fatal("plain http must be refused")
	}
}
