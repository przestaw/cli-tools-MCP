// Package client is a thin Atlassian Cloud REST client covering Jira
// (/rest/api/3, /rest/agile/1.0) and Confluence (/wiki/api/v2, /wiki/rest/api).
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"workctl/internal/httpx"
)

// Client talks to one Atlassian Cloud site using Basic auth (email + API token).
// All request paths are absolute from the site root, e.g. /rest/api/3/myself
// or /wiki/api/v2/pages/123.
type Client struct {
	baseURL string // e.g. https://your-team.atlassian.net
	email   string
	token   string
	http    *http.Client
}

// Options configure a Client.
type Options struct {
	URL       string // site base URL, e.g. https://your-team.atlassian.net
	Email     string
	Token     string
	Timeout   time.Duration
	VerifyTLS bool
}

// New builds a Client from Options.
func New(o Options) *Client {
	base := strings.TrimRight(o.URL, "/")
	return &Client{
		baseURL: base,
		email:   o.Email,
		token:   o.Token,
		http:    httpx.NewClient(o.Timeout, o.VerifyTLS),
	}
}

// BaseURL returns the site base URL (for building human links).
func (c *Client) BaseURL() string { return c.baseURL }

// IssueURL returns the browser URL for an issue key.
func (c *Client) IssueURL(key string) string {
	return c.baseURL + "/browse/" + key
}

// WikiURL joins a Confluence _links.webui path onto the site's /wiki base.
func (c *Client) WikiURL(webui string) string {
	if webui == "" {
		return c.baseURL + "/wiki"
	}
	if strings.HasPrefix(webui, "http") {
		return webui
	}
	return c.baseURL + "/wiki" + webui
}

// Raw executes an arbitrary request against the site root and returns the raw
// JSON response. Used by the `raw` debug command to probe endpoints.
func (c *Client) Raw(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
	var out json.RawMessage
	if err := c.do(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// do executes a request against baseURL+path and decodes a JSON 2xx body into
// out (out may be nil to discard). body may be nil; a json.RawMessage body is
// sent verbatim, anything else is marshalled.
func (c *Client) do(ctx context.Context, method, path string, body any, out any) error {
	req := &http.Request{Header: make(http.Header)}
	req.SetBasicAuth(c.email, c.token)
	_, err := httpx.JSON(ctx, c.http, method, c.baseURL+path, req.Header, body, out, apiError, nil)
	return err
}

func apiError(status int, url string, data []byte) error {
	return &APIError{Status: status, Message: parseErrMsg(data), URL: url}
}

// doRawURL fetches an absolute URL and returns body + content type (used for
// attachment downloads, whose content URLs live on a media host).
func (c *Client) doRawURL(ctx context.Context, method, url string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, "", err
	}
	req.SetBasicAuth(c.email, c.token)
	data, hdr, err := httpx.Do(c.http, req, apiError)
	if err != nil {
		return nil, "", err
	}
	return data, hdr.Get("Content-Type"), nil
}

// doMultipart uploads one file as multipart/form-data with the
// X-Atlassian-Token: no-check header Jira/Confluence require for uploads.
func (c *Client) doMultipart(ctx context.Context, path, fieldName, fileName string, content io.Reader, out any) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile(fieldName, fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(fw, content); err != nil {
		return err
	}
	if err := mw.Close(); err != nil {
		return err
	}
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return err
	}
	req.SetBasicAuth(c.email, c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Atlassian-Token", "no-check")
	data, _, err := httpx.Do(c.http, req, apiError)
	if err != nil {
		return err
	}
	return httpx.Decode(url, data, out)
}

// parseErrMsg extracts a human message from the error-body shapes the Jira and
// Confluence APIs use, falling back to the trimmed raw body.
//
//	Jira:          {"errorMessages":["..."],"errors":{"summary":"..."}}
//	Confluence v2: {"errors":[{"status":400,"code":"...","title":"..."}]}
//	Confluence v1: {"message":"..."}
func parseErrMsg(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var jira struct {
		ErrorMessages []string          `json:"errorMessages"`
		Errors        map[string]string `json:"errors"`
	}
	if json.Unmarshal(data, &jira) == nil {
		var parts []string
		parts = append(parts, jira.ErrorMessages...)
		for k, v := range jira.Errors {
			parts = append(parts, k+": "+v)
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	var confV2 struct {
		Errors []struct {
			Code  string `json:"code"`
			Title string `json:"title"`
		} `json:"errors"`
	}
	if json.Unmarshal(data, &confV2) == nil && len(confV2.Errors) > 0 {
		var parts []string
		for _, e := range confV2.Errors {
			if e.Title != "" {
				parts = append(parts, e.Title)
			} else if e.Code != "" {
				parts = append(parts, e.Code)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	var v1 struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &v1) == nil && v1.Message != "" {
		return v1.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}
