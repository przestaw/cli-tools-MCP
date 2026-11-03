// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// BuildDefinitions lists the build definitions in a project.
func (c *Client) BuildDefinitions(ctx context.Context, project, nameFilter string, all bool) ([]BuildDefinition, error) {
	return getList[BuildDefinition](ctx, c, func(token string) string {
		q := url.Values{}
		if nameFilter != "" {
			q.Set("name", nameFilter)
		}
		if token != "" {
			q.Set("continuationToken", token)
		}
		return c.apiURL(project, "build/definitions", q)
	}, all)
}

// BuildSearch are the optional filters for listing builds.
type BuildSearch struct {
	DefinitionIDs string // comma-separated definition ids
	Branch        string // refs/heads/...
	StatusFilter  string // inProgress|completed|cancelling|postponed|notStarted|all
	ResultFilter  string // succeeded|partiallySucceeded|failed|canceled
	Top           int
}

// Builds lists builds in a project (newest first by default).
func (c *Client) Builds(ctx context.Context, project string, s BuildSearch, all bool) ([]Build, error) {
	return getList[Build](ctx, c, func(token string) string {
		q := url.Values{}
		q.Set("queryOrder", "queueTimeDescending")
		if s.DefinitionIDs != "" {
			q.Set("definitions", s.DefinitionIDs)
		}
		if s.Branch != "" {
			q.Set("branchName", s.Branch)
		}
		if s.StatusFilter != "" {
			q.Set("statusFilter", s.StatusFilter)
		}
		if s.ResultFilter != "" {
			q.Set("resultFilter", s.ResultFilter)
		}
		if s.Top > 0 {
			q.Set("$top", strconv.Itoa(s.Top))
		}
		if token != "" {
			q.Set("continuationToken", token)
		}
		return c.apiURL(project, "build/builds", q)
	}, all)
}

// Build fetches one build by id (keeps raw JSON).
func (c *Client) Build(ctx context.Context, project string, id int) (*Build, error) {
	var raw json.RawMessage
	if _, err := c.doJSON(ctx, http.MethodGet, c.apiURL(project, "build/builds/"+strconv.Itoa(id), nil), nil, &raw); err != nil {
		return nil, err
	}
	var out Build
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode build %d: %w", id, err)
	}
	out.Raw = raw
	return &out, nil
}

// QueueBuild queues a new build of a definition. branch (optional) is
// refs/heads/... ; the build picks the definition default otherwise.
func (c *Client) QueueBuild(ctx context.Context, project string, definitionID int, branch string) (*Build, error) {
	body := map[string]any{"definition": map[string]any{"id": definitionID}}
	if branch != "" {
		body["sourceBranch"] = branch
	}
	var out Build
	full := c.apiURL(project, "build/builds", nil)
	if _, err := c.doJSON(ctx, http.MethodPost, full, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// BuildLogs lists the logs of a build.
func (c *Client) BuildLogs(ctx context.Context, project string, buildID int) ([]BuildLog, error) {
	return getList[BuildLog](ctx, c, func(string) string {
		return c.apiURL(project, "build/builds/"+strconv.Itoa(buildID)+"/logs", nil)
	}, false)
}

// BuildLogContent fetches the plain-text content of one build log.
func (c *Client) BuildLogContent(ctx context.Context, project string, buildID, logID int) ([]byte, error) {
	data, _, err := c.doText(ctx, c.apiURL(project, "build/builds/"+strconv.Itoa(buildID)+"/logs/"+strconv.Itoa(logID), nil))
	return data, err
}

// BuildWebURL builds a browser URL for a build result.
func (c *Client) BuildWebURL(project string, id int) string {
	return fmt.Sprintf("%s/%s/_build/results?buildId=%d", c.orgURL, url.PathEscape(project), id)
}
