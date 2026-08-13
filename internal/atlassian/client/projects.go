package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// Projects lists one page of projects (/rest/api/3/project/search).
func (c *Client) Projects(ctx context.Context, startAt, maxResults int) (*ProjectPage, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	p := fmt.Sprintf("/rest/api/3/project/search?startAt=%d&maxResults=%d&orderBy=key", startAt, maxResults)
	var out ProjectPage
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ProjectsAll lists every project.
func (c *Client) ProjectsAll(ctx context.Context) ([]Project, error) {
	var all []Project
	startAt := 0
	for {
		page, err := c.Projects(ctx, startAt, 50)
		if err != nil {
			return nil, err
		}
		all = append(all, page.Values...)
		startAt += len(page.Values)
		if page.IsLast || len(page.Values) == 0 {
			return all, nil
		}
	}
}

// Project fetches one project by key or id.
func (c *Client) Project(ctx context.Context, keyOrID string) (json.RawMessage, error) {
	return c.Raw(ctx, "GET", "/rest/api/3/project/"+url.PathEscape(keyOrID), nil)
}

// ProjectStatuses returns the project's issue types with their valid statuses.
func (c *Client) ProjectStatuses(ctx context.Context, keyOrID string) (json.RawMessage, error) {
	return c.Raw(ctx, "GET", "/rest/api/3/project/"+url.PathEscape(keyOrID)+"/statuses", nil)
}
