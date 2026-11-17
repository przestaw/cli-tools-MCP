package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// DriveFolderMIME distinguishes a folder from downloadable content.
const DriveFolderMIME = "application/vnd.google-apps.folder"
const driveFileFields = "id,name,mimeType,size,modifiedTime,md5Checksum,webViewLink"

// DriveFile is metadata shared by file listings and downloads.
type DriveFile struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	MIMEType    string     `json:"mimeType"`
	Size        *DriveSize `json:"size,omitempty"`
	Modified    string     `json:"modifiedTime,omitempty"`
	MD5         string     `json:"md5Checksum,omitempty"`
	WebViewLink string     `json:"webViewLink,omitempty"`
}

// DriveSize is a file size: Drive sends it as a string, workctl reports it as a JSON number (files stay far below 2^53).
type DriveSize int64

// UnmarshalJSON accepts the string form of Drive and a plain number.
func (d *DriveSize) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	n, err := strconv.ParseInt(strings.Trim(string(b), `"`), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid Drive size %s", b)
	}
	*d = DriveSize(n)
	return nil
}

// DriveListing retains provider pagination and completeness information.
type DriveListing struct {
	Files         []DriveFile `json:"files"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
	Incomplete    bool        `json:"incompleteSearch,omitempty"`
}

// DriveAbout preserves absent quotas and integer precision.
type DriveAbout struct {
	User struct {
		Name  string `json:"displayName"`
		Email string `json:"emailAddress"`
	} `json:"user"`
	StorageQuota *struct {
		Limit *int64 `json:"limit,string,omitempty"`
		Usage int64  `json:"usage,string"`
	} `json:"storageQuota"`
}

// HasDrive reports whether this client addresses the Drive v3 API.
func (c *Client) HasDrive() bool {
	u, err := url.Parse(c.baseURL)
	return err == nil && strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/drive/v3")
}

var driveID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// DriveFileID recognizes an ID or file endpoint on this client's Drive server.
func (c *Client) DriveFileID(path string) (string, bool) {
	if !c.HasDrive() {
		return "", false
	}
	if driveID.MatchString(path) {
		return path, true
	}
	target, err := c.relPath(path)
	if err != nil {
		return "", false
	}
	u, err := url.Parse(target)
	if err != nil {
		return "", false
	}
	base, _ := url.Parse(c.baseURL)
	if u.IsAbs() && (u.Host != base.Host || u.Scheme != base.Scheme) {
		return "", false
	}
	p := strings.TrimPrefix(u.Path, base.Path)
	id := strings.TrimPrefix(p, "/files/")
	if id == p || !driveID.MatchString(id) {
		return "", false
	}
	return id, true
}

func (c *Client) driveJSON(ctx context.Context, path string, query url.Values, dst any) error {
	if !c.HasDrive() {
		return fmt.Errorf("this profile does not use Drive v3")
	}
	b, _, err := c.Do(ctx, http.MethodGet, path, query, nil, "")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("decode Drive response: %w", err)
	}
	return nil
}

// DriveAbout fetches the current user and storage quota.
func (c *Client) DriveAbout(ctx context.Context) (DriveAbout, error) {
	var out DriveAbout
	err := c.driveJSON(ctx, "/about", url.Values{"fields": {"user(displayName,emailAddress),storageQuota(limit,usage)"}}, &out)
	if err == nil && (out.StorageQuota == nil || (out.User.Name == "" && out.User.Email == "")) {
		err = fmt.Errorf("incomplete Drive about response")
	}
	return out, err
}

// DriveMetadata fetches metadata, never the file's contents.
func (c *Client) DriveMetadata(ctx context.Context, id string) (DriveFile, error) {
	var out DriveFile
	if !driveID.MatchString(id) {
		return out, fmt.Errorf("invalid Drive file id")
	}
	err := c.driveJSON(ctx, "/files/"+id, url.Values{"fields": {driveFileFields}, "supportsAllDrives": {"true"}}, &out)
	if err == nil && (out.ID == "" || out.MIMEType == "") {
		err = fmt.Errorf("incomplete Drive metadata: missing id or mimeType")
	}
	return out, err
}

func driveQuote(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `'`, `\'`)
}

// DriveList lists one page, searching names within a folder.
func (c *Client) DriveList(ctx context.Context, folder, query, pageToken string, limit int) (DriveListing, error) {
	var out DriveListing
	if limit < 1 || limit > 1000 {
		return out, fmt.Errorf("limit must be between 1 and 1000 for Drive")
	}
	if folder == "" {
		folder = "root"
	}
	if !driveID.MatchString(folder) {
		return out, fmt.Errorf("invalid Drive folder id")
	}
	filter := "'" + folder + "' in parents and trashed = false"
	if query != "" {
		filter += " and name contains '" + driveQuote(query) + "'"
	}
	params := url.Values{
		"q": {filter}, "fields": {"files(" + driveFileFields + "),nextPageToken,incompleteSearch"},
		"pageSize":          {strconv.Itoa(limit)},
		"supportsAllDrives": {"true"}, "includeItemsFromAllDrives": {"true"},
	}
	if pageToken != "" {
		params.Set("pageToken", pageToken)
	}
	err := c.driveJSON(ctx, "/files", params, &out)
	if err == nil && out.Files == nil {
		err = fmt.Errorf("incomplete Drive listing: missing files array")
	}
	return out, err
}

// DriveDownload resolves metadata and a media or explicitly selected export endpoint.
func (c *Client) DriveDownload(ctx context.Context, id, exportMIME string) (DriveFile, string, url.Values, error) {
	meta, err := c.DriveMetadata(ctx, id)
	if err != nil {
		return meta, "", nil, err
	}
	if meta.MIMEType == DriveFolderMIME {
		return meta, "", nil, fmt.Errorf("cannot download a Drive folder as a file")
	}
	if strings.HasPrefix(meta.MIMEType, "application/vnd.google-apps.") {
		var about struct {
			Formats map[string][]string `json:"exportFormats"`
		}
		if err := c.driveJSON(ctx, "/about", url.Values{"fields": {"exportFormats"}}, &about); err != nil {
			return meta, "", nil, err
		}
		for _, supported := range about.Formats[meta.MIMEType] {
			if exportMIME == supported {
				return meta, "/files/" + id + "/export", url.Values{"mimeType": {exportMIME}}, nil
			}
		}
		if len(about.Formats[meta.MIMEType]) == 0 {
			return meta, "", nil, fmt.Errorf("%s cannot be exported or downloaded", meta.MIMEType)
		}
		return meta, "", nil, fmt.Errorf("choose export_mime for %s; supported: %s; CSV exports only the first sheet", meta.MIMEType, strings.Join(about.Formats[meta.MIMEType], ", "))
	}
	if exportMIME != "" {
		return meta, "", nil, fmt.Errorf("export_mime applies only to native Google documents")
	}
	return meta, "/files/" + id, url.Values{"alt": {"media"}, "supportsAllDrives": {"true"}}, nil
}
