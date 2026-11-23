// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package google

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"hash"
	"net/url"
	"strings"

	"workctl/internal/api"
)

// ResolveDownload turns a Drive file ID or file URL into the metadata and the media or export request to stream.
// Any other path comes back unchanged with nil metadata.
func ResolveDownload(ctx context.Context, c *api.Client, urlPath string, q url.Values, exportMIME string) (*api.DriveFile, string, url.Values, error) {
	id, ok := c.DriveFileID(urlPath)
	if !ok {
		return nil, urlPath, q, nil
	}
	parsed, err := url.Parse(urlPath)
	if err != nil {
		return nil, "", nil, err
	}
	embedded, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return nil, "", nil, fmt.Errorf("invalid Drive download query: %w", err)
	}
	for key, values := range q {
		embedded[key] = append(embedded[key], values...)
	}
	for key, values := range embedded {
		if key != "alt" || len(values) != 1 || values[0] != "media" {
			return nil, "", nil, fmt.Errorf("file download manages its Drive query; use export_mime for native documents")
		}
	}
	meta, target, query, err := c.DriveDownload(ctx, id, exportMIME)
	if err != nil {
		return nil, "", nil, err
	}
	return &meta, target, query, nil
}

// MD5Hash returns the hash to feed the downloaded bytes through, or nil when there is nothing to verify: no Drive
// file, no published sum, or an export (Drive publishes the sum of the original bytes only).
func MD5Hash(drive *api.DriveFile, exportMIME string) hash.Hash {
	if drive == nil || drive.MD5 == "" || exportMIME != "" {
		return nil
	}
	return md5.New()
}

// MD5Matches reports whether the downloaded bytes summed in sum are the ones Drive published.
func MD5Matches(drive *api.DriveFile, sum hash.Hash) bool {
	return strings.EqualFold(drive.MD5, hex.EncodeToString(sum.Sum(nil)))
}
