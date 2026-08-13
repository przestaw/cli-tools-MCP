package client

import (
	"context"
	"fmt"
	"net/url"
)

// Myself returns the authenticated user.
func (c *Client) Myself(ctx context.Context) (*User, error) {
	var out User
	if err := c.do(ctx, "GET", "/rest/api/3/myself", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UserByAccountID fetches a user by accountId.
func (c *Client) UserByAccountID(ctx context.Context, accountID string) (*User, error) {
	var out User
	if err := c.do(ctx, "GET", "/rest/api/3/user?accountId="+url.QueryEscape(accountID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FindUsers searches users by display name or email.
func (c *Client) FindUsers(ctx context.Context, query string) ([]User, error) {
	var out []User
	if err := c.do(ctx, "GET", "/rest/api/3/user/search?maxResults=50&query="+url.QueryEscape(query), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AssignableUsers lists users that can be assigned to issues in the project.
// An empty query returns the first page; maxResults defaults to 50.
func (c *Client) AssignableUsers(ctx context.Context, projectKey, query string, maxResults int) ([]User, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	q := url.Values{}
	q.Set("project", projectKey)
	q.Set("maxResults", fmt.Sprint(maxResults))
	if query != "" {
		q.Set("query", query)
	}
	var out []User
	if err := c.do(ctx, "GET", "/rest/api/3/user/assignable/search?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
