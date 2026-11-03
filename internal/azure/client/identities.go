// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"net/http"
)

// SearchIdentities resolves a name or email to users/groups using the
// IdentityPicker (the typeahead the web UI uses), so partial queries match.
// The returned LocalID is the identity id used by PR reviewers and votes.
func (c *Client) SearchIdentities(ctx context.Context, query string) ([]IdentityMatch, error) {
	body := map[string]any{
		"query":           query,
		"identityTypes":   []string{"user", "group"},
		"operationScopes": []string{"ims", "source"},
		"properties":      []string{"DisplayName", "Account", "Mail", "SignInAddress"},
	}
	full := c.orgURL + "/_apis/IdentityPicker/Identities?api-version=5.0-preview.1"
	var out struct {
		Results []struct {
			Identities []IdentityMatch `json:"identities"`
		} `json:"results"`
	}
	if _, err := c.doJSON(ctx, http.MethodPost, full, body, &out); err != nil {
		return nil, err
	}
	var ids []IdentityMatch
	for _, r := range out.Results {
		ids = append(ids, r.Identities...)
	}
	return ids, nil
}
