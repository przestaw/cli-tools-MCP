// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"workctl/internal/atlassian/client"
)

var pageCmd = &cobra.Command{
	Use:   "page",
	Short: "Confluence pages",
}

var (
	pageSpace    string
	pageTitle    string
	pageParent   string
	pageBody     string
	pageBodyFile string
	pageStorage  bool
	pageMessage  string
)

var pageGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a page (storage body; --expand renders it)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		if p.JSON {
			raw, err := svc.Client.PageRaw(c, args[0], "storage")
			if err != nil {
				return err
			}
			return p.Raw(raw)
		}
		pg, err := svc.Client.Page(c, args[0], true)
		if err != nil {
			return err
		}
		return p.Page(pg, svc.Client.WikiURL(pg.Links.WebUI))
	},
}

var pageAttachmentsCmd = &cobra.Command{
	Use:   "attachments <id>",
	Short: "List a page's attachments",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		as, err := svc.Client.PageAttachmentsAll(c, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(as)
		}
		rows := make([][]string, len(as))
		for i, a := range as {
			rows[i] = []string{a.ID, a.Title, fmt.Sprint(a.FileSize), a.MediaType, a.Status}
		}
		return p.Table([]string{"ID", "Filename", "Size", "Type", "Status"}, rows)
	},
}

var pageAttachmentOut string

var pageAttachmentDownloadCmd = &cobra.Command{
	Use:   "attachment-download <pageId> <filename>",
	Short: "Download a page attachment by filename",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		as, err := svc.Client.PageAttachmentsAll(c, args[0])
		if err != nil {
			return err
		}
		var match *client.PageAttachment
		for i := range as {
			if as[i].Title == args[1] {
				match = &as[i]
				break
			}
		}
		if match == nil {
			return fmt.Errorf("attachment %q not found on page %s", args[1], args[0])
		}
		data, _, err := svc.Client.DownloadAttachment(c, svc.Client.PageAttachmentURL(*match))
		if err != nil {
			return err
		}
		out := pageAttachmentOut
		if out == "" {
			out = match.Title
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return err
		}
		printer().Msg("wrote %s (%d bytes)", out, len(data))
		return nil
	},
}

var pageCreateCmd = &cobra.Command{
	Use:   "create --space <key> --title <title> [--body text | --body-file f] [--parent id]",
	Short: "Create a page (plain text converted to storage format; --storage for raw XHTML)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if pageSpace == "" || pageTitle == "" {
			return fmt.Errorf("--space and --title are required")
		}
		body, err := resolvePageBody()
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		sp, err := svc.Client.SpaceByKey(c, pageSpace)
		if err != nil {
			return err
		}
		pg, err := svc.Client.CreatePage(c, sp.ID, pageParent, pageTitle, body)
		if err != nil {
			return err
		}
		printer().Msg("created page %s: %s", pg.ID, pg.Title)
		return nil
	},
}

var pageUpdateCmd = &cobra.Command{
	Use:   "update <id> [--title t] [--body text | --body-file f] [--message m]",
	Short: "Update a page (bumps the version; empty body keeps current content)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body, err := resolvePageBody()
		if err != nil {
			return err
		}
		if pageTitle == "" && body == "" {
			return fmt.Errorf("nothing to update (use --title/--body/--body-file)")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		pg, err := svc.Client.UpdatePage(c, args[0], pageTitle, body, pageMessage)
		if err != nil {
			return err
		}
		version := 0
		if pg.Version != nil {
			version = pg.Version.Number
		}
		printer().Msg("updated page %s to version %d", pg.ID, version)
		return nil
	},
}

var pageChildrenCmd = &cobra.Command{
	Use:   "children <id>",
	Short: "List a page's direct children",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		pages, cursor, err := svc.Client.PageChildren(c, args[0], "", flagPageSize)
		if err != nil {
			return err
		}
		if flagAll {
			for cursor != "" {
				more, next, err := svc.Client.PageChildren(c, args[0], cursor, flagPageSize)
				if err != nil {
					return err
				}
				pages = append(pages, more...)
				if next == "" || len(more) == 0 {
					break
				}
				cursor = next
			}
		}
		if p.JSON {
			return p.JSONValue(pages)
		}
		return p.PageTable(pages)
	},
}

var pageCommentsCmd = &cobra.Command{
	Use:   "comments <id>",
	Short: "List a page's footer comments",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		comments, _, err := svc.Client.PageFooterComments(c, args[0], "", flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(comments)
		}
		rows := make([][]string, len(comments))
		for i, cm := range comments {
			body, created := "", ""
			if cm.Body != nil && cm.Body.Storage != nil {
				body = p.StorageToText(cm.Body.Storage.Value)
			}
			if cm.Version != nil {
				created = cm.Version.CreatedAt
			}
			rows[i] = []string{cm.ID, created, body}
		}
		return p.Table([]string{"ID", "Created", "Body"}, rows)
	},
}

var pageCommentAddCmd = &cobra.Command{
	Use:   "comment-add <id> <text>",
	Short: "Add a footer comment to a page",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		cm, err := svc.Client.AddFooterComment(c, args[0], client.TextToStorage(args[1]))
		if err != nil {
			return err
		}
		printer().Msg("added comment %s", cm.ID)
		return nil
	},
}

var pageInlineCommentsCmd = &cobra.Command{
	Use:   "inline-comments <id>",
	Short: "List a page's inline comments (with anchor text and resolution state)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		comments, _, err := svc.Client.PageInlineComments(c, args[0], "", flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(comments)
		}
		rows := make([][]string, len(comments))
		for i, cm := range comments {
			body, anchor := "", ""
			if cm.Body != nil && cm.Body.Storage != nil {
				body = p.StorageToText(cm.Body.Storage.Value)
			}
			if cm.Properties != nil {
				anchor = cm.Properties.TextSelection
			}
			rows[i] = []string{cm.ID, cm.ResolutionStatus, anchor, body}
		}
		return p.Table([]string{"ID", "Resolution", "Anchor", "Body"}, rows)
	},
}

