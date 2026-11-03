// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return New(Options{URL: srv.URL, Email: "a@b.c", Token: "tok", VerifyTLS: true})
}

func TestParseErrMsgShapes(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"errorMessages":["boom"],"errors":{}}`, "boom"},
		{`{"errorMessages":[],"errors":{"summary":"is required"}}`, "summary: is required"},
		{`{"errors":[{"status":400,"code":"INVALID","title":"bad cursor"}]}`, "bad cursor"},
		{`{"message":"v1 style"}`, "v1 style"},
		{`plain text`, "plain text"},
	}
	for _, c := range cases {
		if got := parseErrMsg([]byte(c.body)); got != c.want {
			t.Errorf("parseErrMsg(%s) = %q, want %q", c.body, got, c.want)
		}
	}
}

func TestAPIErrorFormat(t *testing.T) {
	e := &APIError{Status: 404, Message: "Issue does not exist"}
	if e.Error() != "atlassian 404: Issue does not exist" {
		t.Fatalf("got %q", e.Error())
	}
	if !IsNotFound(e) {
		t.Fatal("IsNotFound should be true")
	}
}

func TestDoSendsBasicAuthAndDecodes(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || u != "a@b.c" || p != "tok" {
			t.Errorf("basic auth not sent: %s %s %v", u, p, ok)
		}
		if r.URL.Path != "/rest/api/3/myself" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(User{AccountID: "acc-1", DisplayName: "Bob"})
	})
	u, err := c.Myself(context.Background())
	if err != nil {
		t.Fatalf("Myself: %v", err)
	}
	if u.AccountID != "acc-1" || u.DisplayName != "Bob" {
		t.Fatalf("user = %+v", u)
	}
}

func TestSearchJQLDefaultsFields(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		fields, ok := body["fields"].([]any)
		if !ok || len(fields) == 0 {
			t.Errorf("fields not sent (endpoint would return ids only): %v", body["fields"])
		}
		w.Write([]byte(`{"issues":[],"isLast":true}`))
	})
	if _, err := c.SearchJQL(context.Background(), "project = A", nil, 10, ""); err != nil {
		t.Fatal(err)
	}
}

func TestSearchJQLPaginatesWithTokens(t *testing.T) {
	calls := 0
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		switch body["nextPageToken"] {
		case nil:
			w.Write([]byte(`{"issues":[{"key":"A-1","fields":{"summary":"one"}}],"nextPageToken":"t2","isLast":false}`))
		case "t2":
			w.Write([]byte(`{"issues":[{"key":"A-2","fields":{"summary":"two"}}],"isLast":true}`))
		default:
			t.Errorf("unexpected token %v", body["nextPageToken"])
		}
	})
	all, err := c.SearchJQLAll(context.Background(), "project = A", nil, 1)
	if err != nil {
		t.Fatalf("SearchJQLAll: %v", err)
	}
	if len(all) != 2 || all[0].Key != "A-1" || all[1].Key != "A-2" {
		t.Fatalf("issues = %+v", all)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

func TestSearchJQLPageWalksToRequestedPage(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["nextPageToken"] == nil {
			w.Write([]byte(`{"issues":[{"key":"A-1"}],"nextPageToken":"t2"}`))
			return
		}
		w.Write([]byte(`{"issues":[{"key":"A-2"}],"isLast":true}`))
	})
	page, err := c.SearchJQLPage(context.Background(), "project = A", nil, 2, 1)
	if err != nil {
		t.Fatalf("SearchJQLPage: %v", err)
	}
	if len(page.Issues) != 1 || page.Issues[0].Key != "A-2" {
		t.Fatalf("page 2 = %+v", page.Issues)
	}
}

func TestUploadAttachmentSetsNoCheckHeader(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Atlassian-Token") != "no-check" {
			t.Errorf("X-Atlassian-Token missing, got %q", r.Header.Get("X-Atlassian-Token"))
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("Content-Type = %s", r.Header.Get("Content-Type"))
		}
		w.Write([]byte(`[{"id":"10001","filename":"f.txt"}]`))
	})
	as, err := c.UploadAttachment(context.Background(), "A-1", "f.txt", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("UploadAttachment: %v", err)
	}
	if len(as) != 1 || as[0].ID != "10001" {
		t.Fatalf("attachments = %+v", as)
	}
}

func TestUpdatePageBumpsVersion(t *testing.T) {
	var putBody map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Write([]byte(`{"id":"7","title":"Old","version":{"number":3},"body":{"storage":{"value":"<p>old</p>"}}}`))
		case http.MethodPut:
			json.NewDecoder(r.Body).Decode(&putBody)
			w.Write([]byte(`{"id":"7","title":"Old","version":{"number":4}}`))
		}
	})
	pg, err := c.UpdatePage(context.Background(), "7", "", "<p>new</p>", "msg")
	if err != nil {
		t.Fatalf("UpdatePage: %v", err)
	}
	if pg.Version.Number != 4 {
		t.Fatalf("version = %d", pg.Version.Number)
	}
	v := putBody["version"].(map[string]any)
	if v["number"].(float64) != 4 {
		t.Fatalf("PUT version = %v", v)
	}
	if putBody["title"] != "Old" {
		t.Fatalf("title not kept: %v", putBody["title"])
	}
}

func TestSpaceRootPagesSendsDepthRoot(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("depth") != "root" {
			t.Errorf("depth param = %q", r.URL.Query().Get("depth"))
		}
		w.Write([]byte(`{"results":[{"id":"1","title":"Home"}]}`))
	})
	pages, _, err := c.SpaceRootPages(context.Background(), "9", "", 10, "")
	if err != nil || len(pages) != 1 {
		t.Fatalf("pages = %v, %v", pages, err)
	}
}

func TestDirectChildrenTyped(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/wiki/api/v2/folders/5/direct-children") {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{"results":[
		  {"id":"6","title":"Sub","type":"folder"},
		  {"id":"7","title":"Doc","type":"page"}]}`))
	})
	kids, _, err := c.FolderDirectChildren(context.Background(), "5", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 2 || kids[0].Type != "folder" || kids[1].Type != "page" {
		t.Fatalf("children = %+v", kids)
	}
}

