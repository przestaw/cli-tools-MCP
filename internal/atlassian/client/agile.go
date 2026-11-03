// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"fmt"
	"net/url"
)

// Boards lists boards, optionally filtered by project key.
func (c *Client) Boards(ctx context.Context, projectKey string, startAt, maxResults int) (*AgilePage[Board], error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	q := url.Values{}
	q.Set("startAt", fmt.Sprint(startAt))
	q.Set("maxResults", fmt.Sprint(maxResults))
	if projectKey != "" {
		q.Set("projectKeyOrId", projectKey)
	}
	var out AgilePage[Board]
	if err := c.do(ctx, "GET", "/rest/agile/1.0/board?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Board fetches one board.
func (c *Client) Board(ctx context.Context, id int) (*Board, error) {
	var out Board
	if err := c.do(ctx, "GET", fmt.Sprintf("/rest/agile/1.0/board/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BoardSprints lists a board's sprints, optionally filtered by state
// (future, active, closed - comma separable).
func (c *Client) BoardSprints(ctx context.Context, boardID int, state string, startAt, maxResults int) (*AgilePage[Sprint], error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	q := url.Values{}
	q.Set("startAt", fmt.Sprint(startAt))
	q.Set("maxResults", fmt.Sprint(maxResults))
	if state != "" {
		q.Set("state", state)
	}
	var out AgilePage[Sprint]
	if err := c.do(ctx, "GET", fmt.Sprintf("/rest/agile/1.0/board/%d/sprint?%s", boardID, q.Encode()), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Sprint fetches one sprint.
func (c *Client) Sprint(ctx context.Context, id int) (*Sprint, error) {
	var out Sprint
	if err := c.do(ctx, "GET", fmt.Sprintf("/rest/agile/1.0/sprint/%d", id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddIssuesToSprint moves one or more issues into a sprint (agile API).
func (c *Client) AddIssuesToSprint(ctx context.Context, sprintID int, keys ...string) error {
	body := map[string]any{"issues": keys}
	return c.do(ctx, "POST", fmt.Sprintf("/rest/agile/1.0/sprint/%d/issue", sprintID), body, nil)
}

// agileIssuePage is the agile API's issue page envelope.
type agileIssuePage struct {
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
}

// SprintIssues lists a sprint's issues.
func (c *Client) SprintIssues(ctx context.Context, sprintID int, jql string, startAt, maxResults int) ([]Issue, int, error) {
	return c.agileIssues(ctx, fmt.Sprintf("/rest/agile/1.0/sprint/%d/issue", sprintID), jql, startAt, maxResults)
}

// BoardIssues lists a board's issues.
func (c *Client) BoardIssues(ctx context.Context, boardID int, jql string, startAt, maxResults int) ([]Issue, int, error) {
	return c.agileIssues(ctx, fmt.Sprintf("/rest/agile/1.0/board/%d/issue", boardID), jql, startAt, maxResults)
}

func (c *Client) agileIssues(ctx context.Context, path, jql string, startAt, maxResults int) ([]Issue, int, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	q := url.Values{}
	q.Set("startAt", fmt.Sprint(startAt))
	q.Set("maxResults", fmt.Sprint(maxResults))
	q.Set("fields", "summary,status,issuetype,assignee,priority,updated")
	if jql != "" {
		q.Set("jql", jql)
	}
	var out agileIssuePage
	if err := c.do(ctx, "GET", path+"?"+q.Encode(), nil, &out); err != nil {
		return nil, 0, err
	}
	return out.Issues, out.Total, nil
}
