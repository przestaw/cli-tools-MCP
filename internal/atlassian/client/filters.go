// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"fmt"
	"net/url"
)

// MyFilters lists the filters owned by the authenticated user.
func (c *Client) MyFilters(ctx context.Context) ([]Filter, error) {
	var out []Filter
	if err := c.do(ctx, "GET", "/rest/api/3/filter/my", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Filter fetches one filter by id.
func (c *Client) Filter(ctx context.Context, id string) (*Filter, error) {
	var out Filter
	if err := c.do(ctx, "GET", "/rest/api/3/filter/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchFilters searches filters by name (paged).
func (c *Client) SearchFilters(ctx context.Context, name string, startAt, maxResults int) (*FilterPage, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	p := fmt.Sprintf("/rest/api/3/filter/search?expand=jql,owner&filterName=%s&startAt=%d&maxResults=%d",
		url.QueryEscape(name), startAt, maxResults)
	var out FilterPage
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
