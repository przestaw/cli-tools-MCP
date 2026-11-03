// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"os"

	"github.com/spf13/cobra"
	"workctl/internal/atlassian/client"
	"workctl/internal/atlassian/output"
)

var commentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Jira issue comments",
}

var commentFile string
var commentMarkdown bool

var commentListCmd = &cobra.Command{
	Use:   "list <issueKey>",
	Short: "List comments",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		page, err := svc.Client.Comments(c, args[0], (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(page)
		}
		rows := make([][]string, len(page.Comments))
		for i, cm := range page.Comments {
			rows[i] = []string{cm.ID, output.UserName(cm.Author), shortTime(cm.Created), client.ADFToText(cm.Body)}
		}
		if err := p.Table([]string{"ID", "Author", "Created", "Body"}, rows); err != nil {
			return err
		}
		if !flagQuiet {
			p.Msg("total %d", page.Total)
		}
		return nil
	},
}

var commentAddCmd = &cobra.Command{
	Use:   "add <issueKey> [text]",
	Short: "Add a comment (plain text converted to ADF; --file for a text file)",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		text := ""
		if len(args) == 2 {
			text = args[1]
		}
		if commentFile != "" {
			data, err := os.ReadFile(commentFile)
			if err != nil {
				return err
			}
			text = string(data)
		}
		if text == "" {
			return cmd.Help()
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		body := client.TextToADF(text)
		if commentMarkdown {
			body = client.MarkdownToADF(text)
		}
		cm, err := svc.Client.AddComment(c, args[0], body)
		if err != nil {
			return err
		}
		printer().Msg("added comment %s", cm.ID)
		return nil
	},
}

var commentGetCmd = &cobra.Command{
	Use:   "get <issueKey> <commentId>",
	Short: "Get one comment",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		cm, err := svc.Client.Comment(c, args[0], args[1])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(cm)
		}
		p.Msg("%s  %s  %s", cm.ID, output.UserName(cm.Author), cm.Created)
		p.Msg("%s", client.ADFToText(cm.Body))
		return nil
	},
}

var commentDeleteCmd = &cobra.Command{
	Use:   "delete <issueKey> <commentId>",
	Short: "Delete a comment",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		if err := svc.Client.DeleteComment(c, args[0], args[1]); err != nil {
			return err
		}
		printer().Msg("deleted comment %s", args[1])
		return nil
	},
}

func init() {
	commentAddCmd.Flags().StringVar(&commentFile, "file", "", "read comment text from a file")
	commentAddCmd.Flags().BoolVar(&commentMarkdown, "markdown", false, "parse the comment as Markdown into ADF")
	addPagingFlags(commentListCmd, 50)
	commentCmd.AddCommand(commentListCmd, commentAddCmd, commentGetCmd, commentDeleteCmd)
	rootCmd.AddCommand(commentCmd)
}
