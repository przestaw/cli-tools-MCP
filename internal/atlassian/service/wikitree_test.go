package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// wikiTreeHandler serves: page 1 -> [folder 2, page 3]; folder 2 -> [page 4].
func wikiTreeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/pages/1/direct-children"):
			w.Write([]byte(`{"results":[
			  {"id":"3","title":"Doc","type":"page"},
			  {"id":"2","title":"Folder","type":"folder"}]}`))
		case strings.Contains(r.URL.Path, "/folders/2/direct-children"):
			w.Write([]byte(`{"results":[{"id":"4","title":"Nested","type":"page"}]}`))
		default:
			w.Write([]byte(`{"results":[]}`))
		}
	}
}

func TestWikiDescendantsFoldersFirstAndRecursive(t *testing.T) {
	s := testService(t, wikiTreeHandler())
	nodes, err := s.WikiDescendants(context.Background(), "1", false, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("nodes = %+v", nodes)
	}
	// Folder 2 first (folders-first), its child 4 next (depth-first), page 3 last.
	if nodes[0].ID != "2" || nodes[0].Depth != 1 || nodes[0].Type != "folder" {
		t.Fatalf("first = %+v", nodes[0])
	}
	if nodes[1].ID != "4" || nodes[1].Depth != 2 {
		t.Fatalf("second = %+v", nodes[1])
	}
	if nodes[2].ID != "3" || nodes[2].Depth != 1 {
		t.Fatalf("third = %+v", nodes[2])
	}
}

func TestWikiDescendantsDepthLimit(t *testing.T) {
	s := testService(t, wikiTreeHandler())
	nodes, err := s.WikiDescendants(context.Background(), "1", false, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Depth > 1 {
			t.Fatalf("depth limit violated: %+v", n)
		}
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes = %+v", nodes)
	}
}
