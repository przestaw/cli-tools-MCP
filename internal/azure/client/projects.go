package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
)

// ConnectionData returns the authenticated user (used by `config test`).
func (c *Client) ConnectionData(ctx context.Context) (*ConnectionData, error) {
	var out ConnectionData
	// connectionData is not api-versioned the same way; call it directly.
	full := c.orgURL + "/_apis/connectionData"
	if _, err := c.doJSON(ctx, http.MethodGet, full, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Projects lists the team projects in the organization.
func (c *Client) Projects(ctx context.Context, all bool) ([]Project, error) {
	return getList[Project](ctx, c, func(token string) string {
		q := url.Values{}
		if token != "" {
			q.Set("continuationToken", token)
		}
		return c.apiURL("", "projects", q)
	}, all)
}

// Project fetches one project by name or id (raw JSON).
func (c *Client) Project(ctx context.Context, nameOrID string) (json.RawMessage, error) {
	var out json.RawMessage
	if _, err := c.doJSON(ctx, http.MethodGet, c.apiURL("", "projects/"+url.PathEscape(nameOrID), nil), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
