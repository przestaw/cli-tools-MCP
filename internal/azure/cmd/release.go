package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"workctl/internal/azure/client"
	"workctl/internal/azure/output"
)

var releaseCmd = &cobra.Command{
	Use:     "release",
	Aliases: []string{"rel", "releases"},
	Short:   "Releases (Release Management API, vsrm host)",
}

var (
	releaseDefID  int
	releaseStatus string
	releaseSearch string
	releaseDescr  string
)

var releaseDefsCmd = &cobra.Command{
	Use:     "definitions",
	Aliases: []string{"defs"},
	Short:   "List release definitions",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		defs, err := svc.Client.ReleaseDefinitions(c, project, releaseSearch, true)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(defs)
		}
		rows := make([][]string, len(defs))
		for i, d := range defs {
			rows[i] = []string{fmt.Sprintf("%d", d.ID), d.Name, d.Path}
		}
		return p.Table([]string{"ID", "Name", "Path"}, rows)
	},
}

var releaseListCmd = &cobra.Command{
	Use:   "list [--definition id] [--status active|abandoned|draft]",
	Short: "List releases (newest first)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		rels, err := svc.Client.Releases(c, project, client.ReleaseSearch{
			DefinitionID: releaseDefID,
			StatusFilter: releaseStatus,
			Top:          flagTop,
		}, flagAll)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(rels)
		}
		rows := make([][]string, len(rels))
		for i, r := range rels {
			rows[i] = []string{
				fmt.Sprintf("%d", r.ID), r.Name, r.ReleaseDefinition.Name,
				r.Status, output.UserName(r.CreatedBy), shortTime(r.CreatedOn),
			}
		}
		return p.Table([]string{"ID", "Name", "Definition", "Status", "Created by", "Created"}, rows)
	},
}

var releaseGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a release (with environment/stage status)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("release id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		r, err := svc.Client.Release(c, project, id)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.Raw(r.Raw)
		}
		var stages []string
		for _, e := range r.Environments {
			stages = append(stages, fmt.Sprintf("%s:%s", e.Name, e.Status))
		}
		return p.KeyValue([][2]string{
			{"ID", fmt.Sprintf("%d", r.ID)},
			{"Name", r.Name},
			{"Definition", r.ReleaseDefinition.Name},
			{"Status", r.Status},
			{"Created by", output.UserName(r.CreatedBy)},
			{"Created", shortTime(r.CreatedOn)},
			{"Stages", strings.Join(stages, ", ")},
		})
	},
}

var releaseCreateCmd = &cobra.Command{
	Use:   "create --definition <id> [--description D]",
	Short: "Create a release from a definition",
	RunE: func(cmd *cobra.Command, args []string) error {
		if releaseDefID == 0 {
			return fmt.Errorf("--definition is required")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		r, err := svc.Client.CreateRelease(c, project, releaseDefID, releaseDescr)
		if err != nil {
			return err
		}
		printer().Msg("created release %d (%s)", r.ID, r.Name)
		return nil
	},
}

var releaseOpenCmd = &cobra.Command{
	Use:   "open <id>",
	Short: "Open a release in the browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("release id", args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		return openBrowser(svc.Client.ReleaseWebURL(project, id))
	},
}

func init() {
	releaseDefsCmd.Flags().StringVar(&releaseSearch, "search", "", "filter definitions by name")

	releaseListCmd.Flags().IntVar(&releaseDefID, "definition", 0, "release definition id")
	releaseListCmd.Flags().StringVar(&releaseStatus, "status", "", "active|abandoned|draft")
	addListFlags(releaseListCmd, 30)

	releaseCreateCmd.Flags().IntVar(&releaseDefID, "definition", 0, "release definition id (required)")
	releaseCreateCmd.Flags().StringVar(&releaseDescr, "description", "", "release description")

	releaseCmd.AddCommand(releaseDefsCmd, releaseListCmd, releaseGetCmd, releaseCreateCmd, releaseOpenCmd)
	rootCmd.AddCommand(releaseCmd)
}
