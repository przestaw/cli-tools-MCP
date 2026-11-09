// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Streaming downloads: the answer of a GET goes straight to a writer (usually a file) as it arrives, so large or
// binary files are neither held in memory nor changed by a byte. The profile's policies apply as in Do.

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
)

// maxErrorBody limits how much of a failed answer is read into the error.
const maxErrorBody = 64 << 10

// Streamed describes what Stream copied: size and SHA-256 of the bytes written, and what the server said about them.
type Streamed struct {
	Bytes          int64  `json:"bytes"`
	SHA256         string `json:"sha256"`
	ContentType    string `json:"content_type,omitempty"`
	ServerFileName string `json:"server_file_name,omitempty"` // from Content-Disposition, not trusted as a path
	RedirectedTo   string `json:"redirected_to,omitempty"`    // host of an allowed storage redirect
	Warning        string `json:"warning,omitempty"`
}

// downloadRedirectHosts are the storage hosts a download may be redirected to (GitHub Actions logs and artifacts,
// release assets, user attachments). The request that follows such a redirect carries no credential.
var downloadRedirectHosts = []string{".githubusercontent.com", ".blob.core.windows.net"}

// allowedRedirectHost is a variable so tests can allow their local storage server.
var allowedRedirectHost = func(host string) bool {
	host = strings.ToLower(host)
	if strings.HasPrefix(host, "github-production-user-asset-") && strings.HasSuffix(host, ".s3.amazonaws.com") {
		return true
	}
	for _, suffix := range downloadRedirectHosts {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	return false
}

// downloadClient follows same-host redirects like the API client, and cross-host ones only to an allowed storage
// host, without any header that could carry a credential.
func (c *Client) downloadClient() *http.Client {
	dc := *c.client
	dc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many redirects")
		}
		if strings.EqualFold(req.URL.Host, via[0].URL.Host) {
			return nil
		}
		if req.URL.Scheme != "https" || !allowedRedirectHost(req.URL.Hostname()) {
			return fmt.Errorf("refusing cross-host redirect from %s to %s", via[0].URL.Host, req.URL.Host)
		}
		clean := http.Header{}
		clean.Set("User-Agent", req.Header.Get("User-Agent"))
		req.Header = clean
		return nil
	}
	return &dc
}

// htmlWarning flags an HTML answer to a download: usually a login, SSO or error page instead of the file.
func htmlWarning(contentType string, wanted string) string {
	if !strings.HasPrefix(strings.ToLower(contentType), "text/html") || strings.HasSuffix(strings.ToLower(wanted), ".html") {
		return ""
	}
	return "the server answered with an HTML page (login, SSO or error page?), not necessarily the file"
}

// Stream sends a GET for path and copies the answer to w unchanged. A non-2xx answer is returned as *Error and
// nothing is written. The caller owns w: Stream does not close it or clean up after a failed copy.
// For cli profiles gh or glab write the answer straight to w; they report no content type.
func (c *Client) Stream(ctx context.Context, path string, q url.Values, w io.Writer) (Streamed, error) {
	target, err := c.target(http.MethodGet, path, q)
	if err != nil {
		return Streamed{}, err
	}
	if c.cli != nil {
		hash, counter := sha256.New(), &countingWriter{}
		if err := c.cli.run(ctx, http.MethodGet, target, "", nil, "", io.MultiWriter(w, hash, counter)); err != nil {
			return Streamed{}, err
		}
		return Streamed{Bytes: counter.n, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
	}
	headers, err := c.authorize(ctx, http.MethodGet)
	if err != nil {
		return Streamed{}, err
	}
	headers.Del("Accept") // a download takes whatever type the file has
	req, err := c.newRequest(ctx, http.MethodGet, target, headers, nil, "")
	if err != nil {
		return Streamed{}, err
	}
	resp, err := c.downloadClient().Do(req)
	if err != nil {
		return Streamed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return Streamed{}, &Error{Method: http.MethodGet, URL: req.URL.String(), Status: resp.StatusCode, Body: string(body)}
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, hash), resp.Body)
	if err != nil {
		return Streamed{}, fmt.Errorf("download: %w", err)
	}
	_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	out := Streamed{Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil)), ContentType: resp.Header.Get("Content-Type"), ServerFileName: params["filename"],
		Warning: htmlWarning(resp.Header.Get("Content-Type"), path)}
	if final := resp.Request.URL; !strings.EqualFold(final.Host, req.URL.Host) {
		out.RedirectedTo = final.Hostname()
	}
	return out, nil
}

type countingWriter struct{ n int64 }

func (w *countingWriter) Write(p []byte) (int, error) {
	w.n += int64(len(p))
	return len(p), nil
}

// StreamStorageURL downloads a signed URL on an allowed storage host (for example the private-user-images link
// GitHub puts into body_html) without any credential: the signature in the URL is the authorisation.
func (c *Client) StreamStorageURL(ctx context.Context, rawURL string, w io.Writer) (Streamed, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !allowedRedirectHost(u.Hostname()) {
		return Streamed{}, fmt.Errorf("url must be https on a known storage host (GitHub user content, Actions or release storage)")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Streamed{}, err
	}
	req.Header.Set("User-Agent", c.headers.Get("User-Agent"))
	dc := c.downloadClient()
	dc.Jar = nil
	resp, err := dc.Do(req)
	if err != nil {
		return Streamed{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Streamed{}, &Error{Method: http.MethodGet, URL: u.Host, Status: resp.StatusCode}
	}
	hash, counter := sha256.New(), &countingWriter{}
	if _, err := io.Copy(io.MultiWriter(w, hash, counter), resp.Body); err != nil {
		return Streamed{}, fmt.Errorf("download: %w", err)
	}
	_, params, _ := mime.ParseMediaType(resp.Header.Get("Content-Disposition"))
	return Streamed{Bytes: counter.n, SHA256: hex.EncodeToString(hash.Sum(nil)), ContentType: resp.Header.Get("Content-Type"),
		ServerFileName: params["filename"], RedirectedTo: u.Hostname(), Warning: htmlWarning(resp.Header.Get("Content-Type"), u.Path)}, nil
}
