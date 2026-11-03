// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package generic

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	Path string `json:"path"`
	api.Streamed
}

// downloadCommand is `workctl api download [--query KEY=VALUE]... [--output FILE] [--name NAME] PATH`.
func downloadCommand(ctx context.Context, c *api.Client, args []string) error {
	fs := flag.NewFlagSet("download", flag.ContinueOnError)
	query := api.QueryFlag(fs)
	output := fs.String("output", "", "file to write (default: a new file in the workctl downloads directory)")
	name := fs.String("name", "", "file name in the downloads directory (default: the server's or the path's)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: download [--query KEY=VALUE]... [--output FILE] [--name NAME] PATH")
	}
	out, err := download(ctx, c, fs.Arg(0), query, *output, *name)
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
func download(ctx context.Context, c *api.Client, urlPath string, q url.Values, output, name string) ([]byte, error) {
	var f *os.File
	var err error
	if output != "" {
		f, err = os.OpenFile(output, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	} else {
		var dir string
		if dir, err = downloadsDir(); err == nil {
			f, err = os.CreateTemp(dir, "download-*.part")
		}
	}
	if err != nil {
		return nil, err
	}
	meta, err := c.Stream(ctx, urlPath, q, f)
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
	return json.Marshal(downloaded{Path: final, Streamed: meta})
}

// downloadsDir is where downloads without --output land: <user cache dir>/workctl/downloads, private to the user.
func downloadsDir() (string, error) {
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
