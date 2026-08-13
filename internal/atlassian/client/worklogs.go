package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Worklog is one Jira issue worklog entry (comment is ADF).
type Worklog struct {
	ID               string          `json:"id"`
	Author           *User           `json:"author"`
	Started          string          `json:"started"`
	TimeSpent        string          `json:"timeSpent"`
	TimeSpentSeconds int64           `json:"timeSpentSeconds"`
	Comment          json.RawMessage `json:"comment"`
	Created          string          `json:"created"`
}

// WorklogPage is the paged worklog list response.
type WorklogPage struct {
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	Total      int       `json:"total"`
	Worklogs   []Worklog `json:"worklogs"`
}

// Worklogs lists one page of an issue's worklogs.
func (c *Client) Worklogs(ctx context.Context, key string, startAt, maxResults int) (*WorklogPage, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	p := fmt.Sprintf("/rest/api/3/issue/%s/worklog?startAt=%d&maxResults=%d", url.PathEscape(key), startAt, maxResults)
	var out WorklogPage
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AddWorklog logs time on an issue. timeSpent uses Jira duration syntax
// ("2h 30m", "1d"); started (optional) is a Jira timestamp like
// 2026-06-12T10:00:00.000+0200; comment (optional) is an ADF document.
func (c *Client) AddWorklog(ctx context.Context, key, timeSpent, started string, adfComment any) (*Worklog, error) {
	body := map[string]any{"timeSpent": timeSpent}
	if started != "" {
		body["started"] = started
	}
	if adfComment != nil {
		body["comment"] = adfComment
	}
	var out Worklog
	if err := c.do(ctx, "POST", "/rest/api/3/issue/"+url.PathEscape(key)+"/worklog", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
