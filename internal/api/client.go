// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package api is the HTTP transport of the thin providers (GitHub, GitLab, Jira, kind api):
// a small JSON API client bound to one profile. A profile with backend "cli"
// keeps the same Client interface but sends its requests through gh or glab (see cli.go).
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"workctl/internal/config"
	"workctl/internal/httpx"
)

// Error is an HTTP error status answered by the provider.
type Error struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *Error) Error() string {
	return httpx.RawError(e.Method, e.URL, e.Status, e.Body)
}

// Client sends JSON API requests of one profile. The zero value is not usable; use NewKindClient or NewProfileClient.
type Client struct {
	responseHeaders *http.Header
	baseURL         string
	prefix          string
	client          *http.Client
	headers         http.Header
	cli             *cliBackend  // set for profiles with backend "cli": requests go to gh or glab, not to client
	csrf            *config.CSRF // set when writes need an anti-CSRF token (see csrf.go)
	token           *csrfToken   // fetched on the first write; empty header when the server has CSRF protection off
	form            *formSession // set for auth "form": logs in before the first request (see formlogin.go)
	readOnly        bool         // refuse everything but GET and HEAD
}

func newClient(cfg config.Profile, prefix string, timeout time.Duration) (*Client, error) {
	base := config.NormalizeBaseURL(cfg.URL)
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q", cfg.URL)
	}
	tr, err := httpx.Transport(cfg.VerifyTLS, cfg.CAFile)
	if err != nil {
		return nil, err
	}
	tr.Proxy = http.ProxyFromEnvironment
	if cfg.UsesSSHTunnel() {
		useSSHTunnel(tr, cfg.SSHDestination)
	}
	c := &http.Client{
		Timeout:   timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return fmt.Errorf("refusing cross-host redirect from %s to %s", via[0].URL.Host, req.URL.Host)
			}
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
	h := make(http.Header)
	h.Set("Accept", "application/json")
	h.Set("User-Agent", "workctl/0.4.4")
	client := &Client{baseURL: base, prefix: prefix, client: c, headers: h, csrf: cfg.CSRF, readOnly: cfg.ReadOnly}
	if cfg.CSRF != nil || cfg.FormLogin != nil {
		// Servers tie the token or the login to the session cookie.
		c.Jar, _ = cookiejar.New(nil)
	}
	return client, nil
}

// NewKindClient connects to the default profile of a kind (jira, gitlab, github, api).
func NewKindClient(cfg *config.Config, kind string) (*Client, error) {
	name, profile, err := cfg.ProfileOf(kind)
	if err != nil {
		return nil, err
	}
	return NewProfileClient(cfg, name, profile)
}

// NewProfileClient connects to one named profile, resolving its credential and authentication scheme.
func NewProfileClient(cfg *config.Config, name string, profile config.Profile) (*Client, error) {
	if err := config.ValidateProfile(name, profile); err != nil {
		return nil, err
	}
	if profile.UsesCLI() {
		return newCLIClient(profile, cfg.ProfileTimeout(profile))
	}
	if profile.UsesSSHExec() {
		return nil, fmt.Errorf("profile %q runs remote commands over ssh, not HTTP: use `workctl api exec`", name)
	}
	client, err := newClient(profile, "", cfg.ProfileTimeout(profile))
	if err != nil {
		return nil, err
	}
	auth := strings.ToLower(profile.Auth)
	if auth == "none" {
		return client, nil
	}
	credential, err := config.ResolveProfileSecret(name, profile)
	if err != nil {
		return nil, err
	}
	switch profile.Kind {
	case "github":
		client.headers.Set("Authorization", "Bearer "+credential)
		client.headers.Set("Accept", "application/vnd.github+json")
		client.headers.Set("X-GitHub-Api-Version", "2022-11-28")
	case "gitlab":
		if auth == "bearer" {
			client.headers.Set("Authorization", "Bearer "+credential)
		} else {
			client.headers.Set("PRIVATE-TOKEN", credential)
		}
		client.prefix = "/api/v4"
	case "jira":
		if auth == "basic" {
			client.headers.Set("Authorization", basicAuth(profile.Username, credential))
		} else {
			client.headers.Set("Authorization", "Bearer "+credential)
		}
		client.prefix = "/rest/api/2"
	case "atlassian": // Jira Cloud: e-mail and API token over Basic, or an OAuth bearer token
		if auth == "bearer" {
			client.headers.Set("Authorization", "Bearer "+credential)
		} else {
			client.headers.Set("Authorization", basicAuth(profile.Username, credential))
		}
		client.prefix = "/rest/api/2"
	case "api":
		switch auth {
		case "basic":
			client.headers.Set("Authorization", basicAuth(profile.Username, credential))
		case "header":
			client.headers.Set(profile.AuthHeader, credential)
		case "form":
			client.form = &formSession{login: *profile.FormLogin, username: profile.Username, password: credential, cache: sessionFile(name, profile)}
		default:
			client.headers.Set("Authorization", "Bearer "+credential)
		}
	default:
		return nil, fmt.Errorf("profile %q has unsupported HTTP provider %q", name, profile.Kind)
	}
	return client, nil
}

// basicAuth is the value of an HTTP Basic Authorization header.
func basicAuth(username, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+secret))
}

// WithAccept returns a copy of the client that sends another Accept header (for example a raw-contents media type).
func (c *Client) WithAccept(accept string) *Client { return c.WithHeader("Accept", accept) }

