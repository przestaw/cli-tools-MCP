// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package generic

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"hash"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"workctl/internal/api"
)

// unsafeNameRe matches the characters a downloaded file name may not keep.
var unsafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// downloaded is what `api download` and api_download report: where the file is, plus the stream metadata.
type downloaded struct {
	Path        string         `json:"path"`
	Drive       *api.DriveFile `json:"drive,omitempty"`
	MD5Verified *bool          `json:"md5_verified,omitempty"`
	api.Streamed
}

// downloadCommand is `workctl api download [--query KEY=VALUE]... [--output FILE] [--name NAME] PATH`.
func downloadCommand(ctx context.Context, c *api.Client, args []string) error {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	query := api.QueryFlag(fs)
	output := fs.String("output", "", "file to write (default: a new file in the workctl downloads directory)")
	name := fs.String("name", "", "file name in the downloads directory (default: the server's or the path's)")
	exportMIME := fs.String("export-mime", "", "Drive native document export MIME type; required for native documents")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: download [--query KEY=VALUE]... [--output FILE] [--name NAME] PATH")
	}
	out, err := download(ctx, c, fs.Arg(0), query, *output, *name, *exportMIME)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(out, '\n'))
	return err
}

// download streams path into output, or, when output is empty, into a new file in the downloads directory, and
// returns the result as JSON. An existing output file is replaced. Files in the downloads directory are never
// overwritten: each gets a unique prefix and its final name only after the copy is complete. A failed download
// leaves no file behind, not even the output file it replaced.
func download(ctx context.Context, c *api.Client, urlPath string, q url.Values, output, name, exportMIME string) ([]byte, error) {
	drive, urlPath, q, err := resolveDrive(ctx, c, urlPath, q, exportMIME)
	if err != nil {
		return nil, err
	}
	if drive != nil && name == "" {
		name = drive.Name
	}
	f, err := createTarget(output)
	if err != nil {
		return nil, err
	}
	var dst io.Writer = f
	var sum hash.Hash
	if md5Needed(drive, exportMIME) {
		sum = md5.New()
		dst = io.MultiWriter(f, sum)
	}
	meta, err := c.Stream(ctx, urlPath, q, dst)
	var verified *bool
	if err == nil && sum != nil {
		matches := strings.EqualFold(drive.MD5, hex.EncodeToString(sum.Sum(nil)))
		verified = &matches
		if !matches {
			err = fmt.Errorf("checksum mismatch against Drive MD5; downloaded file removed")
		}
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(f.Name())
		return nil, err
	}
	final := f.Name()
	if output == "" {
		final = strings.TrimSuffix(final, ".part") + "-" + safeName(name, meta.ServerFileName, urlPath)
		if err := os.Rename(f.Name(), final); err != nil {
			_ = os.Remove(f.Name())
			return nil, err
		}
	}
	return json.Marshal(downloaded{Path: final, Streamed: meta, Drive: drive, MD5Verified: verified})
}

// md5Needed reports whether the bytes are the original Drive file and Drive published a sum to compare.
func md5Needed(drive *api.DriveFile, exportMIME string) bool {
	return drive != nil && drive.MD5 != "" && exportMIME == ""
}

// resolveDrive turns a Drive file ID or file URL into the metadata and the media or export request to stream.
// Any other path is returned unchanged, and export_mime is refused for it.
func resolveDrive(ctx context.Context, c *api.Client, urlPath string, q url.Values, exportMIME string) (*api.DriveFile, string, url.Values, error) {
	id, ok := c.DriveFileID(urlPath)
	if !ok {
		if exportMIME != "" {
			return nil, "", nil, fmt.Errorf("export_mime requires a Drive file id")
		}
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

// createTarget opens output (replacing it), or creates a temporary file in the downloads directory.
func createTarget(output string) (*os.File, error) {
	if output != "" {
		return os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	}
	dir, err := DownloadsDir()
	if err != nil {
		return nil, err
	}
	return os.CreateTemp(dir, "download-*.part")
}

// DownloadsDir is where downloads without --output land: <user cache dir>/workctl/downloads, private to the user.
func DownloadsDir() (string, error) {
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cache, "workctl", "downloads")
	return dir, os.MkdirAll(dir, 0o700)
}

// safeName picks the first non-empty candidate and reduces it to a plain file name: no directories, no characters
// that a shell or another system could read specially.
func safeName(candidates ...string) string {
	for _, candidate := range candidates {
		candidate, _, _ = strings.Cut(candidate, "?")
		name := strings.Trim(unsafeNameRe.ReplaceAllString(path.Base(strings.ReplaceAll(candidate, `\`, "/")), "_"), "._")
		if name != "" {
			return name
		}
	}
	return "file"
}
