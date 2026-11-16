// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package generic

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
)

// maxAPIUploadBytes caps one streaming upload (5 GiB, enough for checkpoints, wheels and dataset snapshots).
const maxAPIUploadBytes = 5 << 30

// UploadResult describes a file uploaded through an api profile.
type UploadResult struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	ContentType string `json:"content_type,omitempty"`
	URL         string `json:"url,omitempty"`
	ColabURL    string `json:"colab_url,omitempty"`
}

// UploadOptions says where and under which name UploadFile stores a file.
type UploadOptions struct {
	FolderID  string // Google Drive parent folder (defaults to profile.default_project)
	Name      string // remote file name (defaults to the local base name)
	Target    string // upload path for non-Drive api profiles
	Overwrite bool   // replace an existing remote file of the same name instead of failing
}

func uploadCommand(ctx context.Context, cfg *config.Config, c *api.Client, args []string) error {
	if err := cfg.RequireWrite(); err != nil {
		return err
	}
	fs := flag.NewFlagSet("upload", flag.ContinueOnError)
	var opt UploadOptions
	fs.StringVar(&opt.FolderID, "folder", "", "parent folder ID (Google Drive; defaults to profile.default_project)")
	fs.StringVar(&opt.Name, "name", "", "remote file name (default: local file base name)")
	fs.StringVar(&opt.Target, "target", "", "API upload path for non-Google-Drive servers (e.g. /artifacts/foo.whl)")
	fs.BoolVar(&opt.Overwrite, "overwrite", false, "replace a remote file of the same name (Google Drive)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: upload [--folder ID] [--name NAME] [--target PATH] [--overwrite] FILE")
	}
	_, profile, err := cfg.ProfileOf(Kind)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	out, err := UploadFile(ctx, c, profile, abs, opt)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(out, '\n'))
	return err
}

// CheckUploadFile validates that path is a readable non-empty regular file within maxAPIUploadBytes.
func CheckUploadFile(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("path must be absolute: %q", path)
	}
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > maxAPIUploadBytes {
		return fmt.Errorf("%s: not a regular file of 1 byte to 5 GiB", path)
	}
	return nil
}

// IsGoogleDriveProfile reports whether profile points at a Google Drive v3 API endpoint.
func IsGoogleDriveProfile(profile config.Profile) bool {
	base := strings.ToLower(config.NormalizeBaseURL(profile.URL))
	return strings.HasSuffix(base, "/drive/v3")
}

// UploadFile streams a local file to a Google Drive v3 profile (resumable upload, creating or updating in folderID)
// or to a generic api profile at targetPath.
func UploadFile(ctx context.Context, c *api.Client, profile config.Profile, localPath string, opt UploadOptions) ([]byte, error) {
	if err := CheckUploadFile(localPath); err != nil {
		return nil, err
	}
	f, err := os.Open(localPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || st.Size() > maxAPIUploadBytes {
		return nil, fmt.Errorf("%s: not a regular file of 1 byte to 5 GiB", localPath)
	}

	remoteName := strings.TrimSpace(opt.Name)
	if remoteName == "" {
		remoteName = filepath.Base(localPath)
	}
	ctype := detectUploadMime(localPath, f)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}

	hasher := sha256.New()
	reader := io.TeeReader(io.LimitReader(f, st.Size()), hasher)

	if IsGoogleDriveProfile(profile) {
		parent := cliutil.FirstNonEmpty(strings.TrimSpace(opt.FolderID), strings.TrimSpace(profile.DefaultProject))
		return uploadGoogleDrive(ctx, c, profile, reader, st.Size(), remoteName, ctype, parent, opt.Overwrite, hasher.Sum)
	}
	if strings.TrimSpace(opt.Target) == "" {
		return nil, fmt.Errorf("target upload path is required for non-Google-Drive api profiles")
	}
	respBody, _, err := c.StreamReader(ctx, http.MethodPut, opt.Target, nil, reader, st.Size(), ctype, nil)
	if err != nil {
		return nil, err
	}
	res := UploadResult{
		Name:        remoteName,
		Bytes:       st.Size(),
		SHA256:      hex.EncodeToString(hasher.Sum(nil)),
		ContentType: ctype,
		URL:         cliutil.JSONField(respBody, "url"),
	}
	return json.Marshal(res)
}

