// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var linkCmd = &cobra.Command{
	Use:     "link",
	Aliases: []string{"rel", "relation"},
	Short:   "Jira issue links",
}

var linkType string

var linkListCmd = &cobra.Command{
	Use:   "list <issueKey>",
	Short: "List an issue's links (both directions)",
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
			links, err := svc.Client.IssueLinks(c, args[0])
			if err != nil {
				return err
			}
			return p.JSONValue(links)
		}
		flat, err := svc.FlatLinks(c, args[0])
		if err != nil {
			return err
		}
		return p.TraceLinks(flat)
	},
}

var linkTypesCmd = &cobra.Command{
	Use:   "types",
	Short: "List issue link types (with directional phrases)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		ts, err := svc.Client.LinkTypes(c)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(ts)
		}
		rows := make([][]string, len(ts))
		for i, t := range ts {
			rows[i] = []string{t.ID, t.Name, t.Outward, t.Inward}
		}
		return p.Table([]string{"ID", "Name", "Outward", "Inward"}, rows)
	},
}

var linkAddCmd = &cobra.Command{
	Use:   "add <fromIssue> <toIssue> --type <name>",
	Short: "Link two issues (from -[outward phrase]-> to)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		if linkType == "" {
			return fmt.Errorf("--type is required (see `atlassian link types`)")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		if err := svc.Client.CreateLink(c, linkType, args[0], args[1]); err != nil {
			return err
		}
		// Echo the stored relation with the type's directional phrase so a
		// reversed link is visible immediately.
		phrase := ""
		if ts, err := svc.Client.LinkTypes(c); err == nil {
			for _, t := range ts {
				if strings.EqualFold(t.Name, linkType) {
					phrase = t.Outward
					break
				}
			}
		}
		if phrase != "" {
			printer().Msg("linked: %s %s %s", args[0], phrase, args[1])
		} else {
			printer().Msg("linked %s -> %s (%s)", args[0], args[1], linkType)
		}
		return nil
	},
}

var linkGetCmd = &cobra.Command{
	Use:   "get <linkId>",
	Short: "Get one issue link (raw)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		raw, err := svc.Client.Link(c, args[0])
		if err != nil {
			return err
		}
		return printer().Raw(raw)
	},
}

var linkDeleteCmd = &cobra.Command{
	Use:   "delete <linkId>",
	Short: "Delete an issue link (link ids: `atlassian link list <key> --json`)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		if err := svc.Client.DeleteLink(c, args[0]); err != nil {
			return err
		}
		printer().Msg("deleted link %s", args[0])
		return nil
	},
}

var linkRemoteCmd = &cobra.Command{
	Use:   "remote <issueKey>",
	Short: "List an issue's remote (web) links (raw)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		raw, err := svc.Client.RemoteLinks(c, args[0])
		if err != nil {
			return err
		}
		return printer().Raw(raw)
	},
}

func init() {
	linkAddCmd.Flags().StringVar(&linkType, "type", "", "link type name (e.g. Blocks, Relates)")
	linkCmd.AddCommand(linkListCmd, linkTypesCmd, linkAddCmd, linkGetCmd, linkDeleteCmd, linkRemoteCmd)
	rootCmd.AddCommand(linkCmd)
}
