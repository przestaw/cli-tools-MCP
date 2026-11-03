// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"net/url"
	"strconv"
)

// DefaultSearchFields is the field set requested for issue tables.
var DefaultSearchFields = []string{"summary", "status", "issuetype", "assignee", "priority", "updated"}

// SearchJQL runs one page of an enhanced JQL search (/rest/api/3/search/jql,
// token-based pagination). pageToken "" fetches the first page. The endpoint
// returns only issue ids unless fields are requested, so nil fields default to
// DefaultSearchFields ("*all"/"*navigable" request everything).
func (c *Client) SearchJQL(ctx context.Context, jql string, fields []string, maxResults int, pageToken string) (*SearchPage, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	if len(fields) == 0 {
		fields = DefaultSearchFields
	}
	body := map[string]any{
		"jql":        jql,
		"maxResults": maxResults,
		"expand":     "names",
		"fields":     fields,
	}
	if pageToken != "" {
		body["nextPageToken"] = pageToken
	}
	var out SearchPage
	if err := c.do(ctx, "POST", "/rest/api/3/search/jql", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchJQLPage fetches the n-th page (1-based) of a JQL search by walking
// page tokens (the API has no random page access).
func (c *Client) SearchJQLPage(ctx context.Context, jql string, fields []string, page, pageSize int) (*SearchPage, error) {
	if page < 1 {
		page = 1
	}
	token := ""
	for i := 1; ; i++ {
		sp, err := c.SearchJQL(ctx, jql, fields, pageSize, token)
		if err != nil {
			return nil, err
		}
		if i == page {
			return sp, nil
		}
		if sp.NextPageToken == "" || sp.IsLast {
			// requested page is past the end: return an empty page
			return &SearchPage{IsLast: true}, nil
		}
		token = sp.NextPageToken
	}
}

// SearchJQLAll fetches every page of a JQL search.
func (c *Client) SearchJQLAll(ctx context.Context, jql string, fields []string, pageSize int) ([]Issue, error) {
	var all []Issue
	token := ""
	for {
		sp, err := c.SearchJQL(ctx, jql, fields, pageSize, token)
		if err != nil {
			return nil, err
		}
		all = append(all, sp.Issues...)
		if sp.NextPageToken == "" || sp.IsLast || len(sp.Issues) == 0 {
			return all, nil
		}
		token = sp.NextPageToken
	}
}

// ApproximateCount returns the approximate total of a JQL query.
func (c *Client) ApproximateCount(ctx context.Context, jql string) (int, error) {
	var out struct {
		Count int `json:"count"`
	}
	if err := c.do(ctx, "POST", "/rest/api/3/search/approximate-count", map[string]any{"jql": jql}, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

// SearchCQL runs one page of a Confluence CQL content search (v1 API,
// offset pagination).
func (c *Client) SearchCQL(ctx context.Context, cql string, start, limit int) (*CQLPage, error) {
	if limit <= 0 {
		limit = 25
	}
	q := url.Values{}
	q.Set("cql", cql)
	q.Set("start", strconv.Itoa(start))
	q.Set("limit", strconv.Itoa(limit))
	var out CQLPage
	if err := c.do(ctx, "GET", "/wiki/rest/api/content/search?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SearchCQLAll fetches every page of a CQL search.
func (c *Client) SearchCQLAll(ctx context.Context, cql string, pageSize int) ([]CQLResult, error) {
	var all []CQLResult
	start := 0
	for {
		p, err := c.SearchCQL(ctx, cql, start, pageSize)
		if err != nil {
			return nil, err
		}
		all = append(all, p.Results...)
		if len(p.Results) == 0 || len(p.Results) < p.Limit {
			return all, nil
		}
		start += len(p.Results)
	}
}
