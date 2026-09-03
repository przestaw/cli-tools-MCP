package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type APIError struct {
	Method string
	URL    string
	Status int
	Body   string
}

func (e *APIError) Error() string {
	b := strings.TrimSpace(e.Body)
	if len(b) > 1200 {
		b = b[:1200] + "..."
	}
	return fmt.Sprintf("%s %s: HTTP %d: %s", e.Method, e.URL, e.Status, b)
}

type APIClient struct {
	baseURL string
	prefix  string
	client  *http.Client
	headers http.Header
}

func newAPIClient(cfg EndpointConfig, prefix string, timeout time.Duration) (*APIClient, error) {
	base := normalizeBaseURL(cfg.URL)
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q", cfg.URL)
	}
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
	if !cfg.VerifyTLS {
		tlsCfg.InsecureSkipVerify = true // explicitly requested by config
	}
	if cfg.CAFile != "" {
		pem, err := os.ReadFile(os.ExpandEnv(cfg.CAFile))
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA file %s contains no usable PEM certificates", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	tr := &http.Transport{TLSClientConfig: tlsCfg, Proxy: http.ProxyFromEnvironment}
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
	h.Set("User-Agent", "workctl/0.1.5")
	return &APIClient{baseURL: base, prefix: prefix, client: c, headers: h}, nil
}

func newJiraClient(cfg *Config) (*APIClient, error) {
	if !cfg.hasJira() {
		return nil, fmt.Errorf("jira is not configured: set jira.url (run `workctl jira init`)")
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	c, err := newAPIClient(cfg.Jira, "/rest/api/2", time.Duration(cfg.TimeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	sec, err := resolveSecret("jira", cfg.Jira)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(cfg.Jira.Auth) {
	case "bearer":
		c.headers.Set("Authorization", "Bearer "+sec)
	case "basic":
		if cfg.Jira.Username == "" {
			return nil, fmt.Errorf("jira.username is required for basic auth")
		}
		raw := cfg.Jira.Username + ":" + sec
		c.headers.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(raw)))
	}
	return c, nil
}

func newGitLabClient(cfg *Config) (*APIClient, error) {
	if !cfg.hasGitLab() {
		return nil, fmt.Errorf("gitlab is not configured: set gitlab.url (run `workctl gitlab init`)")
	}
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	c, err := newAPIClient(cfg.GitLab.EndpointConfig, "/api/v4", time.Duration(cfg.TimeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	sec, err := resolveSecret("gitlab", cfg.GitLab.EndpointConfig)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(cfg.GitLab.Auth) {
	case "private-token":
		c.headers.Set("PRIVATE-TOKEN", sec)
	case "bearer":
		c.headers.Set("Authorization", "Bearer "+sec)
	}
	return c, nil
}

func newGitHubClient(cfg *Config) (*APIClient, error) {
	if !cfg.hasGitHub() {
		return nil, fmt.Errorf("github is not configured: set github.url (run `workctl github init`)")
	}
	c, err := newAPIClient(cfg.GitHub.EndpointConfig, "", time.Duration(cfg.TimeoutSeconds)*time.Second)
	if err != nil {
		return nil, err
	}
	sec, err := resolveSecret("github", cfg.GitHub.EndpointConfig)
	if err != nil {
		return nil, err
	}
	c.headers.Set("Authorization", "Bearer "+sec)
	c.headers.Set("Accept", "application/vnd.github+json")
	c.headers.Set("X-GitHub-Api-Version", "2022-11-28")
	return c, nil
}

func (c *APIClient) endpoint(path string) (string, error) {
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
	return c.baseURL + path, nil
}

func (c *APIClient) do(ctx context.Context, method, path string, q url.Values, body []byte, contentType string) ([]byte, string, error) {
	endpoint, err := c.endpoint(path)
	if err != nil {
		return nil, "", err
	}
	if len(q) > 0 {
		sep := "?"
		if strings.Contains(endpoint, "?") {
			sep = "&"
		}
		endpoint += sep + q.Encode()
	}
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, r)
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
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, resp.Header.Get("Content-Type"), &APIError{Method: method, URL: endpoint, Status: resp.StatusCode, Body: string(b)}
	}
	return b, resp.Header.Get("Content-Type"), nil
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
