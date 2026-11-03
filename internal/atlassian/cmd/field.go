// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

var fieldCmd = &cobra.Command{
	Use:   "field",
	Short: "Jira field registry (find customfield_* ids)",
}

var fieldSearch string

var fieldListCmd = &cobra.Command{
	Use:   "list [--search text]",
	Short: "List all fields (system + custom)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		fs, err := svc.Client.Fields(c)
		if err != nil {
			return err
		}
		var rows [][]string
		var filtered []any
		for _, f := range fs {
			if fieldSearch != "" && !strings.Contains(strings.ToLower(f.Name+f.ID), strings.ToLower(fieldSearch)) {
				continue
			}
			filtered = append(filtered, f)
			schema, custom := "", ""
			if f.Schema != nil {
				schema = f.Schema.Type
				if f.Schema.Items != "" {
					schema += " of " + f.Schema.Items
				}
			}
			if f.Custom {
				custom = "custom"
			}
			rows = append(rows, []string{f.ID, f.Name, schema, custom})
		}
		if p.JSON {
			return p.JSONValue(filtered)
		}
		return p.Table([]string{"ID", "Name", "Schema", "Custom"}, rows)
	},
}

func init() {
	fieldListCmd.Flags().StringVar(&fieldSearch, "search", "", "filter by substring (name or id)")
	fieldCmd.AddCommand(fieldListCmd)
	rootCmd.AddCommand(fieldCmd)
}
