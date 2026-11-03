// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

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
