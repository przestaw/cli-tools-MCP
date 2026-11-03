// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var pipelineCmd = &cobra.Command{
	Use:     "pipeline",
	Aliases: []string{"pl", "pipelines"},
	Short:   "Pipelines (YAML/Classic, Pipelines API)",
}

var pipelineParams string

var pipelineListCmd = &cobra.Command{
	Use:   "list",
	Short: "List pipelines in the project",
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
		pls, err := svc.Client.Pipelines(c, project, true)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(pls)
		}
		rows := make([][]string, len(pls))
		for i, pl := range pls {
			rows[i] = []string{fmt.Sprintf("%d", pl.ID), pl.Name, pl.Folder}
		}
		return p.Table([]string{"ID", "Name", "Folder"}, rows)
	},
}

var pipelineGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get a pipeline (full JSON)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pipeline id", args[0])
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
		raw, err := svc.Client.Pipeline(c, project, id)
		if err != nil {
			return err
		}
		return printer().Raw(raw)
	},
}

var pipelineRunsCmd = &cobra.Command{
	Use:   "runs <pipelineId>",
	Short: "List the runs of a pipeline (newest first)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pipeline id", args[0])
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
		runs, err := svc.Client.PipelineRuns(c, project, id)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(runs)
		}
		rows := make([][]string, len(runs))
		for i, r := range runs {
			rows[i] = []string{fmt.Sprintf("%d", r.ID), r.Name, r.State, r.Result, shortTime(r.CreatedDate)}
		}
		return p.Table([]string{"Run", "Name", "State", "Result", "Created"}, rows)
	},
}

var pipelineRunCmd = &cobra.Command{
	Use:   "run <pipelineId> [--branch B] [--params '<json>']",
	Short: "Queue a new run of a pipeline",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := atoiArg("pipeline id", args[0])
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
		body := map[string]any{}
		if pipelineParams != "" {
			if err := json.Unmarshal([]byte(pipelineParams), &body); err != nil {
				return fmt.Errorf("invalid --params JSON: %w", err)
			}
		}
		if repoBranch != "" {
			body["resources"] = map[string]any{
				"repositories": map[string]any{
					"self": map[string]any{"refName": "refs/heads/" + repoBranch},
				},
			}
		}
		run, err := svc.Client.RunPipeline(c, project, id, body)
		if err != nil {
			return err
		}
		printer().Msg("queued run %d (%s)", run.ID, run.State)
		return nil
	},
}

func init() {
	pipelineRunCmd.Flags().StringVar(&repoBranch, "branch", "", "branch to run (default: pipeline default)")
	pipelineRunCmd.Flags().StringVar(&pipelineParams, "params", "", "raw run payload JSON (templateParameters/resources/...)")

	pipelineCmd.AddCommand(pipelineListCmd, pipelineGetCmd, pipelineRunsCmd, pipelineRunCmd)
	rootCmd.AddCommand(pipelineCmd)
}