func TestSpaceRootFoldersFiltersByParent(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/content/search"):
			w.Write([]byte(`{"results":[{"id":"10","type":"folder","title":"Root"},{"id":"11","type":"folder","title":"Nested"}],"limit":50,"size":2}`))
		case strings.Contains(r.URL.Path, "/folders/10"):
			w.Write([]byte(`{"id":"10","title":"Root","parentId":null}`))
		case strings.Contains(r.URL.Path, "/folders/11"):
			w.Write([]byte(`{"id":"11","title":"Nested","parentId":"10"}`))
		}
	})
	folders, err := c.SpaceRootFolders(context.Background(), "AB", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 1 || folders[0].ID != "10" || folders[0].Type != "folder" {
		t.Fatalf("root folders = %+v", folders)
	}
}

func TestAddWorklogBody(t *testing.T) {
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/3/issue/A-1/worklog" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"id":"100","timeSpent":"2h"}`))
	})
	adf := TextToADF("did things")
	wl, err := c.AddWorklog(context.Background(), "A-1", "2h", "2026-06-12T10:00:00.000+0200", adf)
	if err != nil {
		t.Fatal(err)
	}
	if wl.ID != "100" {
		t.Fatalf("worklog = %+v", wl)
	}
	if body["timeSpent"] != "2h" || body["started"] == nil || body["comment"] == nil {
		t.Fatalf("body = %v", body)
	}
}

func TestAddInlineCommentBody(t *testing.T) {
	var body map[string]any
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wiki/api/v2/inline-comments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&body)
		w.Write([]byte(`{"id":"55","resolutionStatus":"open"}`))
	})
	cm, err := c.AddInlineComment(context.Background(), "7", "<p>hi</p>", "the anchor", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if cm.ID != "55" {
		t.Fatalf("comment = %+v", cm)
	}
	props := body["inlineCommentProperties"].(map[string]any)
	if props["textSelection"] != "the anchor" ||
		props["textSelectionMatchIndex"].(float64) != 1 ||
		props["textSelectionMatchCount"].(float64) != 2 {
		t.Fatalf("props = %v", props)
	}
}

func TestFooterCommentChildrenPath(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/wiki/api/v2/footer-comments/9/children") {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Write([]byte(`{"results":[{"id":"10"}]}`))
	})
	kids, _, err := c.FooterCommentChildren(context.Background(), "9", "", 10)
	if err != nil || len(kids) != 1 {
		t.Fatalf("children = %v, %v", kids, err)
	}
}

func TestNextCursor(t *testing.T) {
	if got := nextCursor("/wiki/api/v2/spaces?limit=2&cursor=abc%3D%3D&x=1"); got != "abc==" {
		t.Fatalf("nextCursor = %q", got)
	}
	if got := nextCursor(""); got != "" {
		t.Fatalf("empty next should be empty, got %q", got)
	}
}

func TestNon2xxBecomesAPIError(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"errorMessages":["Issue does not exist or you do not have permission to see it."]}`))
	})
	_, err := c.Issue(context.Background(), "A-404", "")
	if err == nil || !IsNotFound(err) {
		t.Fatalf("want 404 APIError, got %v", err)
	}
}
