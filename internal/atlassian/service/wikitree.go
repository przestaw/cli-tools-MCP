package service

import (
	"context"

	"workctl/internal/atlassian/client"
)

// WikiNode is one row of a recursive Confluence content listing.
type WikiNode struct {
	Depth int    `json:"depth"` // 1 = direct child of the start node
	Type  string `json:"type"`
	ID    string `json:"id"`
	Title string `json:"title"`
}

// maxWikiNodes caps a descendants walk against runaway trees.
const maxWikiNodes = 1000

// WikiDescendants walks a page's or folder's content tree depth-first up to
// maxDepth levels, folders before pages on each level. Only pages and folders
// are descended into (whiteboards etc. are listed as leaves).
func (s *Service) WikiDescendants(ctx context.Context, id string, isFolder bool, maxDepth int) ([]WikiNode, error) {
	if maxDepth <= 0 {
		maxDepth = 3
	}
	var out []WikiNode
	err := s.walkWiki(ctx, id, isFolder, 1, maxDepth, &out)
	return out, err
}

func (s *Service) walkWiki(ctx context.Context, id string, isFolder bool, depth, maxDepth int, out *[]WikiNode) error {
	if depth > maxDepth || len(*out) >= maxWikiNodes {
		return nil
	}
	var (
		children []client.ContentChild
		cursor   string
		err      error
	)
	for {
		var batch []client.ContentChild
		if isFolder {
			batch, cursor, err = s.Client.FolderDirectChildren(ctx, id, cursor, 100)
		} else {
			batch, cursor, err = s.Client.PageDirectChildren(ctx, id, cursor, 100)
		}
		if err != nil {
			return err
		}
		children = append(children, batch...)
		if cursor == "" || len(batch) == 0 {
			break
		}
	}
	// Folders first, then the rest, keeping tree order within each group.
	ordered := make([]client.ContentChild, 0, len(children))
	for _, c := range children {
		if c.Type == "folder" {
			ordered = append(ordered, c)
		}
	}
	for _, c := range children {
		if c.Type != "folder" {
			ordered = append(ordered, c)
		}
	}
	for _, c := range ordered {
		if len(*out) >= maxWikiNodes {
			return nil
		}
		*out = append(*out, WikiNode{Depth: depth, Type: c.Type, ID: c.ID, Title: c.Title})
		if c.Type == "page" || c.Type == "folder" {
			if err := s.walkWiki(ctx, c.ID, c.Type == "folder", depth+1, maxDepth, out); err != nil {
				return err
			}
		}
	}
	return nil
}
