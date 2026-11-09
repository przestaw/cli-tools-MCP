// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"workctl/internal/config"
)

func TestGetAllPagesReadsUntilAShortPage(t *testing.T) {
	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages = append(pages, r.URL.Query().Get("page"))
		n := 100
		if page, _ := strconv.Atoi(r.URL.Query().Get("page")); page == 2 {
			n = 5
		}
		_, _ = fmt.Fprint(w, "["+strings.TrimSuffix(strings.Repeat(`{"x":1},`, n), ",")+"]")
	}))
	t.Cleanup(srv.Close)
	c, err := NewProfileClient(config.Default(), "p", config.Profile{Kind: "api", URL: srv.URL, Auth: "none", VerifyTLS: true})
	if err != nil {
		t.Fatal(err)
	}
	out, err := c.GetAllPages(context.Background(), "/items", nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(out, &items); err != nil || len(items) != 105 || strings.Join(pages, ",") != "1,2" {
		t.Fatalf("items=%d pages=%v err=%v", len(items), pages, err)
	}
	if out, _ := c.GetAllPages(context.Background(), "/items", nil, 1); strings.Count(string(out), "{") != 100 {
		t.Fatalf("maxPages=1 must stop after one page: %d items", strings.Count(string(out), "{"))
	}
}
