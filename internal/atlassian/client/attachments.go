// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"io"
	"net/url"
)

// Attachments lists an issue's attachments (read from the attachment field).
func (c *Client) Attachments(ctx context.Context, key string) ([]Attachment, error) {
	var out struct {
		Fields struct {
			Attachment []Attachment `json:"attachment"`
		} `json:"fields"`
	}
	p := "/rest/api/3/issue/" + url.PathEscape(key) + "?fields=attachment"
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, err
	}
	return out.Fields.Attachment, nil
}

// UploadAttachment uploads one file to an issue.
func (c *Client) UploadAttachment(ctx context.Context, key, fileName string, content io.Reader) ([]Attachment, error) {
	var out []Attachment
	p := "/rest/api/3/issue/" + url.PathEscape(key) + "/attachments"
	if err := c.doMultipart(ctx, p, "file", fileName, content, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AttachmentMeta fetches a single attachment's metadata by attachment id.
func (c *Client) AttachmentMeta(ctx context.Context, id string) (*Attachment, error) {
	var out Attachment
	if err := c.do(ctx, "GET", "/rest/api/3/attachment/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DownloadAttachment downloads an attachment's bytes via its content URL.
func (c *Client) DownloadAttachment(ctx context.Context, contentURL string) ([]byte, string, error) {
	return c.doRawURL(ctx, "GET", contentURL)
}

// DeleteAttachment deletes an attachment by id.
func (c *Client) DeleteAttachment(ctx context.Context, id string) error {
	return c.do(ctx, "DELETE", "/rest/api/3/attachment/"+url.PathEscape(id), nil, nil)
}