var (
	inlineAnchor     string
	inlineMatchIndex int
	inlineMatchCount int
)

var pageInlineCommentAddCmd = &cobra.Command{
	Use:   "inline-comment-add <id> <text> --anchor <selectedText>",
	Short: "Add an inline comment anchored to text on the page",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if inlineAnchor == "" {
			return fmt.Errorf("--anchor is required (text on the page to attach the comment to)")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		cm, err := svc.Client.AddInlineComment(c, args[0], client.TextToStorage(args[1]),
			inlineAnchor, inlineMatchIndex, inlineMatchCount)
		if err != nil {
			return err
		}
		printer().Msg("added inline comment %s", cm.ID)
		return nil
	},
}

var pageCommentChildrenCmd = &cobra.Command{
	Use:   "comment-children <commentId>",
	Short: "List the replies of a footer comment",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		comments, _, err := svc.Client.FooterCommentChildren(c, args[0], "", flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(comments)
		}
		rows := make([][]string, len(comments))
		for i, cm := range comments {
			body, created := "", ""
			if cm.Body != nil && cm.Body.Storage != nil {
				body = p.StorageToText(cm.Body.Storage.Value)
			}
			if cm.Version != nil {
				created = cm.Version.CreatedAt
			}
			rows[i] = []string{cm.ID, created, body}
		}
		return p.Table([]string{"ID", "Created", "Body"}, rows)
	},
}

var descendantsDepth int

var pageDescendantsCmd = &cobra.Command{
	Use:   "descendants <id> [--depth N]",
	Short: "Recursive content tree below a page or folder (folders first)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		// Accept folder ids too: probe page first, fall back to folder.
		isFolder := false
		if _, err := svc.Client.Page(c, args[0], false); err != nil {
			if !client.IsNotFound(err) {
				return err
			}
			if _, ferr := svc.Client.FolderRaw(c, args[0]); ferr != nil {
				return err // original page error: id is neither page nor folder
			}
			isFolder = true
		}
		nodes, err := svc.WikiDescendants(c, args[0], isFolder, descendantsDepth)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(nodes)
		}
		for _, n := range nodes {
			fmt.Fprintf(cmd.OutOrStdout(), "%s%s %s  %s\n",
				strings.Repeat("  ", n.Depth-1), n.Type, n.ID, n.Title)
		}
		if !flagQuiet {
			p.Msg("%d nodes (depth <= %d)", len(nodes), descendantsDepth)
		}
		return nil
	},
}

var pageOpenCmd = &cobra.Command{
	Use:   "open <id>",
	Short: "Open a page in the browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		pg, err := svc.Client.Page(c, args[0], false)
		if err != nil {
			return err
		}
		return openBrowser(svc.Client.WikiURL(pg.Links.WebUI))
	},
}

// resolvePageBody assembles the storage body from --body/--body-file,
// converting plain text unless --storage says it already is storage XHTML.
func resolvePageBody() (string, error) {
	text := pageBody
	if pageBodyFile != "" {
		data, err := os.ReadFile(pageBodyFile)
		if err != nil {
			return "", err
		}
		text = string(data)
	}
	if text == "" {
		return "", nil
	}
	if pageStorage {
		return text, nil
	}
	return client.TextToStorage(text), nil
}

func init() {
	for _, c := range []*cobra.Command{pageCreateCmd, pageUpdateCmd} {
		c.Flags().StringVar(&pageTitle, "title", "", "page title")
		c.Flags().StringVar(&pageBody, "body", "", "body text")
		c.Flags().StringVar(&pageBodyFile, "body-file", "", "read body from file")
		c.Flags().BoolVar(&pageStorage, "storage", false, "body is already storage-format XHTML")
	}
	pageCreateCmd.Flags().StringVar(&pageSpace, "space", "", "space key (required)")
	pageCreateCmd.Flags().StringVar(&pageParent, "parent", "", "parent page id")
	pageUpdateCmd.Flags().StringVar(&pageMessage, "message", "", "version message")
	pageInlineCommentAddCmd.Flags().StringVar(&inlineAnchor, "anchor", "", "page text to anchor the comment to (required)")
	pageInlineCommentAddCmd.Flags().IntVar(&inlineMatchIndex, "match-index", 0, "which occurrence of the anchor text (0-based)")
	pageInlineCommentAddCmd.Flags().IntVar(&inlineMatchCount, "match-count", 1, "total occurrences of the anchor text on the page")
	pageDescendantsCmd.Flags().IntVar(&descendantsDepth, "depth", 3, "maximum tree depth")
	addPagingFlags(pageChildrenCmd, 50)
	addPagingFlags(pageCommentsCmd, 50)
	addPagingFlags(pageInlineCommentsCmd, 50)
	addPagingFlags(pageCommentChildrenCmd, 50)

	pageAttachmentDownloadCmd.Flags().StringVarP(&pageAttachmentOut, "output", "o", "", "output path (default: original filename)")
	pageCmd.AddCommand(pageGetCmd, pageCreateCmd, pageUpdateCmd, pageChildrenCmd,
		pageCommentsCmd, pageCommentAddCmd, pageInlineCommentsCmd, pageInlineCommentAddCmd,
		pageCommentChildrenCmd, pageDescendantsCmd, pageOpenCmd,
		pageAttachmentsCmd, pageAttachmentDownloadCmd)
	rootCmd.AddCommand(pageCmd)
}
