// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"workctl/internal/atlassian/output"
	"workctl/internal/atlassian/service"
)

var projectCmd = &cobra.Command{
	Use:     "project",
	Aliases: []string{"proj"},
	Short:   "Jira projects",
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List projects",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		ps, err := svc.Client.ProjectsAll(c)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(ps)
		}
		rows := make([][]string, len(ps))
		for i, pr := range ps {
			rows[i] = []string{pr.Key, pr.Name, pr.ID, pr.TypeKey, output.UserName(pr.Lead)}
		}
		return p.Table([]string{"Key", "Name", "ID", "Type", "Lead"}, rows)
	},
}

var projectGetCmd = &cobra.Command{
	Use:   "get <key>",
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

var projectTypesCmd = &cobra.Command{
	Use:   "types <projectKey>",
	Short: "Issue types creatable in a project (with ids for createmeta)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		ts, err := svc.Client.CreateMetaIssueTypes(c, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(ts)
		}
		rows := make([][]string, len(ts))
		for i, t := range ts {
			sub := ""
			if t.Subtask {
				sub = "subtask"
			}
			rows[i] = []string{t.ID, t.Name, sub, t.Description}
		}
		return p.Table([]string{"ID", "Name", "Subtask", "Description"}, rows)
	},
}

var projectFieldsCmd = &cobra.Command{
	Use:   "fields <projectKey> <issueType>",
	Short: "Create-screen fields for a project + issue type (name or id): type, required, options",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		typeID, err := resolveIssueTypeID(svc, args[0], args[1])
		if err != nil {
			return err
		}
		fs, err := svc.Client.CreateMetaFields(c, args[0], typeID)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(fs)
		}
		rows := make([][]string, len(fs))
		for i, f := range fs {
			schema, required := "", ""
			if f.Schema != nil {
				schema = f.Schema.Type
				if f.Schema.Items != "" {
					schema += " of " + f.Schema.Items
				}
			}
			if f.Required {
				required = "yes"
			}
			opts := ""
			if len(f.AllowedValues) > 2 {
				opts = "yes (use --json)"
			}
			rows[i] = []string{f.IDKey(), f.Name, schema, required, opts}
		}
		return p.Table([]string{"ID", "Name", "Schema", "Required", "Options"}, rows)
	},
}

var projectStatusesCmd = &cobra.Command{
	Use:   "statuses <projectKey>",
	Short: "Issue types with their workflow statuses (raw)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		raw, err := svc.Client.ProjectStatuses(c, args[0])
		if err != nil {
			return err
		}
		return printer().Raw(raw)
	},
}

// resolveIssueTypeID maps an issue type name or id to its id via createmeta.
func resolveIssueTypeID(svc *service.Service, projectKey, nameOrID string) (string, error) {
	c, cancel := ctx()
	defer cancel()
	ts, err := svc.Client.CreateMetaIssueTypes(c, projectKey)
	if err != nil {
		return "", err
	}
	var names []string
	for _, t := range ts {
		if t.ID == nameOrID || strings.EqualFold(t.Name, nameOrID) {
			return t.ID, nil
		}
		names = append(names, t.Name)
	}
	return "", fmt.Errorf("issue type %q not found in %s (available: %s)", nameOrID, projectKey, strings.Join(names, ", "))
}

func init() {
	projectCmd.AddCommand(projectListCmd, projectGetCmd, projectTypesCmd, projectFieldsCmd, projectStatusesCmd)
	rootCmd.AddCommand(projectCmd)
}
