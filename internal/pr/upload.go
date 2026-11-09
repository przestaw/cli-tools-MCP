// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package pr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/issue"
	"workctl/internal/mcp"
)

// uploadTypes are the media upload accepts: screenshots and screen recordings for a description or a comment.
var uploadTypes = map[string]string{
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp",
	".mp4": "video/mp4", ".mov": "video/quicktime", ".webm": "video/webm",
}

// maxUploadBytes is GitHub's limit for a video attachment; images there stop at 10 MB.
const maxUploadBytes = 100 << 20

// githubUploads maps a GitHub API URL to its attachment host. The upload endpoint is not part of GitHub's documented
// API (the web UI uses it), so it is offered for github.com only.
var githubUploads = map[string]string{"https://api.github.com": "https://uploads.github.com"}

func uploadTool(repo, profile map[string]any) mcp.Tool {
	return mcp.Tool{Name: "upload", Description: "Upload one image or video from a local file (png, jpg, gif, webp, mp4, mov, webm; up to 100 MB) so it can be shown in a pull request, merge request or issue: a GitHub user attachment (github.com), a GitLab project upload or a Jira issue attachment. Returns url and markdown to paste into a description or comment. Needs policy.allow_write=true.",
		InputSchema: mcp.ObjSchema(map[string]any{"path": mcp.StrProp("Absolute path of the local file"), "repo": repo,
			"issue": mcp.StrProp("Jira issue key, e.g. ABC-123, instead of repo"), "profile": profile}, "path")}
}

// upload sends one local media file to the repository's or the issue's server.
func upload(ctx context.Context, cfg *config.Config, a map[string]any) (out []byte, resultErr error) {
	if err := cfg.RequireWrite(); err != nil {
		return nil, err
	}
	path := mcp.ArgString(a, "path")
	data, ctype, err := readMedia(path)
	if err != nil {
		return nil, err
	}
	name := filepath.Base(path)
	if key := mcp.ArgString(a, "issue"); key != "" {
		pName, profile, err := issue.ResolveProfile(cfg, mcp.ArgString(a, "profile"), key)
		if err != nil {
			return nil, err
		}
		c, err := api.NewProfileClient(cfg, pName, profile)
		if err != nil {
			return nil, err
		}
		defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
		return uploadJira(ctx, c, key, name, ctype, data)
	}
	t, err := resolveRepo(cfg, a)
	if err != nil {
		return nil, err
	}
	profile := cfg.Profiles[t.profile]
	c, err := api.NewProfileClient(cfg, t.profile, profile)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
	if t.kind == "gitlab" {
		return uploadGitLab(ctx, c, t.repo, name, ctype, data)
	}
	host, ok := githubUploads[config.NormalizeBaseURL(profile.URL)]
	if !ok || profile.UsesCLI() {
		return nil, fmt.Errorf("upload to GitHub needs a token profile for github.com (profile %q)", t.profile)
	}
	return uploadGitHub(ctx, c, host, t.repo, name, ctype, data)
}

// readMedia reads a local image or video and refuses anything whose bytes are not one, whatever its name says.
func readMedia(path string) ([]byte, string, error) {
	if !filepath.IsAbs(path) {
		return nil, "", fmt.Errorf("path must be absolute: %q", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	ctype, ok := uploadTypes[ext]
	if !ok {
		return nil, "", fmt.Errorf("upload takes images and videos only (png, jpg, gif, webp, mp4, mov, webm), not %q", ext)
	}
	st, err := os.Stat(path)
	if err != nil {
		return nil, "", err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 || st.Size() > maxUploadBytes {
		return nil, "", fmt.Errorf("%s: not a regular file of 1 byte to 100 MB", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sniffed := http.DetectContentType(data)
	quickTime := ext == ".mov" && len(data) > 8 && string(data[4:8]) == "ftyp" // Go does not sniff QuickTime
	if !strings.HasPrefix(sniffed, "image/") && !strings.HasPrefix(sniffed, "video/") && !quickTime {
		return nil, "", fmt.Errorf("%s does not contain an image or a video (found %s)", path, sniffed)
	}
	return data, ctype, nil
}

func uploadGitHub(ctx context.Context, c *api.Client, host, repo, name, ctype string, data []byte) ([]byte, error) {
	var r struct {
		ID int64 `json:"id"`
	}
	if err := doJSON(ctx, c, http.MethodGet, "repos/"+repo, nil, nil, "", &r); err != nil {
		return nil, err
	}
	q := url.Values{"name": {name}, "repository_id": {fmt.Sprint(r.ID)}}
	var asset struct {
		URL string `json:"url"`
	}
	if err := doJSON(ctx, c.WithBaseURL(host).WithAccept("application/json"), http.MethodPost, "/user-attachments/assets", q, data, ctype, &asset); err != nil {
		return nil, err
	}
	md := asset.URL // GitHub shows a player for a bare video link
	if strings.HasPrefix(ctype, "image/") {
		md = fmt.Sprintf("![%s](%s)", name, asset.URL)
	}
	return json.Marshal(map[string]string{"url": asset.URL, "markdown": md})
}

func uploadGitLab(ctx context.Context, c *api.Client, project, name, ctype string, data []byte) ([]byte, error) {
	body, form, err := multipartFile(name, ctype, data)
	if err != nil {
		return nil, err
	}
	var r struct {
		URL      string `json:"url"`
		FullPath string `json:"full_path"`
		Markdown string `json:"markdown"`
	}
	if err := doJSON(ctx, c, http.MethodPost, "projects/"+url.PathEscape(project)+"/uploads", nil, body, form, &r); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"url": cliutil.FirstNonEmpty(r.FullPath, r.URL), "markdown": r.Markdown})
}

func uploadJira(ctx context.Context, c *api.Client, key, name, ctype string, data []byte) ([]byte, error) {
	body, form, err := multipartFile(name, ctype, data)
	if err != nil {
		return nil, err
	}
	var r []struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := doJSON(ctx, c.WithHeader("X-Atlassian-Token", "no-check"), http.MethodPost, "issue/"+url.PathEscape(key)+"/attachments", nil, body, form, &r); err != nil {
		return nil, err
	}
	if len(r) == 0 {
		return nil, fmt.Errorf("jira stored no attachment")
	}
	return json.Marshal(map[string]string{"url": r[0].Content, "markdown": "!" + r[0].Filename + "!"})
}

// multipartFile is a form with the one file field ("file") that GitLab and Jira expect.
func multipartFile(name, ctype string, data []byte) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, name))
	h.Set("Content-Type", ctype)
	part, err := w.CreatePart(h)
	if err == nil {
		_, err = part.Write(data)
	}
	if err == nil {
		err = w.Close()
	}
	return buf.Bytes(), w.FormDataContentType(), err
}

func doJSON(ctx context.Context, c *api.Client, method, path string, q url.Values, body []byte, ctype string, v any) error {
	data, err := api.BodyOnly(c.Do(ctx, method, path, q, body, ctype))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("%s %s: unexpected answer: %w", method, path, err)
	}
	return nil
}
