// Package api is the HTTP transport of the thin providers (GitHub, GitLab, Jira):
// a small JSON API client bound to one profile. A profile with backend "cli"
// keeps the same Client interface but sends its requests through gh or glab (see cli.go).
package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
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
	baseURL string
	prefix  string
	client  *http.Client
	headers http.Header
	cli     *cliBackend // set for profiles with backend "cli": requests go to gh or glab, not to client
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
	h.Set("User-Agent", "workctl/0.3.18")
	return &Client{baseURL: base, prefix: prefix, client: c, headers: h}, nil
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
	case "api":
		switch auth {
		case "basic":
			client.headers.Set("Authorization", basicAuth(profile.Username, credential))
		case "header":
			client.headers.Set(profile.AuthHeader, credential)
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
func (c *Client) WithAccept(accept string) *Client {
	copied := *c
	copied.headers = c.headers.Clone()
	copied.headers.Set("Accept", accept)
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

// Do sends one request, natively over net/http or, for a cli profile, through gh or glab (see cli.go).
// It returns the response body and its Content-Type.
func (c *Client) Do(ctx context.Context, method, path string, q url.Values, body []byte, contentType string) ([]byte, string, error) {
	target, err := c.relPath(path)
	if err != nil {
		return nil, "", err
	}
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(target, "?") {
			sep = "&"
		}
		target += sep + q.Encode()
	}
	if c.cli != nil {
		return c.cli.do(ctx, method, target, c.headers.Get("Accept"), body, contentType)
	}
	endpoint := c.baseURL + target
	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header = c.headers.Clone()
	if body != nil {
		if contentType == "" {
			contentType = "application/json"
		}
		req.Header.Set("Content-Type", contentType)
	}
	data, hdr, err := httpx.Do(c.client, req, func(status int, url string, body []byte) error {
		return &Error{Method: method, URL: url, Status: status, Body: string(body)}
	})
	return data, hdr.Get("Content-Type"), err
}

// BodyOnly adapts a Do result to callers that need only the body.
func BodyOnly(b []byte, _ string, err error) ([]byte, error) { return b, err }
