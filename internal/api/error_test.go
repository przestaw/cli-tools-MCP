// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"workctl/internal/config"
)

func TestStatusHelpersSeeThroughWrapping(t *testing.T) {
	err := fmt.Errorf("fetch: %w", &Error{Method: "GET", URL: "/x", Status: http.StatusUnauthorized})
	if !IsStatus(err, http.StatusUnauthorized) || IsStatus(err, http.StatusNotFound) {
		t.Fatalf("IsStatus on %v", err)
	}
	if status, ok := StatusOf(err); !ok || status != http.StatusUnauthorized {
		t.Fatalf("StatusOf = %d, %v", status, ok)
	}
	if _, ok := StatusOf(errors.New("plain")); ok || IsStatus(nil, http.StatusOK) {
		t.Fatal("a non-HTTP error has no status")
	}
}

func TestDoBodyMatchesDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte("body"))
	}))
	defer srv.Close()
	client, err := newClient(config.Profile{URL: srv.URL, VerifyTLS: true}, "", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path   string
		want   string
		status int
	}{
		{"/ok", "body", 0},
		{"/missing", "", http.StatusNotFound},
	}
	for _, tc := range tests {
		got, err := client.DoBody(context.Background(), "GET", tc.path, nil, nil, "")
		if string(got) != tc.want && tc.status == 0 {
			t.Errorf("%s: body %q, want %q", tc.path, got, tc.want)
		}
		if tc.status != 0 && !IsStatus(err, tc.status) {
			t.Errorf("%s: err = %v, want status %d", tc.path, err, tc.status)
		}
		if tc.status == 0 && err != nil {
			t.Errorf("%s: unexpected error %v", tc.path, err)
		}
	}
}
