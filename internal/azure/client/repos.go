package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Repositories lists the Git repositories in a project.
func (c *Client) Repositories(ctx context.Context, project string) ([]Repository, error) {
	return getList[Repository](ctx, c, func(string) string {
		return c.apiURL(project, "git/repositories", nil)
	}, false)
}

// Repository fetches one repository by name or id (keeps raw JSON in Raw).
func (c *Client) Repository(ctx context.Context, project, repo string) (*Repository, error) {
	var raw json.RawMessage
	if _, err := c.doJSON(ctx, http.MethodGet, c.apiURL(project, "git/repositories/"+url.PathEscape(repo), nil), nil, &raw); err != nil {
		return nil, err
	}
	var out Repository
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode repository %s: %w", repo, err)
	}
	out.Raw = raw
	return &out, nil
}

// Refs lists refs (branches/tags) of a repository. filterType is "heads" or
// "tags" (empty for all).
func (c *Client) Refs(ctx context.Context, project, repo, filterType string) ([]GitRef, error) {
	return getList[GitRef](ctx, c, func(string) string {
		q := url.Values{}
		if filterType != "" {
			q.Set("filter", filterType)
		}
		return c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/refs", q)
	}, false)
}

// Items lists items (files/folders) under a path of a repository at a branch.
// recursion is "none", "oneLevel", or "full".
func (c *Client) Items(ctx context.Context, project, repo, path, branch, recursion string) ([]GitItem, error) {
	return getList[GitItem](ctx, c, func(string) string {
		q := url.Values{}
		q.Set("scopePath", orSlash(path))
		if recursion == "" {
			recursion = "oneLevel"
		}
		q.Set("recursionLevel", recursion)
		if branch != "" {
			q.Set("versionDescriptor.version", strings.TrimPrefix(branch, "refs/heads/"))
			q.Set("versionDescriptor.versionType", "branch")
		}
		return c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/items", q)
	}, false)
}

// FileContent returns the raw bytes of a file in a repository at a branch.
func (c *Client) FileContent(ctx context.Context, project, repo, path, branch string) ([]byte, error) {
	data, _, err := c.itemContent(ctx, project, repo, path, strings.TrimPrefix(branch, "refs/heads/"), "branch")
	return data, err
}

// FileContentAtCommit returns a file's bytes at a specific commit. A file that
// does not exist at that commit (e.g. it was added or deleted) yields nil
// content and no error, so callers can diff against an empty side.
func (c *Client) FileContentAtCommit(ctx context.Context, project, repo, path, commitID string) ([]byte, error) {
	data, missing, err := c.itemContent(ctx, project, repo, path, commitID, "commit")
	if missing {
		return nil, nil
	}
	return data, err
}

// itemContent fetches file bytes at a version (versionType "branch"|"commit"|"tag").
// It reports missing==true for a 404 so callers can treat the file as absent.
func (c *Client) itemContent(ctx context.Context, project, repo, path, version, versionType string) (data []byte, missing bool, err error) {
	q := url.Values{}
	q.Set("path", orSlash(path))
	q.Set("$format", "text")
	q.Set("includeContent", "true")
	if version != "" {
		q.Set("versionDescriptor.version", version)
		q.Set("versionDescriptor.versionType", versionType)
	}
	data, _, err = c.doText(ctx, c.apiURL(project, "git/repositories/"+url.PathEscape(repo)+"/items", q))
	if err != nil {
		if ae, ok := err.(*APIError); ok && ae.Status == http.StatusNotFound {
			return nil, true, nil
		}
		return nil, false, err
	}
	return data, false, nil
}

// RepoWebURL builds a browser URL for a repository.
func (c *Client) RepoWebURL(project, repo string) string {
	return fmt.Sprintf("%s/%s/_git/%s", c.orgURL, url.PathEscape(project), url.PathEscape(repo))
}

func orSlash(p string) string {
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}
