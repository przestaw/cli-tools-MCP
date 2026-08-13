package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// traceHandler serves a small linked-issue graph:
// A-1 -[blocks]-> A-2 -[blocks]-> A-3, A-1 <-[is blocked by]- A-3 (cycle back).
func traceHandler(t *testing.T) http.HandlerFunc {
	issue := func(key, summary, links string) string {
		return fmt.Sprintf(`{"key":%q,"fields":{"summary":%q,"status":{"name":"Open"},"issuelinks":[%s]}}`, key, summary, links)
	}
	blocks := func(out string) string {
		return fmt.Sprintf(`{"id":"1","type":{"name":"Blocks","inward":"is blocked by","outward":"blocks"},"outwardIssue":{"key":%q,"fields":{"summary":"x"}}}`, out)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "A-1"):
			w.Write([]byte(issue("A-1", "root", blocks("A-2"))))
		case strings.Contains(r.URL.Path, "A-2"):
			w.Write([]byte(issue("A-2", "mid", blocks("A-3"))))
		case strings.Contains(r.URL.Path, "A-3"):
			w.Write([]byte(issue("A-3", "leaf", blocks("A-1"))))
		default:
			w.WriteHeader(404)
			w.Write([]byte(`{"errorMessages":["nope"]}`))
		}
	}
}

func TestTraceWalksAndDetectsCycle(t *testing.T) {
	s := testService(t, traceHandler(t))
	root, err := s.Trace(context.Background(), "A-1", TraceOptions{Depth: 5})
	if err != nil {
		t.Fatal(err)
	}
	if root.Key != "A-1" || len(root.Edges) != 1 {
		t.Fatalf("root = %+v", root)
	}
	mid := root.Edges[0].Node
	if mid.Key != "A-2" || root.Edges[0].Label != "blocks" || root.Edges[0].Direction != "outward" {
		t.Fatalf("mid edge = %+v", root.Edges[0])
	}
	leaf := mid.Edges[0].Node
	if leaf.Key != "A-3" {
		t.Fatalf("leaf = %+v", leaf)
	}
	back := leaf.Edges[0].Node
	if back.Key != "A-1" || !back.Cyclic {
		t.Fatalf("cycle not detected: %+v", back)
	}
}

func TestTraceDepthCap(t *testing.T) {
	s := testService(t, traceHandler(t))
	root, err := s.Trace(context.Background(), "A-1", TraceOptions{Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	mid := root.Edges[0].Node
	if len(mid.Edges) != 0 || !mid.Capped {
		t.Fatalf("depth cap not applied: %+v", mid)
	}
}

func TestNormalizeDirection(t *testing.T) {
	cases := map[string]string{
		"": "both", "both": "both",
		"down": "outward", "outward": "outward", "Down": "outward",
		"up": "inward", "inward": "inward", "UP": "inward",
	}
	for in, want := range cases {
		got, err := NormalizeDirection(in)
		if err != nil || got != want {
			t.Errorf("NormalizeDirection(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := NormalizeDirection("sideways"); err == nil {
		t.Fatal("want error for unknown direction")
	}
}

func TestTraceDirectionUpDown(t *testing.T) {
	s := testService(t, traceHandler(t))
	// "up" follows inward links only; the test graph has only outward links,
	// so the root must have no edges.
	root, err := s.Trace(context.Background(), "A-1", TraceOptions{Depth: 3, Direction: "up"})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Edges) != 0 {
		t.Fatalf("up should follow no outward links: %+v", root.Edges)
	}
	// "down" follows outward links.
	root, err = s.Trace(context.Background(), "A-1", TraceOptions{Depth: 1, Direction: "down"})
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Edges) != 1 || root.Edges[0].Node.Key != "A-2" {
		t.Fatalf("down edges = %+v", root.Edges)
	}
}

func TestFlatLinks(t *testing.T) {
	s := testService(t, traceHandler(t))
	links, err := s.FlatLinks(context.Background(), "A-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].Key != "A-2" || links[0].Label != "blocks" {
		t.Fatalf("links = %+v", links)
	}
}
