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
	"os"
	"path/filepath"
	"strings"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/google"
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

	var res UploadResult
	if profile.HasDrive() {
		up, err := google.Upload(ctx, c, profile, reader, st.Size(), remoteName, ctype, opt.FolderID, opt.Overwrite)
		if err != nil {
			return nil, err
		}
		res = UploadResult{ID: up.ID, Name: up.Name, ContentType: up.MIMEType, URL: up.WebViewLink, ColabURL: up.ColabURL}
	} else {
		if strings.TrimSpace(opt.Target) == "" {
			return nil, fmt.Errorf("target upload path is required for non-Google-Drive api profiles")
		}
		respBody, _, err := c.StreamReader(ctx, http.MethodPut, opt.Target, nil, reader, st.Size(), ctype, nil)
		if err != nil {
			return nil, err
		}
		res = UploadResult{Name: remoteName, ContentType: ctype, URL: cliutil.JSONField(respBody, "url")}
	}
	res.Bytes = st.Size()
	res.SHA256 = hex.EncodeToString(hasher.Sum(nil))
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
