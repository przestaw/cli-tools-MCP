// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"fmt"
	"net/url"
)

// Comments lists one page of an issue's comments.
func (c *Client) Comments(ctx context.Context, key string, startAt, maxResults int) (*CommentPage, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	p := fmt.Sprintf("/rest/api/3/issue/%s/comment?startAt=%d&maxResults=%d&orderBy=created", url.PathEscape(key), startAt, maxResults)
	var out CommentPage
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddComment adds a comment; body is an ADF document.
func (c *Client) AddComment(ctx context.Context, key string, adfBody any) (*Comment, error) {
	var out Comment
	if err := c.do(ctx, "POST", "/rest/api/3/issue/"+url.PathEscape(key)+"/comment", map[string]any{"body": adfBody}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Comment fetches a single comment.
func (c *Client) Comment(ctx context.Context, key, commentID string) (*Comment, error) {
	var out Comment
	p := "/rest/api/3/issue/" + url.PathEscape(key) + "/comment/" + url.PathEscape(commentID)
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteComment deletes a comment.
func (c *Client) DeleteComment(ctx context.Context, key, commentID string) error {
	p := "/rest/api/3/issue/" + url.PathEscape(key) + "/comment/" + url.PathEscape(commentID)
	return c.do(ctx, "DELETE", p, nil, nil)
}
