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
)

// maxErrorBody limits how much of a failed answer is read into the error.
const maxErrorBody = 64 << 10

// Streamed describes what Stream copied: size and SHA-256 of the bytes written, and what the server said about them.
type Streamed struct {
	Bytes          int64  `json:"bytes"`
	SHA256         string `json:"sha256"`
	ContentType    string `json:"content_type,omitempty"`
	ServerFileName string `json:"server_file_name,omitempty"` // from Content-Disposition, not trusted as a path
}

// Stream sends a GET for path and copies the answer to w unchanged. A non-2xx answer is returned as *Error and
// nothing is written. The caller owns w: Stream does not close it or clean up after a failed copy.
// Not available for cli profiles: gh and glab buffer their output.
func (c *Client) Stream(ctx context.Context, path string, q url.Values, w io.Writer) (Streamed, error) {
	target, err := c.target(http.MethodGet, path, q)
	if err != nil {
		return Streamed{}, err
	}
	if c.cli != nil {
		return Streamed{}, fmt.Errorf("downloads are not available for profiles with backend cli")
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
	resp, err := c.client.Do(req)
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
	return Streamed{Bytes: n, SHA256: hex.EncodeToString(hash.Sum(nil)), ContentType: resp.Header.Get("Content-Type"), ServerFileName: params["filename"]}, nil
}
