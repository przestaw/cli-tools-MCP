package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// ReleaseDefinitions lists the release pipelines in a project (vsrm host).
func (c *Client) ReleaseDefinitions(ctx context.Context, project, search string, all bool) ([]ReleaseDefinition, error) {
	return getList[ReleaseDefinition](ctx, c, func(token string) string {
		q := url.Values{}
		if search != "" {
			q.Set("searchText", search)
		}
		if token != "" {
			q.Set("continuationToken", token)
		}
		return c.vsrmURLFor(project, "release/definitions", q)
	}, all)
}

// ReleaseSearch are the optional filters for listing releases.
type ReleaseSearch struct {
	DefinitionID int
	StatusFilter string // active|abandoned|draft
	Top          int
}

// Releases lists releases in a project (newest first, vsrm host).
func (c *Client) Releases(ctx context.Context, project string, s ReleaseSearch, all bool) ([]Release, error) {
	return getList[Release](ctx, c, func(token string) string {
		q := url.Values{}
		q.Set("queryOrder", "descending")
		if s.DefinitionID > 0 {
			q.Set("definitionId", strconv.Itoa(s.DefinitionID))
		}
		if s.StatusFilter != "" {
			q.Set("statusFilter", s.StatusFilter)
		}
		if s.Top > 0 {
			q.Set("$top", strconv.Itoa(s.Top))
		}
		if token != "" {
			q.Set("continuationToken", token)
		}
		return c.vsrmURLFor(project, "release/releases", q)
	}, all)
}

// Release fetches one release by id (keeps raw JSON).
func (c *Client) Release(ctx context.Context, project string, id int) (*Release, error) {
	var raw json.RawMessage
	if _, err := c.doJSON(ctx, http.MethodGet, c.vsrmURLFor(project, "release/releases/"+strconv.Itoa(id), nil), nil, &raw); err != nil {
		return nil, err
	}
	var out Release
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode release %d: %w", id, err)
	}
	out.Raw = raw
	return &out, nil
}

// CreateRelease creates a release from a definition.
func (c *Client) CreateRelease(ctx context.Context, project string, definitionID int, description string) (*Release, error) {
	body := map[string]any{
		"definitionId":       definitionID,
		"description":        description,
		"isDraft":            false,
		"reason":             "none",
		"manualEnvironments": nil,
	}
	var out Release
	full := c.vsrmURLFor(project, "release/releases", nil)
	if _, err := c.doJSON(ctx, http.MethodPost, full, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ReleaseWebURL builds a browser URL for a release.
func (c *Client) ReleaseWebURL(project string, id int) string {
	return fmt.Sprintf("%s/%s/_releaseProgress?releaseId=%d", c.orgURL, url.PathEscape(project), id)
}
