// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"workctl/internal/azure/service"
)

var repoCmd = &cobra.Command{
	Use:     "repo",
	Aliases: []string{"r", "repos"},
	Short:   "Git repositories",
}

var (
	repoBranch    string
	repoPath      string
	repoRecursion string
	repoOut       string
)

var repoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List repositories in the project",
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
		repos, err := svc.Client.Repositories(c, project)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(repos)
		}
		rows := make([][]string, len(repos))
		for i, r := range repos {
			rows[i] = []string{r.Name, r.ID, service.ShortRef(r.DefaultBranch)}
		}
		return p.Table([]string{"Name", "ID", "Default branch"}, rows)
	},
}

var repoGetCmd = &cobra.Command{
	Use:   "get <repo>",
	Short: "Get a repository (name or id)",
	Args:  cobra.ExactArgs(1),
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
		r, err := svc.Client.Repository(c, project, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.Raw(r.Raw)
		}
		return p.KeyValue([][2]string{
			{"Name", r.Name},
			{"ID", r.ID},
			{"Project", r.Project.Name},
			{"Default branch", service.ShortRef(r.DefaultBranch)},
			{"Size", fmt.Sprintf("%d", r.Size)},
			{"Disabled", fmt.Sprintf("%v", r.IsDisabled)},
			{"Web", r.WebURL},
			{"Clone (HTTPS)", r.RemoteURL},
			{"Clone (SSH)", r.SSHURL},
		})
	},
}

var repoBranchesCmd = &cobra.Command{
	Use:     "branches <repo>",
	Aliases: []string{"refs"},
	Short:   "List branches of a repository",
	Args:    cobra.ExactArgs(1),
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
		refs, err := svc.Client.Refs(c, project, args[0], "heads")
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(refs)
		}
		rows := make([][]string, len(refs))
		for i, r := range refs {
			rows[i] = []string{service.ShortRef(r.Name), r.ObjectID}
		}
		return p.Table([]string{"Branch", "Commit"}, rows)
	},
}

var repoFilesCmd = &cobra.Command{
	Use:   "files <repo> [--path /] [--branch main] [--recursion none|oneLevel|full]",
	Short: "List files/folders in a repository",
	Args:  cobra.ExactArgs(1),
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
		items, err := svc.Client.Items(c, project, args[0], repoPath, service.NormalizeBranch(repoBranch), repoRecursion)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(items)
		}
		rows := make([][]string, len(items))
		for i, it := range items {
			kind := "file"
			if it.IsFolder {
				kind = "dir"
			}
			rows[i] = []string{kind, it.Path}
		}
		return p.Table([]string{"Type", "Path"}, rows)
	},
}

var repoShowCmd = &cobra.Command{
	Use:   "show <repo> <path> [--branch main] [-o file]",
	Short: "Print (or download) the content of a file",
	Args:  cobra.ExactArgs(2),
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
		data, err := svc.Client.FileContent(c, project, args[0], args[1], service.NormalizeBranch(repoBranch))
		if err != nil {
			return err
		}
		if repoOut != "" {
			if err := os.WriteFile(repoOut, data, 0o644); err != nil {
				return err
			}
			printer().Msg("wrote %s (%d bytes)", repoOut, len(data))
			return nil
		}
		_, err = os.Stdout.Write(data)
		return err
	},
}

var repoOpenCmd = &cobra.Command{
	Use:   "open <repo>",
	Short: "Open a repository in the browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		project, err := svc.Project(flagProject)
		if err != nil {
			return err
		}
		return openBrowser(svc.Client.RepoWebURL(project, args[0]))
	},
}

func init() {
	repoFilesCmd.Flags().StringVar(&repoPath, "path", "/", "path to list under")
	repoFilesCmd.Flags().StringVar(&repoBranch, "branch", "", "branch (default: repo default)")
	repoFilesCmd.Flags().StringVar(&repoRecursion, "recursion", "oneLevel", "none|oneLevel|full")

	repoShowCmd.Flags().StringVar(&repoBranch, "branch", "", "branch (default: repo default)")
	repoShowCmd.Flags().StringVarP(&repoOut, "out", "o", "", "write to file instead of stdout")

	repoCmd.AddCommand(repoListCmd, repoGetCmd, repoBranchesCmd, repoFilesCmd, repoShowCmd, repoOpenCmd)
	rootCmd.AddCommand(repoCmd)
}
