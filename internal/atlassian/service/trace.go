package service

import (
	"context"
	"fmt"
	"strings"

	"workctl/internal/atlassian/client"
)

// TraceNode is one issue in a traceability tree.
type TraceNode struct {
	Key     string
	Summary string
	Status  string
	Edges   []TraceEdge
	Cyclic  bool // already shown elsewhere in the tree
	Capped  bool // children beyond the depth limit
}

// TraceEdge is a directed link from a parent node to a child node.
type TraceEdge struct {
	Direction string // outward | inward | parent | subtask
	Label     string // directional phrase, e.g. "blocks" / "is blocked by"
	Node      *TraceNode
}

// FlatLink is one row of the flat (one-level) trace table.
type FlatLink struct {
	Direction string
	Label     string
	Key       string
	Summary   string
	Status    string
}

// TraceOptions configure a trace traversal.
type TraceOptions struct {
	Depth     int    // max link depth (default 2)
	Direction string // down (outward) | up (inward) | both (default both)
	Hierarchy bool   // include parent/subtask edges
}

// NormalizeDirection maps user direction words to the internal link
// directions: down -> outward (e.g. "blocks"), up -> inward (e.g. "is
// blocked by"). The internal names stay accepted as aliases.
func NormalizeDirection(dir string) (string, error) {
	switch strings.ToLower(dir) {
	case "", "both":
		return "both", nil
	case "down", "outward":
		return "outward", nil
	case "up", "inward":
		return "inward", nil
	default:
		return "", fmt.Errorf("unknown direction %q (use up, down, or both)", dir)
	}
}

// Trace builds the link tree of an issue by walking issuelinks (and optionally
// the parent/subtask hierarchy) breadth-consistently up to Depth.
func (s *Service) Trace(ctx context.Context, key string, opt TraceOptions) (*TraceNode, error) {
	if opt.Depth <= 0 {
		opt.Depth = 2
	}
	dir, err := NormalizeDirection(opt.Direction)
	if err != nil {
		return nil, err
	}
	opt.Direction = dir
	visited := map[string]bool{}
	return s.traceNode(ctx, key, opt, visited, opt.Depth)
}

func (s *Service) traceNode(ctx context.Context, key string, opt TraceOptions, visited map[string]bool, depthLeft int) (*TraceNode, error) {
	is, err := s.Client.Issue(ctx, key, "")
	if err != nil {
		return nil, err
	}
	node := &TraceNode{Key: is.Key, Summary: is.Fields.Summary}
	if is.Fields.Status != nil {
		node.Status = is.Fields.Status.Name
	}
	visited[is.Key] = true
	if depthLeft == 0 {
		node.Capped = hasEdges(&is.Fields, opt)
		return node, nil
	}

	type rawEdge struct {
		direction string
		label     string
		ref       *client.IssueRef
	}
	var edges []rawEdge
	for _, l := range is.Fields.IssueLinks {
		if l.OutwardIssue != nil && (opt.Direction == "both" || opt.Direction == "outward") {
			edges = append(edges, rawEdge{"outward", l.Type.Outward, l.OutwardIssue})
		}
		if l.InwardIssue != nil && (opt.Direction == "both" || opt.Direction == "inward") {
			edges = append(edges, rawEdge{"inward", l.Type.Inward, l.InwardIssue})
		}
	}
	if opt.Hierarchy {
		if is.Fields.Parent != nil {
			edges = append(edges, rawEdge{"parent", "parent", is.Fields.Parent})
		}
		for i := range is.Fields.Subtasks {
			edges = append(edges, rawEdge{"subtask", "subtask", &is.Fields.Subtasks[i]})
		}
	}

	for _, e := range edges {
		if e.ref == nil || e.ref.Key == "" {
			continue
		}
		if visited[e.ref.Key] {
			leaf := &TraceNode{Key: e.ref.Key, Cyclic: true}
			if e.ref.Fields != nil {
				leaf.Summary = e.ref.Fields.Summary
			}
			node.Edges = append(node.Edges, TraceEdge{Direction: e.direction, Label: e.label, Node: leaf})
			continue
		}
		child, err := s.traceNode(ctx, e.ref.Key, opt, visited, depthLeft-1)
		if err != nil {
			// Unreadable child (permissions etc.): show the ref, keep going.
			child = &TraceNode{Key: e.ref.Key, Summary: "(" + strings.TrimPrefix(err.Error(), "atlassian ") + ")"}
		}
		node.Edges = append(node.Edges, TraceEdge{Direction: e.direction, Label: e.label, Node: child})
	}
	return node, nil
}

// hasEdges reports whether fields carry any edge the options would follow.
func hasEdges(f *client.IssueFields, opt TraceOptions) bool {
	for _, l := range f.IssueLinks {
		if l.OutwardIssue != nil && (opt.Direction == "both" || opt.Direction == "outward") {
			return true
		}
		if l.InwardIssue != nil && (opt.Direction == "both" || opt.Direction == "inward") {
			return true
		}
	}
	if opt.Hierarchy && (f.Parent != nil || len(f.Subtasks) > 0) {
		return true
	}
	return false
}

// FlatLinks returns an issue's links as flat rows (one level).
func (s *Service) FlatLinks(ctx context.Context, key string) ([]FlatLink, error) {
	links, err := s.Client.IssueLinks(ctx, key)
	if err != nil {
		return nil, err
	}
	var out []FlatLink
	add := func(direction, label string, ref *client.IssueRef) {
		if ref == nil {
			return
		}
		fl := FlatLink{Direction: direction, Label: label, Key: ref.Key}
		if ref.Fields != nil {
			fl.Summary = ref.Fields.Summary
			if ref.Fields.Status != nil {
				fl.Status = ref.Fields.Status.Name
			}
		}
		out = append(out, fl)
	}
	for _, l := range links {
		add("outward", l.Type.Outward, l.OutwardIssue)
		add("inward", l.Type.Inward, l.InwardIssue)
	}
	return out, nil
}
