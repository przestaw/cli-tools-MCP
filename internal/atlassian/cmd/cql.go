// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"workctl/internal/atlassian/client"
	"workctl/internal/atlassian/service"
)

var cqlCmd = &cobra.Command{
	Use:   "cql <cql>",
	Short: "Run a Confluence CQL search",
	Long:  "Runs a CQL content search, e.g. `atlassian cql \"space = AB AND title ~ 'design'\"`.",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("provide a CQL string (e.g. \"type = page AND text ~ 'export'\")")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		return runCQL(svc, args[0])
	},
}

var cqlSaveCmd = &cobra.Command{
	Use:   "save <name> <cql>",
	Short: "Save a named CQL query",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if cfg.SavedCQL == nil {
			cfg.SavedCQL = map[string]string{}
		}
		cfg.SavedCQL[args[0]] = args[1]
		return cfg.Save()
	},
}

var cqlRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a saved CQL query",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		cql, ok := cfg.SavedCQL[args[0]]
		if !ok {
			return fmt.Errorf("no saved CQL query %q", args[0])
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		return runCQL(svc, cql)
	},
}

var cqlListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved CQL queries",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		for name, q := range cfg.SavedCQL {
			fmt.Printf("%s\t%s\n", name, q)
		}
		return nil
	},
}

func runCQL(svc *service.Service, cql string) error {
	c, cancel := ctx()
	defer cancel()
	p := printer()
	var results []client.CQLResult
	if flagAll {
		all, err := svc.Client.SearchCQLAll(c, cql, flagPageSize)
		if err != nil {
			return err
		}
		results = all
	} else {
		page, err := svc.Client.SearchCQL(c, cql, (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		results = page.Results
		if !flagQuiet && !p.JSON {
			defer p.Msg("page %d (%d results, total %d)", flagPage, len(page.Results), page.TotalSize)
		}
	}
	if p.JSON {
		return p.JSONValue(results)
	}
	rows := make([][]string, len(results))
	for i, r := range results {
		space := ""
		if r.Space != nil {
			space = r.Space.Key
		}
		rows[i] = []string{r.ID, r.Type, r.Title, space}
	}
	return p.Table([]string{"ID", "Type", "Title", "Space"}, rows)
}

func init() {
	addPagingFlags(cqlCmd, 25)
	cqlCmd.AddCommand(cqlSaveCmd, cqlRunCmd, cqlListCmd)
	rootCmd.AddCommand(cqlCmd)
}
