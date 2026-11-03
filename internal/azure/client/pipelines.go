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

// Pipelines lists the pipelines in a project (Pipelines API).
func (c *Client) Pipelines(ctx context.Context, project string, all bool) ([]Pipeline, error) {
	return getList[Pipeline](ctx, c, func(token string) string {
		q := url.Values{}
		if token != "" {
			q.Set("continuationToken", token)
		}
		return c.apiURL(project, "pipelines", q)
	}, all)
}

// Pipeline fetches one pipeline by id (raw JSON).
func (c *Client) Pipeline(ctx context.Context, project string, id int) (json.RawMessage, error) {
	var out json.RawMessage
	if _, err := c.doJSON(ctx, http.MethodGet, c.apiURL(project, "pipelines/"+strconv.Itoa(id), nil), nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// PipelineRuns lists the runs of a pipeline (newest first).
func (c *Client) PipelineRuns(ctx context.Context, project string, pipelineID int) ([]Run, error) {
	return getList[Run](ctx, c, func(string) string {
		return c.apiURL(project, "pipelines/"+strconv.Itoa(pipelineID)+"/runs", nil)
	}, false)
}

// PipelineRun fetches one run (keeps raw JSON).
func (c *Client) PipelineRun(ctx context.Context, project string, pipelineID, runID int) (*Run, error) {
	var raw json.RawMessage
	if _, err := c.doJSON(ctx, http.MethodGet, c.apiURL(project, "pipelines/"+strconv.Itoa(pipelineID)+"/runs/"+strconv.Itoa(runID), nil), nil, &raw); err != nil {
		return nil, err
	}
	var out Run
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode run %d: %w", runID, err)
	}
	out.Raw = raw
	return &out, nil
}

// RunPipeline queues a new run of a pipeline. body may be nil (run the default
// branch) or carry resources/templateParameters/stagesToSkip.
func (c *Client) RunPipeline(ctx context.Context, project string, pipelineID int, body map[string]any) (*Run, error) {
	if body == nil {
		body = map[string]any{}
	}
	var out Run
	full := c.apiURL(project, "pipelines/"+strconv.Itoa(pipelineID)+"/runs", nil)
	if _, err := c.doJSON(ctx, http.MethodPost, full, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
