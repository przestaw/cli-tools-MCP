// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package client is a thin Azure DevOps REST client covering Core (projects),
// Git (repositories, pull requests), Pipelines, Build and Release Management.
// All requests use Basic auth with an empty username and a Personal Access
// Token, and carry the api-version query parameter the REST API requires.
package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"workctl/internal/httpx"
)

// apiVersion is the default REST API version sent on every request.
const apiVersion = "7.1"

// Client talks to one Azure DevOps organization using a PAT. Org-scoped calls
// go to orgURL (https://dev.azure.com/{org}); Release Management calls go to
// vsrmURL (https://vsrm.dev.azure.com/{org}).
type Client struct {
	orgURL  string
	vsrmURL string
	pat     string
	http    *http.Client
}

// Options configure a Client.
type Options struct {
	OrgURL    string // e.g. https://dev.azure.com/your-org
	Token     string // Personal Access Token
	Timeout   time.Duration
	VerifyTLS bool
}

// New builds a Client from Options.
func New(o Options) *Client {
	base := strings.TrimRight(o.OrgURL, "/")
	return &Client{
		orgURL:  base,
		vsrmURL: vsrmBase(base),
		pat:     o.Token,
		http:    httpx.NewClient(o.Timeout, o.VerifyTLS),
	}
}

// vsrmBase derives the Release Management host from the org host.
func vsrmBase(orgURL string) string {
	if strings.Contains(orgURL, "://dev.azure.com") {
		return strings.Replace(orgURL, "://dev.azure.com", "://vsrm.dev.azure.com", 1)
	}
	if strings.Contains(orgURL, ".visualstudio.com") {
		return strings.Replace(orgURL, ".visualstudio.com", ".vsrm.visualstudio.com", 1)
	}
	return orgURL
}

// OrgURL returns the organization base URL (for building human links).
func (c *Client) OrgURL() string { return c.orgURL }

// apiURL builds an org-scoped _apis URL. project may be "" for org-level calls.
// q may carry extra query values; an api-version is added unless q already sets one.
func (c *Client) apiURL(project, apiPath string, q url.Values) string {
	return buildURL(c.orgURL, project, apiPath, q)
}

// vsrmURLFor builds a Release Management _apis URL (vsrm host).
func (c *Client) vsrmURLFor(project, apiPath string, q url.Values) string {
	return buildURL(c.vsrmURL, project, apiPath, q)
}

func buildURL(host, project, apiPath string, q url.Values) string {
	var b strings.Builder
	b.WriteString(host)
	if project != "" {
		b.WriteString("/")
		b.WriteString(url.PathEscape(project))
	}
	b.WriteString("/_apis/")
	b.WriteString(strings.TrimLeft(apiPath, "/"))
	if q == nil {
		q = url.Values{}
	}
	if q.Get("api-version") == "" {
		q.Set("api-version", apiVersion)
	}
	b.WriteString("?")
	b.WriteString(q.Encode())
	return b.String()
}

// WebURL builds a browser URL under the org (web routes live off the org root).
func (c *Client) WebURL(suffix string) string {
	return c.orgURL + "/" + strings.TrimLeft(suffix, "/")
}

// Raw executes an arbitrary request against a full URL or an org-relative path
// and returns the raw JSON. A leading "/" is treated as org-relative; an
// absolute URL is used verbatim. Used by the `raw` command to probe endpoints.
func (c *Client) Raw(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	full := path
	if strings.HasPrefix(path, "/") {
		full = c.orgURL + path
	}
	var out json.RawMessage
	if _, err := c.doJSON(ctx, method, full, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// doJSON executes a request against a full URL and decodes a JSON 2xx body into
// out (out may be nil to discard). It returns the response headers (for
// continuation-token paging). body may be nil; a json.RawMessage is sent
// verbatim, anything else is marshalled.
func (c *Client) doJSON(ctx context.Context, method, full string, body any, out any) (http.Header, error) {
	req := &http.Request{Header: make(http.Header)}
	req.SetBasicAuth("", c.pat)
	return httpx.JSON(ctx, c.http, method, full, req.Header, body, out, apiError, func(data []byte) error {
		if looksLikeHTML(data) {
			return apiError(http.StatusUnauthorized, full, data)
		}
		return nil
	})
}

func apiError(status int, url string, data []byte) error {
	return &APIError{Status: status, Message: parseErrMsg(data), URL: url}
}

// doText fetches a full URL and returns the raw body + content type (used for
// build/release logs and file content, which are returned as plain text).
func (c *Client) doText(ctx context.Context, full string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, "", err
	}
	req.SetBasicAuth("", c.pat)
	data, hdr, err := httpx.Do(c.http, req, apiError)
	if err != nil {
		return nil, "", err
	}
	return data, hdr.Get("Content-Type"), nil
}

// listEnvelope is the standard Azure DevOps list shape.
type listEnvelope[T any] struct {
	Count int `json:"count"`
	Value []T `json:"value"`
}

// getList fetches a list endpoint, following the x-ms-continuationtoken header
// when all is set. top (>0) is applied as $top. The builder produces the URL
// for a given continuation token ("" for the first page).
func getList[T any](ctx context.Context, c *Client, builder func(token string) string, all bool) ([]T, error) {
	var out []T
	token := ""
	for {
		var env listEnvelope[T]
		hdr, err := c.doJSON(ctx, http.MethodGet, builder(token), nil, &env)
		if err != nil {
			return nil, err
		}
		out = append(out, env.Value...)
		token = hdr.Get("x-ms-continuationtoken")
		if !all || token == "" {
			return out, nil
		}
	}
}

// looksLikeHTML reports whether data begins with an HTML/XML marker.
func looksLikeHTML(data []byte) bool {
	s := strings.TrimSpace(string(data))
	return strings.HasPrefix(s, "<!DOCTYPE") || strings.HasPrefix(s, "<html") || strings.HasPrefix(s, "<HTML")
}

// parseErrMsg extracts a human message from the Azure DevOps error body
// (`{"message":"...","typeKey":"..."}`), falling back to the trimmed raw body.
func parseErrMsg(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var e struct {
		Message string `json:"message"`
		Value   *struct {
			Message string `json:"Message"`
		} `json:"value"`
	}
	if json.Unmarshal(data, &e) == nil {
		if e.Message != "" {
			return e.Message
		}
		if e.Value != nil && e.Value.Message != "" {
			return e.Value.Message
		}
	}
	if looksLikeHTML(data) {
		return "authentication failed (check your PAT and its scopes)"
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