func detectUploadMime(localPath string, f *os.File) string {
	ext := strings.ToLower(filepath.Ext(localPath))
	switch ext {
	case ".ipynb":
		return "application/vnd.google.colaboratory"
	case ".whl", ".zip":
		return "application/zip"
	case ".zst":
		return "application/zstd"
	case ".gz", ".tgz":
		return "application/gzip"
	case ".json":
		return "application/json"
	case ".csv":
		return "text/csv"
	case ".parquet", ".pt", ".pth", ".pkl", ".bin", ".so":
		return "application/octet-stream"
	}
	if byExt := mime.TypeByExtension(ext); byExt != "" {
		return byExt
	}
	var head [512]byte
	n, _ := f.Read(head[:])
	if n > 0 {
		return http.DetectContentType(head[:n])
	}
	return "application/octet-stream"
}

func uploadGoogleDrive(ctx context.Context, c *api.Client, profile config.Profile, body io.Reader, size int64, name, ctype, folderID string, overwrite bool, sumFn func([]byte) []byte) ([]byte, error) {
	base := config.NormalizeBaseURL(profile.URL)
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	uploadRoot := u.Scheme + "://" + u.Host + "/upload/drive/v3"

	existingID, err := findDriveFileInFolder(ctx, c, name, folderID)
	if err != nil {
		return nil, err
	}
	if existingID != "" && !overwrite {
		return nil, fmt.Errorf("%q already exists in the folder (id %s): rename the upload or pass overwrite to replace it", name, existingID)
	}

	initClient := c.WithBaseURL(uploadRoot)
	q := url.Values{
		"uploadType":        {"resumable"},
		"fields":            {"id,name,size,mimeType,webViewLink"},
		"supportsAllDrives": {"true"},
	}
	initMethod := http.MethodPost
	initPath := "/files"
	meta := map[string]any{"name": name}
	if existingID != "" {
		initMethod = http.MethodPatch
		initPath = "/files/" + url.PathEscape(existingID)
	} else if folderID != "" {
		meta["parents"] = []string{folderID}
	}
	metaBytes, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}

	_, hdr, err := initClient.StreamReader(ctx, initMethod, initPath, q, strings.NewReader(string(metaBytes)), int64(len(metaBytes)), "application/json; charset=UTF-8", map[string]string{
		"X-Upload-Content-Type":   ctype,
		"X-Upload-Content-Length": fmt.Sprintf("%d", size),
	})
	if err != nil {
		return nil, err
	}
	loc := hdr.Get("Location")
	if loc == "" {
		return nil, fmt.Errorf("google drive resumable upload returned no Location header")
	}
	locURL, err := url.Parse(loc)
	if err != nil || !strings.EqualFold(locURL.Host, u.Host) {
		return nil, fmt.Errorf("google drive resumable upload returned unexpected session host")
	}

	sessionClient := c.WithBaseURL(locURL.Scheme + "://" + locURL.Host)
	respBody, _, err := sessionClient.StreamReader(ctx, http.MethodPut, locURL.RequestURI(), nil, body, size, ctype, nil)
	if err != nil {
		return nil, err
	}
	var uploaded struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		MimeType    string `json:"mimeType"`
		WebViewLink string `json:"webViewLink"`
	}
	_ = json.Unmarshal(respBody, &uploaded)
	res := UploadResult{
		ID:          uploaded.ID,
		Name:        cliutil.FirstNonEmpty(uploaded.Name, name),
		Bytes:       size,
		SHA256:      hex.EncodeToString(sumFn(nil)),
		ContentType: cliutil.FirstNonEmpty(uploaded.MimeType, ctype),
		URL:         uploaded.WebViewLink,
	}
	if uploaded.ID != "" && (strings.HasSuffix(strings.ToLower(name), ".ipynb") || uploaded.MimeType == "application/vnd.google.colaboratory") {
		res.ColabURL = "https://colab.research.google.com/drive/" + uploaded.ID
	}
	return json.Marshal(res)
}

func findDriveFileInFolder(ctx context.Context, c *api.Client, name, folderID string) (string, error) {
	if folderID == "" {
		return "", nil
	}
	escapedName := strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), `'`, `\'`)
	escapedFolder := strings.ReplaceAll(strings.ReplaceAll(folderID, `\`, `\\`), `'`, `\'`)
	q := url.Values{
		"q":                         {fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", escapedName, escapedFolder)},
		"fields":                    {"files(id,name)"},
		"pageSize":                  {"1"},
		"includeItemsFromAllDrives": {"true"},
		"supportsAllDrives":         {"true"},
	}
	b, _, err := c.Do(ctx, http.MethodGet, "/files", q, nil, "")
	if err != nil {
		return "", err
	}
	var list struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if json.Unmarshal(b, &list) == nil && len(list.Files) > 0 {
		return list.Files[0].ID, nil
	}
	return "", nil
}
