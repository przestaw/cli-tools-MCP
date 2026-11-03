// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// Spaces lists one cursor page of Confluence spaces. keys filters by space
// key(s). Returns results + the next-page cursor ("" when done).
func (c *Client) Spaces(ctx context.Context, keys []string, cursor string, limit int) ([]Space, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	if len(keys) > 0 {
		q.Set("keys", strings.Join(keys, ","))
	}
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[Space]
	if err := c.do(ctx, "GET", "/wiki/api/v2/spaces?"+q.Encode(), nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// SpacesAll lists every space (optionally filtered by keys).
func (c *Client) SpacesAll(ctx context.Context, keys []string) ([]Space, error) {
	var all []Space
	cursor := ""
	for {
		res, next, err := c.Spaces(ctx, keys, cursor, 100)
		if err != nil {
			return nil, err
		}
		all = append(all, res...)
		if next == "" || len(res) == 0 {
			return all, nil
		}
		cursor = next
	}
}

// SpaceByKey resolves a space key to its Space (v2 needs numeric space ids).
func (c *Client) SpaceByKey(ctx context.Context, key string) (*Space, error) {
	res, _, err := c.Spaces(ctx, []string{key}, "", 1)
	if err != nil {
		return nil, err
	}
	if len(res) == 0 {
		return nil, &APIError{Status: 404, Message: fmt.Sprintf("space %q not found", key)}
	}
	return &res[0], nil
}

// SpaceRaw fetches one space by numeric id.
func (c *Client) SpaceRaw(ctx context.Context, id string) (json.RawMessage, error) {
	return c.Raw(ctx, "GET", "/wiki/api/v2/spaces/"+url.PathEscape(id), nil)
}

// SpacePages lists one cursor page of the pages in a space.
func (c *Client) SpacePages(ctx context.Context, spaceID string, cursor string, limit int) ([]Page, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	q.Set("sort", "-modified-date")
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[Page]
	p := "/wiki/api/v2/spaces/" + url.PathEscape(spaceID) + "/pages?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// PageAttachments lists one cursor page of a page's attachments.
func (c *Client) PageAttachments(ctx context.Context, pageID, cursor string, limit int) ([]PageAttachment, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[PageAttachment]
	p := "/wiki/api/v2/pages/" + url.PathEscape(pageID) + "/attachments?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// PageAttachmentsAll lists every attachment of a page.
func (c *Client) PageAttachmentsAll(ctx context.Context, pageID string) ([]PageAttachment, error) {
	var all []PageAttachment
	cursor := ""
	for {
		res, next, err := c.PageAttachments(ctx, pageID, cursor, 100)
		if err != nil {
			return nil, err
		}
		all = append(all, res...)
		if next == "" || len(res) == 0 {
			return all, nil
		}
		cursor = next
	}
}

// PageAttachmentURL joins an attachment's downloadLink onto the site's /wiki base.
func (c *Client) PageAttachmentURL(a PageAttachment) string {
	if strings.HasPrefix(a.DownloadLink, "http") {
		return a.DownloadLink
	}
	return c.baseURL + "/wiki" + a.DownloadLink
}

// PageRaw fetches a page's raw JSON; bodyFormat "" omits the body.
func (c *Client) PageRaw(ctx context.Context, id, bodyFormat string) (json.RawMessage, error) {
	p := "/wiki/api/v2/pages/" + url.PathEscape(id)
	if bodyFormat != "" {
		p += "?body-format=" + url.QueryEscape(bodyFormat)
	}
	return c.Raw(ctx, "GET", p, nil)
}

// Page fetches and decodes a page (storage body when withBody).
func (c *Client) Page(ctx context.Context, id string, withBody bool) (*Page, error) {
	format := ""
	if withBody {
		format = "storage"
	}
	raw, err := c.PageRaw(ctx, id, format)
	if err != nil {
		return nil, err
	}
	var pg Page
	if err := json.Unmarshal(raw, &pg); err != nil {
		return nil, fmt.Errorf("decode page %s: %w", id, err)
	}
	return &pg, nil
}

// PageChildren lists one cursor page of a page's direct children.
func (c *Client) PageChildren(ctx context.Context, id string, cursor string, limit int) ([]Page, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[Page]
	p := "/wiki/api/v2/pages/" + url.PathEscape(id) + "/children?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// CreatePage creates a page with a storage-format body.
func (c *Client) CreatePage(ctx context.Context, spaceID, parentID, title, storageBody string) (*Page, error) {
	body := map[string]any{
		"spaceId": spaceID,
		"status":  "current",
		"title":   title,
		"body": map[string]any{
			"representation": "storage",
			"value":          storageBody,
		},
	}
	if parentID != "" {
		body["parentId"] = parentID
	}
	var out Page
	if err := c.do(ctx, "POST", "/wiki/api/v2/pages", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePage updates title and/or body, bumping the version number. Empty
// title keeps the current title; empty storageBody keeps the current body.
func (c *Client) UpdatePage(ctx context.Context, id, title, storageBody, versionMessage string) (*Page, error) {
	cur, err := c.Page(ctx, id, storageBody == "")
	if err != nil {
		return nil, err
	}
	if title == "" {
		title = cur.Title
	}
	if storageBody == "" {
		if cur.Body == nil || cur.Body.Storage == nil {
			return nil, fmt.Errorf("page %s has no storage body to keep", id)
		}
		storageBody = cur.Body.Storage.Value
	}
	version := 1
	if cur.Version != nil {
		version = cur.Version.Number + 1
	}
	body := map[string]any{
		"id":     id,
		"status": "current",
		"title":  title,
		"body": map[string]any{
			"representation": "storage",
			"value":          storageBody,
		},
		"version": map[string]any{"number": version, "message": versionMessage},
	}
	var out Page
	if err := c.do(ctx, "PUT", "/wiki/api/v2/pages/"+url.PathEscape(id), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SpaceRootPages lists one cursor page of a space's root-level pages.
// sortParam is a v2 pages sort value ("title", "-modified-date", ...);
// empty means title. Root folders are not included (pages endpoint).
func (c *Client) SpaceRootPages(ctx context.Context, spaceID string, cursor string, limit int, sortParam string) ([]Page, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if sortParam == "" {
		sortParam = "title"
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	q.Set("depth", "root")
	q.Set("sort", sortParam)
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[Page]
	p := "/wiki/api/v2/spaces/" + url.PathEscape(spaceID) + "/pages?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// directChildren lists one cursor page of typed children for a content node.
func (c *Client) directChildren(ctx context.Context, contentType, id, cursor string, limit int) ([]ContentChild, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[ContentChild]
	p := "/wiki/api/v2/" + contentType + "/" + url.PathEscape(id) + "/direct-children?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// PageDirectChildren lists a page's direct children with their content type
// (page, folder, whiteboard, ...).
func (c *Client) PageDirectChildren(ctx context.Context, id, cursor string, limit int) ([]ContentChild, string, error) {
	return c.directChildren(ctx, "pages", id, cursor, limit)
}

// FolderDirectChildren lists a folder's direct children with their content type.
func (c *Client) FolderDirectChildren(ctx context.Context, id, cursor string, limit int) ([]ContentChild, string, error) {
	return c.directChildren(ctx, "folders", id, cursor, limit)
}

// FolderRaw fetches a folder's JSON by id.
func (c *Client) FolderRaw(ctx context.Context, id string) (json.RawMessage, error) {
	return c.Raw(ctx, "GET", "/wiki/api/v2/folders/"+url.PathEscape(id), nil)
}

// folderInfo is the subset of a v2 folder used for root detection and links.
type folderInfo struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	ParentID string `json:"parentId"`
	Links    struct {
		WebUI string `json:"webui"`
	} `json:"_links"`
}

// SpaceRootFolders finds a space's root-level folders. Folders are invisible
// to the v2 pages endpoints, so they are discovered via CQL (type = folder)
// and filtered to parentId == null with concurrent v2 folder lookups.
func (c *Client) SpaceRootFolders(ctx context.Context, spaceKey string, cap int) ([]ContentChild, error) {
	if cap <= 0 {
		cap = 100
	}
	hits, err := c.SearchCQL(ctx, fmt.Sprintf("space = %q and type = folder", spaceKey), 0, cap)
	if err != nil {
		// Instances without the folder content type reject the CQL: no folders.
		return nil, nil
	}
	type slot struct {
		child ContentChild
		ok    bool
	}
	slots := make([]slot, len(hits.Results))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i := range hits.Results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			raw, err := c.FolderRaw(ctx, hits.Results[i].ID)
			if err != nil {
				return
			}
			var f folderInfo
			if json.Unmarshal(raw, &f) != nil {
				return
			}
			if f.ParentID == "" { // root-level folder
				slots[i] = slot{child: ContentChild{ID: f.ID, Title: f.Title, Type: "folder", Status: f.Status}, ok: true}
			}
		}(i)
	}
	wg.Wait()
	var out []ContentChild
	for _, s := range slots {
		if s.ok {
			out = append(out, s.child)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Title < out[j].Title })
	return out, nil
}

// PageFooterComments lists a page's footer comments (storage bodies).
func (c *Client) PageFooterComments(ctx context.Context, pageID string, cursor string, limit int) ([]FooterComment, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	q.Set("body-format", "storage")
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[FooterComment]
	p := "/wiki/api/v2/pages/" + url.PathEscape(pageID) + "/footer-comments?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// InlineComment is a Confluence inline comment (anchored to page text).
type InlineComment struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	Title            string `json:"title"`
	ResolutionStatus string `json:"resolutionStatus"`
	Version          *struct {
		Number    int    `json:"number"`
		CreatedAt string `json:"createdAt"`
		AuthorID  string `json:"authorId"`
	} `json:"version"`
	Body *struct {
		Storage *struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
	Properties *struct {
		TextSelection string `json:"textSelection"`
	} `json:"properties"`
}

// PageInlineComments lists a page's inline comments (storage bodies).
func (c *Client) PageInlineComments(ctx context.Context, pageID string, cursor string, limit int) ([]InlineComment, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	q.Set("body-format", "storage")
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[InlineComment]
	p := "/wiki/api/v2/pages/" + url.PathEscape(pageID) + "/inline-comments?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// AddInlineComment creates an inline comment anchored to selected page text.
// anchor must match text on the page; matchIndex selects which of matchCount
// occurrences is anchored (0-based).
func (c *Client) AddInlineComment(ctx context.Context, pageID, storageBody, anchor string, matchIndex, matchCount int) (*InlineComment, error) {
	if matchCount <= 0 {
		matchCount = 1
	}
	body := map[string]any{
		"pageId": pageID,
		"body": map[string]any{
			"representation": "storage",
			"value":          storageBody,
		},
		"inlineCommentProperties": map[string]any{
			"textSelection":           anchor,
			"textSelectionMatchIndex": matchIndex,
			"textSelectionMatchCount": matchCount,
		},
	}
	var out InlineComment
	if err := c.do(ctx, "POST", "/wiki/api/v2/inline-comments", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// FooterCommentChildren lists the replies of a footer comment.
func (c *Client) FooterCommentChildren(ctx context.Context, commentID string, cursor string, limit int) ([]FooterComment, string, error) {
	if limit <= 0 {
		limit = 50
	}
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	q.Set("body-format", "storage")
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	var out V2Page[FooterComment]
	p := "/wiki/api/v2/footer-comments/" + url.PathEscape(commentID) + "/children?" + q.Encode()
	if err := c.do(ctx, "GET", p, nil, &out); err != nil {
		return nil, "", err
	}
	return out.Results, nextCursor(out.Links.Next), nil
}

// AddFooterComment adds a footer comment to a page (storage-format body).
func (c *Client) AddFooterComment(ctx context.Context, pageID, storageBody string) (*FooterComment, error) {
	body := map[string]any{
		"pageId": pageID,
		"body": map[string]any{
			"representation": "storage",
			"value":          storageBody,
		},
	}
	var out FooterComment
	if err := c.do(ctx, "POST", "/wiki/api/v2/footer-comments", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TextToStorage converts plain text to Confluence storage format (escaped
// XHTML paragraphs; single newlines become <br/>).
func TextToStorage(text string) string {
	esc := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	var b strings.Builder
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r", ""), "\n\n") {
		if strings.TrimSpace(para) == "" {
			continue
		}
		lines := strings.Split(para, "\n")
		for i := range lines {
			lines[i] = esc.Replace(lines[i])
		}
		b.WriteString("<p>" + strings.Join(lines, "<br/>") + "</p>")
	}
	return b.String()
}

// nextCursor extracts the cursor parameter from a v2 _links.next path.
func nextCursor(next string) string {
	if next == "" {
		return ""
	}
	if i := strings.Index(next, "cursor="); i >= 0 {
		cur := next[i+len("cursor="):]
		if j := strings.IndexByte(cur, '&'); j >= 0 {
			cur = cur[:j]
		}
		if dec, err := url.QueryUnescape(cur); err == nil {
			return dec
		}
		return cur
	}
	return ""
}