// WithHeader returns a copy of the client that sends one more header (for example Jira's X-Atlassian-Token).
func (c *Client) WithHeader(key, value string) *Client {
	copied := *c
	copied.headers = c.headers.Clone()
	copied.headers.Set(key, value)
	return &copied
}

// WithBaseURL returns a copy of the client for another host of the same provider (GitHub uploads). Callers pass a
// fixed host of that provider, never a URL from the agent, because the credential goes with it.
func (c *Client) WithBaseURL(base string) *Client {
	copied := *c
	copied.baseURL = strings.TrimRight(base, "/")
	return &copied
}

// relPath turns a caller's path into the server-relative path of a request: a path that starts with "/" is used as it
// is, any other one gets the provider prefix. Absolute URLs are refused so a credential cannot leave the profile's host.
func (c *Client) relPath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return "", fmt.Errorf("absolute URLs are not allowed; use a path on the configured server")
	}
	if !strings.HasPrefix(path, "/") {
		path = strings.TrimRight(c.prefix, "/") + "/" + path
	}
	return path, nil
}

// IsReadMethod reports whether an HTTP method only reads (GET or HEAD); every other method counts as a write.
func IsReadMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

// Do sends one request, natively over net/http or, for a cli profile, through gh or glab (see cli.go).
// It applies the profile's policies (read-only, form login, anti-CSRF token) and returns the response body and its
// Content-Type. The body is passed on as the server sent it, HTML included.
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body []byte, contentType string) ([]byte, string, error) {
	target, err := c.target(method, path, q)
	if err != nil {
		return nil, "", err
	}
	if c.cli != nil {
		return c.cli.do(ctx, method, target, c.headers.Get("Accept"), body, contentType)
	}
	headers, err := c.authorize(ctx, method)
	if err != nil {
		return nil, "", err
	}
	out, respType, err := c.send(ctx, method, target, headers, body, contentType)
	if f := c.form; err == nil && f != nil && f.cached {
		// A saved session the server dropped answers with the login page: log in again and repeat the request once.
		f.cached = false
		if _, loginPage := formHTML(out, "", f.login.PasswordField); loginPage {
			c.dropSession()
			if headers, err = c.authorize(ctx, method); err != nil {
				return nil, "", err
			}
			return c.send(ctx, method, target, headers, body, contentType)
		}
	}
	return out, respType, err
}

// DoWithHeaders retains response headers for callers that need Location as well as the body.
func (c *Client) DoWithHeaders(ctx context.Context, method, path string, q url.Values, body []byte, contentType string) ([]byte, http.Header, error) {
	if c.cli != nil {
		return nil, nil, fmt.Errorf("response metadata requires a native HTTP profile")
	}
	var headers http.Header
	copy := *c
	copy.responseHeaders = &headers
	data, _, err := copy.Do(ctx, method, path, q, body, contentType)
	return data, headers, err
}

// ValidateRequest checks a request locally without authenticating or sending it.
func (c *Client) ValidateRequest(method, path string, q url.Values) error {
	target, err := c.target(method, path, q)
	if err != nil {
		return err
	}
	_, err = http.NewRequest(method, c.baseURL+target, nil)
	return err
}

// target turns path and query into a server-relative target and enforces profile read-only policy.
func (c *Client) target(method, path string, q url.Values) (string, error) {
	target, err := c.relPath(path)
	if err != nil {
		return "", err
	}
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + q.Encode()
	}
	if c.readOnly && !IsReadMethod(method) {
		return "", fmt.Errorf("the profile is read-only: %s is not allowed", method)
	}
	return target, nil
}

// authorize returns the headers of one native request: it logs in with the form first when the profile needs it,
// and adds the anti-CSRF token to a write.
func (c *Client) authorize(ctx context.Context, method string) (http.Header, error) {
	if c.form != nil && !c.form.loggedIn {
		if c.loadSession() {
			c.form.loggedIn, c.form.cached = true, true
		} else if err := c.logIn(ctx); err != nil {
			return nil, err
		}
	}
	headers := c.headers.Clone()
	if c.csrf != nil && !IsReadMethod(method) {
		tok, err := c.csrfHeader(ctx)
		if err != nil {
			return nil, err
		}
		if tok.header != "" {
			headers.Set(tok.header, tok.value)
		}
	}
	return headers, nil
}

// newRequest builds one HTTP request to a server-relative target; a body without a content type is sent as JSON.
func (c *Client) newRequest(ctx context.Context, method, target string, headers http.Header, body []byte, contentType string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+target, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header = headers
	if body != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}

// send performs one HTTP request and reads the whole answer, with no policy of its own: Do and the form login use it.
func (c *Client) send(ctx context.Context, method, target string, headers http.Header, body []byte, contentType string) ([]byte, string, error) {
	req, err := c.newRequest(ctx, method, target, headers, body, contentType)
	if err != nil {
		return nil, "", err
	}
	data, hdr, err := httpx.Do(c.client, req, func(status int, url string, body []byte) error {
		return &Error{Method: method, URL: url, Status: status, Body: string(body)}
	})
	if c.responseHeaders != nil {
		*c.responseHeaders = hdr
	}
	return data, hdr.Get("Content-Type"), err
}

// BodyOnly adapts a Do result to callers that need only the body.
func BodyOnly(b []byte, _ string, err error) ([]byte, error) { return b, err }
