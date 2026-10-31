package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"workctl/internal/httpx"
)

type APIError struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return httpx.RawError(e.Method, e.URL, e.Status, e.Body)
}

type APIClient struct {
	baseURL string
	prefix  string
	client  *http.Client
	headers http.Header
	cli     *cliBackend // set for profiles with backend "cli": requests go to gh or glab, not to client
}

func newAPIClient(cfg Profile, prefix string, timeout time.Duration) (*APIClient, error) {
	base := normalizeBaseURL(cfg.URL)
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q", cfg.URL)
	}
	tr, err := httpx.Transport(cfg.VerifyTLS, cfg.CAFile)
	if err != nil {
		return nil, err
	}
	tr.Proxy = http.ProxyFromEnvironment
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
	h.Set("User-Agent", "workctl/0.3.9")
	return &APIClient{baseURL: base, prefix: prefix, client: c, headers: h}, nil
}

// newKindClient connects to the default profile of a kind (jira, gitlab, github).
func newKindClient(cfg *Config, kind string) (*APIClient, error) {
	name, profile, err := cfg.profileOf(kind)
	if err != nil {
		return nil, err
	}
	return newProfileClient(cfg, name, profile)
}

func profileTimeout(cfg *Config, profile Profile) time.Duration {
	seconds := profile.TimeoutSeconds
	if seconds <= 0 {
		seconds = cfg.TimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func newProfileClient(cfg *Config, name string, profile Profile) (*APIClient, error) {
	if err := validateProfile(name, profile); err != nil {
		return nil, err
	}
	if profile.usesCLI() {
		return newCLIClient(profile, profileTimeout(cfg, profile))
	}
	client, err := newAPIClient(profile, "", profileTimeout(cfg, profile))
	if err != nil {
		return nil, err
	}
	credential, err := resolveProfileSecret(name, profile)
	if err != nil {
		return nil, err
	}
	auth := strings.ToLower(profile.Auth)
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
			client.headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(profile.Username+":"+credential)))
		} else {
			client.headers.Set("Authorization", "Bearer "+credential)
		}
		client.prefix = "/rest/api/2"
	default:
		return nil, fmt.Errorf("profile %q has unsupported HTTP provider %q", name, profile.Kind)
	}
	return client, nil
}

// relPath turns a caller's path into the server-relative path of a request: a path that starts with "/" is used as it
// is, any other one gets the provider prefix. Absolute URLs are refused so a credential cannot leave the profile's host.
func (c *APIClient) relPath(path string) (string, error) {
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

func (c *APIClient) endpoint(path string) (string, error) {
	rel, err := c.relPath(path)
	return c.baseURL + rel, err
}

// do sends one request, natively over net/http or, for a cli profile, through gh or glab (see cli_backend.go).
func (c *APIClient) do(ctx context.Context, method, path string, q url.Values, body []byte, contentType string) ([]byte, string, error) {
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
		return &APIError{Method: method, URL: url, Status: status, Body: string(body)}
	})
	return data, hdr.Get("Content-Type"), err
}

func prettyJSONBytes(b []byte) []byte {
	if !json.Valid(b) {
		return b
	}
	var out bytes.Buffer
	if err := json.Indent(&out, b, "", "  "); err != nil {
		return b
	}
	out.WriteByte('\n')
	return out.Bytes()
}

func printBody(b []byte) error {
	if json.Valid(b) {
		_, err := os.Stdout.Write(prettyJSONBytes(b))
		return err
	}
	if len(b) > 0 {
		if _, err := os.Stdout.Write(b); err != nil {
			return err
		}
		if b[len(b)-1] != '\n' {
			_, _ = os.Stdout.Write([]byte("\n"))
		}
	}
	return nil
}

func readBodyArg(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	if strings.HasPrefix(s, "@") {
		return os.ReadFile(strings.TrimPrefix(s, "@"))
	}
	return []byte(s), nil
}
