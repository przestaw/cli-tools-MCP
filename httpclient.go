package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type APIClient struct {
	baseURL string
	client  *http.Client
	headers http.Header
}

func newAPIClient(cfg *Config) (*APIClient, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid URL %q", cfg.URL)
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: !cfg.VerifyTLS, // explicitly requested with --insecure or config
		},
	}
	client := &http.Client{
		Timeout:   time.Duration(cfg.TimeoutSeconds) * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 0 && !strings.EqualFold(req.URL.Host, via[0].URL.Host) {
				return fmt.Errorf("refusing cross-host redirect")
			}
			return nil
		},
	}
	headers := make(http.Header)
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", "workctl/0.1.0")
	if token, err := resolveSecret(cfg); err == nil && token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	return &APIClient{baseURL: cfg.URL, client: client, headers: headers}, nil
}

func (c *APIClient) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return nil, fmt.Errorf("absolute URLs are not allowed")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header = c.headers.Clone()
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s %s: HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return b, nil
}

func printBody(b []byte) error {
	if json.Valid(b) {
		var out bytes.Buffer
		if err := json.Indent(&out, b, "", "  "); err == nil {
			out.WriteByte('\n')
			b = out.Bytes()
		}
	}
	_, err := os.Stdout.Write(b)
	return err
}
