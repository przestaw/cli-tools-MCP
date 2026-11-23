// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package google holds the Google Drive specifics of the generic upload and download commands.
package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
)

// Uploaded is what Drive reports about a finished upload.
type Uploaded struct {
	ID          string
	Name        string
	MIMEType    string
	WebViewLink string
	ColabURL    string
}

// Upload streams body as name into folderID (defaults to profile.default_project, then My Drive) with a resumable
// upload, replacing a file of the same name only when overwrite is set.
func Upload(ctx context.Context, c *api.Client, profile config.Profile, body io.Reader, size int64, name, ctype, folderID string, overwrite bool) (Uploaded, error) {
	folderID = cliutil.FirstNonEmpty(strings.TrimSpace(folderID), strings.TrimSpace(profile.DefaultProject))
	base := config.NormalizeBaseURL(profile.URL)
	u, err := url.Parse(base)
	if err != nil {
		return Uploaded{}, err
	}
	uploadRoot := u.Scheme + "://" + u.Host + "/upload/drive/v3"

	existingID, err := findFileInFolder(ctx, c, name, folderID)
	if err != nil {
		return Uploaded{}, err
	}
	if existingID != "" && !overwrite {
		return Uploaded{}, fmt.Errorf("%q already exists in the folder (id %s): rename the upload or pass overwrite to replace it", name, existingID)
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
		return Uploaded{}, err
	}

	_, hdr, err := initClient.StreamReader(ctx, initMethod, initPath, q, strings.NewReader(string(metaBytes)), int64(len(metaBytes)), "application/json; charset=UTF-8", map[string]string{
		"X-Upload-Content-Type":   ctype,
		"X-Upload-Content-Length": fmt.Sprintf("%d", size),
	})
	if err != nil {
		return Uploaded{}, err
	}
	loc := hdr.Get("Location")
	if loc == "" {
		return Uploaded{}, fmt.Errorf("google drive resumable upload returned no Location header")
	}
	locURL, err := url.Parse(loc)
	if err != nil || locURL.User != nil || locURL.Scheme != u.Scheme || !strings.EqualFold(locURL.Host, u.Host) {
		return Uploaded{}, fmt.Errorf("google drive resumable upload returned unexpected session host")
	}

	sessionClient := c.WithBaseURL(locURL.Scheme + "://" + locURL.Host)
	respBody, _, err := sessionClient.StreamReader(ctx, http.MethodPut, locURL.RequestURI(), nil, body, size, ctype, nil)
	if err != nil {
		return Uploaded{}, err
	}
	var uploaded struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		MimeType    string `json:"mimeType"`
		WebViewLink string `json:"webViewLink"`
	}
	if err := json.Unmarshal(respBody, &uploaded); err != nil {
		return Uploaded{}, fmt.Errorf("decode Drive upload response: %w", err)
	}
	if uploaded.ID == "" {
		return Uploaded{}, fmt.Errorf("missing file id in Drive upload response; check the destination before retrying")
	}
	res := Uploaded{
		ID:          uploaded.ID,
		Name:        cliutil.FirstNonEmpty(uploaded.Name, name),
		MIMEType:    cliutil.FirstNonEmpty(uploaded.MimeType, ctype),
		WebViewLink: uploaded.WebViewLink,
	}
	if strings.HasSuffix(strings.ToLower(name), ".ipynb") || uploaded.MimeType == "application/vnd.google.colaboratory" {
		res.ColabURL = "https://colab.research.google.com/drive/" + uploaded.ID
	}
	return res, nil
}

func findFileInFolder(ctx context.Context, c *api.Client, name, folderID string) (string, error) {
	if folderID == "" {
		folderID = "root"
	}
	escapedName := strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), `'`, `\'`)
	escapedFolder := strings.ReplaceAll(strings.ReplaceAll(folderID, `\`, `\\`), `'`, `\'`)
	q := url.Values{
		"q":                         {fmt.Sprintf("name = '%s' and '%s' in parents and trashed = false", escapedName, escapedFolder)},
		"fields":                    {"files(id,name),nextPageToken"},
		"pageSize":                  {"2"},
		"includeItemsFromAllDrives": {"true"},
		"supportsAllDrives":         {"true"},
	}
	b, _, err := c.Do(ctx, http.MethodGet, "/files", q, nil, "")
	if err != nil {
		return "", err
	}
	var list struct {
		NextPageToken string `json:"nextPageToken"`
		Files         []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if err := json.Unmarshal(b, &list); err != nil {
		return "", fmt.Errorf("decode Drive file lookup: %w", err)
	}
	if len(list.Files) > 1 || list.NextPageToken != "" {
		return "", fmt.Errorf("multiple Drive files named %q in folder %s; refusing ambiguous overwrite", name, folderID)
	}
	if len(list.Files) > 0 {
		if list.Files[0].ID == "" {
			return "", fmt.Errorf("missing id in Drive file lookup")
		}
		return list.Files[0].ID, nil
	}
	return "", nil
}
