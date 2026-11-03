// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"github.com/spf13/cobra"
)

var identityCmd = &cobra.Command{
	Use:     "identity",
	Aliases: []string{"id", "user", "users"},
	Short:   "Look up user identities",
}

var identitySearchCmd = &cobra.Command{
	Use:   "search <name|email>",
	Short: "Search identities by name or email (resolves the id for --reviewer/--creator)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		ids, err := svc.Client.SearchIdentities(c, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(ids)
		}
		rows := make([][]string, len(ids))
		for i, id := range ids {
			rows[i] = []string{id.DisplayName, id.LocalID, id.Account(), id.EntityType}
		}
		return p.Table([]string{"Name", "ID", "Account", "Type"}, rows)
	},
}

func init() {
	identityCmd.AddCommand(identitySearchCmd)
	rootCmd.AddCommand(identityCmd)
}
