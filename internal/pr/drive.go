package pr

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

func driveFile(ctx context.Context, cfg *config.Config, name string, p config.Profile, a map[string]any) (out []byte, resultErr error) {
	if mcp.ArgString(a, "repo") != "" || mcp.ArgString(a, "ref") != "" {
		return nil, fmt.Errorf("repo and ref do not apply to Drive")
	}
	c, err := api.NewProfileClient(cfg, name, p)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
	ctx, cancel := context.WithTimeout(ctx, cfg.ProfileTimeout(p))
	defer cancel()
	id := mcp.ArgString(a, "path")
	folder := cliutil.FirstNonEmpty(mcp.ArgString(a, "folder"), p.DefaultProject, "root")
	if id != "" {
		meta, err := c.DriveMetadata(ctx, id)
		if err != nil {
			return nil, err
		}
		if meta.MIMEType != api.DriveFolderMIME {
			if mcp.ArgString(a, "query") != "" || mcp.ArgString(a, "page_token") != "" || mcp.ArgString(a, "folder") != "" {
				return nil, fmt.Errorf("query, folder and page_token apply to a Drive listing, not file metadata")
			}
			return json.Marshal(meta)
		}
		folder = id
	}
	listing, err := c.DriveList(ctx, folder, mcp.ArgString(a, "query"), mcp.ArgString(a, "page_token"), mcp.ArgInt(a, "limit", 30))
	if err != nil {
		return nil, err
	}
	return json.Marshal(listing)
}

// FileCommand shares repository and Drive file operations with the MCP tool.
func FileCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("file", flag.ContinueOnError)
	profile := fs.String("profile", cfg.ProfileName(), "profile: repository or Google Drive")
	repo := fs.String("repo", "", "repository owner/name or group/project")
	ref := fs.String("ref", "", "repository branch, tag or commit")
	folder := fs.String("folder", "", "Drive folder id (default: profile folder or root)")
	query := fs.String("query", "", "Drive file name contains this text")
	page := fs.String("page-token", "", "Drive nextPageToken from the previous listing")
	limit := fs.Int("limit", 30, "Drive page size, 1..1000")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return fmt.Errorf("usage: file [flags] [repository path or Drive id]")
	}
	a := map[string]any{"profile": *profile, "repo": *repo, "ref": *ref, "folder": *folder, "query": *query, "page_token": *page, "path": fs.Arg(0)}
	if *limit != 30 {
		a["limit"] = *limit
	}
	out, err := CallTool(context.Background(), cfg, "file", a)
	if err != nil {
		return err
	}
	return cliutil.PrintBody(out)
}
