// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"errors"
	"fmt"
	"net/http"
	"testing"
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
