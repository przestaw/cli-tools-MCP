// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:     "project",
	Aliases: []string{"proj"},
	Short:   "Team projects",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List team projects in the organization",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		ps, err := svc.Client.Projects(c, true)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(ps)
		}
		rows := make([][]string, len(ps))
		for i, pr := range ps {
			rows[i] = []string{pr.Name, pr.ID, pr.State, pr.Visibility}
		}
		return p.Table([]string{"Name", "ID", "State", "Visibility"}, rows)
	},
}

var projectGetCmd = &cobra.Command{
	Use:   "get <name|id>",
	Short: "Get a project (full JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		raw, err := svc.Client.Project(c, args[0])
		if err != nil {
			return err
		}
		return printer().Raw(raw)
	},
}

func init() {
	projectCmd.AddCommand(projectListCmd, projectGetCmd)
	rootCmd.AddCommand(projectCmd)
}
