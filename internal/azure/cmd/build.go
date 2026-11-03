// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"workctl/internal/azure/client"
	"workctl/internal/azure/output"
	"workctl/internal/azure/service"
)

var buildCmd = &cobra.Command{
	Use:     "build",
	Aliases: []string{"b", "builds"},
	Short:   "Builds (Build API)",
}

var (
	buildDefs    string
	buildBranch  string
	buildStatus  string
	buildResult  string
	buildDefName string
)

var buildDefsCmd = &cobra.Command{
	Use:     "definitions",
	Aliases: []string{"defs"},
	Short:   "List build definitions",
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
		defs, err := svc.Client.BuildDefinitions(c, project, buildDefName, true)
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

var buildListCmd = &cobra.Command{
	Use:   "list [--definitions ids] [--branch B] [--status S] [--result R]",
	Short: "List builds (newest first)",
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
		builds, err := svc.Client.Builds(c, project, client.BuildSearch{
			DefinitionIDs: buildDefs,
			Branch:        service.NormalizeBranch(buildBranch),
			StatusFilter:  buildStatus,
			ResultFilter:  buildResult,
			Top:           flagTop,
		}, flagAll)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(builds)
		}
		rows := make([][]string, len(builds))
		for i, b := range builds {
			rows[i] = []string{
				fmt.Sprintf("%d", b.ID), b.BuildNumber, b.Definition.Name,
				b.Status, b.Result, service.ShortRef(b.SourceBranch), shortTime(b.QueueTime),
			}
		}
		return p.Table([]string{"ID", "Number", "Definition", "Status", "Result", "Branch", "Queued"}, rows)
	},
}

var buildGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a build",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("build id", args[0])
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
		b, err := svc.Client.Build(c, project, id)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.Raw(b.Raw)
		}
		return p.KeyValue([][2]string{
			{"ID", fmt.Sprintf("%d", b.ID)},
			{"Number", b.BuildNumber},
			{"Definition", b.Definition.Name},
			{"Status", b.Status},
			{"Result", b.Result},
			{"Branch", service.ShortRef(b.SourceBranch)},
			{"Requested by", output.UserName(b.RequestedBy)},
			{"Queued", shortTime(b.QueueTime)},
			{"Started", shortTime(b.StartTime)},
			{"Finished", shortTime(b.FinishTime)},
		})
	},
}

var buildQueueCmd = &cobra.Command{
	Use:   "queue <definitionId> [--branch B]",
	Short: "Queue a new build of a definition",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("definition id", args[0])
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
		b, err := svc.Client.QueueBuild(c, project, id, service.NormalizeBranch(buildBranch))
		if err != nil {
			return err
		}
		printer().Msg("queued build %d (%s)", b.ID, b.Status)
		return nil
	},
}

var buildLogsCmd = &cobra.Command{
	Use:   "logs <buildId> [logId]",
	Short: "List a build's logs, or print one log's content",
	Args:  cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		buildID, err := atoiArg("build id", args[0])
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
		if len(args) == 2 {
			logID, err := atoiArg("log id", args[1])
			if err != nil {
				return err
			}
			data, err := svc.Client.BuildLogContent(c, project, buildID, logID)
			if err != nil {
				return err
			}
			_, err = os.Stdout.Write(data)
			return err
		}
		logs, err := svc.Client.BuildLogs(c, project, buildID)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(logs)
		}
		rows := make([][]string, len(logs))
		for i, l := range logs {
			rows[i] = []string{fmt.Sprintf("%d", l.ID), l.Type, fmt.Sprintf("%d", l.LineCount)}
		}
		return p.Table([]string{"Log", "Type", "Lines"}, rows)
	},
}

var buildOpenCmd = &cobra.Command{
	Use:   "open <id>",
	Short: "Open a build result in the browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("build id", args[0])
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
		return openBrowser(svc.Client.BuildWebURL(project, id))
	},
}

func init() {
	buildDefsCmd.Flags().StringVar(&buildDefName, "name", "", "filter definitions by name")

	buildListCmd.Flags().StringVar(&buildDefs, "definitions", "", "comma-separated definition ids")
	buildListCmd.Flags().StringVar(&buildBranch, "branch", "", "branch filter")
	buildListCmd.Flags().StringVar(&buildStatus, "status", "", "inProgress|completed|notStarted|all")
	buildListCmd.Flags().StringVar(&buildResult, "result", "", "succeeded|failed|canceled|partiallySucceeded")
	addListFlags(buildListCmd, 30)

	buildQueueCmd.Flags().StringVar(&buildBranch, "branch", "", "branch to build (default: definition default)")

	buildCmd.AddCommand(buildDefsCmd, buildListCmd, buildGetCmd, buildQueueCmd, buildLogsCmd, buildOpenCmd)
	rootCmd.AddCommand(buildCmd)
}
