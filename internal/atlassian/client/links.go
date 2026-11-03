// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"encoding/json"
	"net/url"
)

// LinkTypes lists the issue link types of the site.
func (c *Client) LinkTypes(ctx context.Context) ([]LinkType, error) {
	var out struct {
		IssueLinkTypes []LinkType `json:"issueLinkTypes"`
	}
	if err := c.do(ctx, "GET", "/rest/api/3/issueLinkType", nil, &out); err != nil {
		return nil, err
	}
	return out.IssueLinkTypes, nil
}

// IssueLinks returns an issue's links (from its issuelinks field).
func (c *Client) IssueLinks(ctx context.Context, key string) ([]IssueLink, error) {
	var out struct {
		Fields struct {
			IssueLinks []IssueLink `json:"issuelinks"`
		} `json:"fields"`
	}
	p := "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=issuelinks"
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, err
	}
	return out.Fields.IssueLinks, nil
}

// CreateLink links two issues so that fromKey carries the type's outward
// phrase toward toKey (e.g. type "Blocks": from blocks to).
//
// Jira Cloud renders the OUTWARD phrase on the issue sent as "inwardIssue"
// and the INWARD phrase on the issue sent as "outwardIssue" — the payload
// field names are the reverse of the displayed direction (verified against
// /rest/api/3/issueLink on Jira Cloud, 2026-07). Hence from -> inwardIssue.
func (c *Client) CreateLink(ctx context.Context, typeName, fromKey, toKey string) error {
	body := map[string]any{
		"type":         map[string]any{"name": typeName},
		"inwardIssue":  map[string]any{"key": fromKey},
		"outwardIssue": map[string]any{"key": toKey},
	}
	return c.do(ctx, "POST", "/rest/api/3/issueLink", body, nil)
}

// Link fetches one issue link by link id.
func (c *Client) Link(ctx context.Context, id string) (json.RawMessage, error) {
	return c.Raw(ctx, "GET", "/rest/api/3/issueLink/"+url.PathEscape(id), nil)
}

// DeleteLink deletes an issue link by link id.
func (c *Client) DeleteLink(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/rest/api/3/issueLink/"+url.PathEscape(id), nil, nil)
}

// RemoteLinks lists an issue's remote (web) links.
func (c *Client) RemoteLinks(ctx context.Context, key string) (json.RawMessage, error) {
	return c.Raw(ctx, "GET", "/rest/api/3/issue/"+url.PathEscape(key)+"/remotelink", nil)
}
