// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Anti-CSRF tokens for profiles with a "csrf" block: some servers refuse a write unless it carries a token that a GET
// handed out in the same session (usually only for password logins; API tokens are often exempt). The client fetches
// the token before its first write and keeps the session cookie in memory; neither reaches the output.

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// csrfToken is the header a write must carry; an empty header means none is needed.
type csrfToken struct{ header, value string }

// csrfHeader returns the token of the client's session, fetching it on the first write.
// A 404 from the token endpoint means CSRF protection is off.
func (c *Client) csrfHeader(ctx context.Context) (csrfToken, error) {
	if c.token != nil {
		return *c.token, nil
	}
	b, _, err := c.Do(ctx, http.MethodGet, c.csrf.Path, nil, nil, "")
	var apiErr *Error
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		c.token = &csrfToken{}
		return *c.token, nil
	}
	if err != nil {
		return csrfToken{}, fmt.Errorf("CSRF token: %w", err)
	}
	var answer map[string]any
	if err := json.Unmarshal(b, &answer); err != nil {
		return csrfToken{}, fmt.Errorf("CSRF token: %s did not answer JSON", c.csrf.Path)
	}
	header := c.csrf.Header
	if c.csrf.HeaderField != "" {
		header, _ = answer[c.csrf.HeaderField].(string)
	}
	value, _ := answer[c.csrf.TokenField].(string)
	if header == "" || value == "" {
		return csrfToken{}, fmt.Errorf("CSRF token: %s has no %s", c.csrf.Path, c.csrf.TokenField)
	}
	c.token = &csrfToken{header: header, value: value}
	return *c.token, nil
}
