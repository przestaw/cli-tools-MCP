// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// IssueRaw fetches an issue's full JSON (all fields + names expand).
func (c *Client) IssueRaw(ctx context.Context, key string, expand string) (json.RawMessage, error) {
	q := url.Values{}
	q.Set("expand", "names")
	if expand != "" {
		q.Set("expand", "names,"+expand)
	}
	return c.Raw(ctx, "GET", "/rest/api/3/issue/"+url.PathEscape(key)+"?"+q.Encode(), nil)
}

// Issue fetches and decodes an issue (keeps the raw body in Raw).
func (c *Client) Issue(ctx context.Context, key string, expand string) (*Issue, error) {
	raw, err := c.IssueRaw(ctx, key, expand)
	if err != nil {
		return nil, err
	}
	var is Issue
	if err := json.Unmarshal(raw, &is); err != nil {
		return nil, fmt.Errorf("decode issue %s: %w", key, err)
	}
	is.Raw = raw
	return &is, nil
}

// CreateIssue creates an issue. fields is the Jira "fields" object.
func (c *Client) CreateIssue(ctx context.Context, fields map[string]any) (*IssueRef, error) {
	var out IssueRef
	body := map[string]any{"fields": fields}
	if err := c.do(ctx, "POST", "/rest/api/3/issue", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateIssue updates issue fields (PUT with a "fields" object).
func (c *Client) UpdateIssue(ctx context.Context, key string, fields map[string]any) error {
	body := map[string]any{"fields": fields}
	return c.do(ctx, "PUT", "/rest/api/3/issue/"+url.PathEscape(key), body, nil)
}

// DeleteIssue deletes an issue (optionally cascading to subtasks).
func (c *Client) DeleteIssue(ctx context.Context, key string, deleteSubtasks bool) error {
	p := "/rest/api/3/issue/" + url.PathEscape(key)
	if deleteSubtasks {
		p += "?deleteSubtasks=true"
	}
	return c.do(ctx, "DELETE", p, nil, nil)
}

// Transitions lists the workflow transitions currently available on an issue.
func (c *Client) Transitions(ctx context.Context, key string) ([]Transition, error) {
	var out struct {
		Transitions []Transition `json:"transitions"`
	}
	if err := c.do(ctx, "GET", "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", nil, &out); err != nil {
		return nil, err
	}
	return out.Transitions, nil
}

// DoTransition performs a workflow transition by transition id.
func (c *Client) DoTransition(ctx context.Context, key, transitionID string) error {
	body := map[string]any{"transition": map[string]any{"id": transitionID}}
	return c.do(ctx, "POST", "/rest/api/3/issue/"+url.PathEscape(key)+"/transitions", body, nil)
}

// Assign sets the issue assignee. accountID "" unassigns; "-1" uses the
// project default.
func (c *Client) Assign(ctx context.Context, key, accountID string) error {
	var body map[string]any
	if accountID == "" {
		body = map[string]any{"accountId": nil}
	} else {
		body = map[string]any{"accountId": accountID}
	}
	return c.do(ctx, "PUT", "/rest/api/3/issue/"+url.PathEscape(key)+"/assignee", body, nil)
}

// EditMeta returns the editable fields of an issue (id -> meta).
func (c *Client) EditMeta(ctx context.Context, key string) (map[string]FieldMeta, error) {
	var out struct {
		Fields map[string]FieldMeta `json:"fields"`
	}
	if err := c.do(ctx, "GET", "/rest/api/3/issue/"+url.PathEscape(key)+"/editmeta", nil, &out); err != nil {
		return nil, err
	}
	return out.Fields, nil
}

// CreateMetaIssueTypes lists the issue types available for creation in a project.
func (c *Client) CreateMetaIssueTypes(ctx context.Context, projectKey string) ([]IssueType, error) {
	var all []IssueType
	startAt := 0
	for {
		var out struct {
			IssueTypes []IssueType `json:"issueTypes"`
			IsLast     bool        `json:"isLast"`
			Total      int         `json:"total"`
		}
		p := fmt.Sprintf("/rest/api/3/issue/createmeta/%s/issuetypes?startAt=%d&maxResults=100", url.PathEscape(projectKey), startAt)
		if err := c.do(ctx, "GET", p, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.IssueTypes...)
		startAt += len(out.IssueTypes)
		if out.IsLast || len(out.IssueTypes) == 0 {
			return all, nil
		}
	}
}

// CreateMetaFields lists the create-screen fields for a project + issue type.
func (c *Client) CreateMetaFields(ctx context.Context, projectKey, issueTypeID string) ([]FieldMeta, error) {
	var all []FieldMeta
	startAt := 0
	for {
		var out struct {
			Fields []FieldMeta `json:"fields"`
			IsLast bool        `json:"isLast"`
		}
		p := fmt.Sprintf("/rest/api/3/issue/createmeta/%s/issuetypes/%s?startAt=%d&maxResults=200",
			url.PathEscape(projectKey), url.PathEscape(issueTypeID), startAt)
		if err := c.do(ctx, "GET", p, nil, &out); err != nil {
			return nil, err
		}
		all = append(all, out.Fields...)
		startAt += len(out.Fields)
		if out.IsLast || len(out.Fields) == 0 {
			return all, nil
		}
	}
}
