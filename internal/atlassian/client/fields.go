package client

import (
	"context"
)

// Fields lists all field definitions of the site (system + custom).
func (c *Client) Fields(ctx context.Context) ([]Field, error) {
	var out []Field
	if err := c.do(ctx, "GET", "/rest/api/3/field", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
