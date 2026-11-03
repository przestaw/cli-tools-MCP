// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"github.com/spf13/cobra"
	"workctl/internal/atlassian/output"
)

var filterCmd = &cobra.Command{
	Use:   "filter",
	Short: "Jira saved filters",
}

var filterMineCmd = &cobra.Command{
	Use:   "mine",
	Short: "List my filters",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		fs, err := svc.Client.MyFilters(c)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(fs)
		}
		rows := make([][]string, len(fs))
		for i, f := range fs {
			rows[i] = []string{f.ID, f.Name, f.JQL}
		}
		return p.Table([]string{"ID", "Name", "JQL"}, rows)
	},
}

var filterSearchCmd = &cobra.Command{
	Use:   "search <name>",
	Short: "Search filters by name",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		page, err := svc.Client.SearchFilters(c, args[0], (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(page)
		}
		rows := make([][]string, len(page.Values))
		for i, f := range page.Values {
			rows[i] = []string{f.ID, f.Name, output.UserName(f.Owner), f.JQL}
		}
		if err := p.Table([]string{"ID", "Name", "Owner", "JQL"}, rows); err != nil {
			return err
		}
		if !flagQuiet {
			p.Msg("total %d", page.Total)
		}
		return nil
	},
}

var filterGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a filter",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		f, err := svc.Client.Filter(c, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(f)
		}
		return p.Table([]string{"Field", "Value"}, [][]string{
			{"ID", f.ID}, {"Name", f.Name}, {"Owner", output.UserName(f.Owner)}, {"JQL", f.JQL},
		})
	},
}

var filterRunCmd = &cobra.Command{
	Use:   "run <id>",
	Short: "Run a filter's JQL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		f, err := svc.Client.Filter(c, args[0])
		if err != nil {
			return err
		}
		return runJQL(svc, f.JQL)
	},
}

func init() {
	addPagingFlags(filterSearchCmd, 50)
	addPagingFlags(filterRunCmd, 50)
	filterCmd.AddCommand(filterMineCmd, filterSearchCmd, filterGetCmd, filterRunCmd)
	rootCmd.AddCommand(filterCmd)
}
