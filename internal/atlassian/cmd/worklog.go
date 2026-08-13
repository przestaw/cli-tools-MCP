package cmd

import (
	"github.com/spf13/cobra"
	"workctl/internal/atlassian/client"
	"workctl/internal/atlassian/output"
)

var worklogCmd = &cobra.Command{
	Use:   "worklog",
	Short: "Jira issue worklogs (time tracking)",
}

var (
	worklogComment string
	worklogStarted string
)

var worklogListCmd = &cobra.Command{
	Use:   "list <issueKey>",
	Short: "List worklogs",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		page, err := svc.Client.Worklogs(c, args[0], (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(page)
		}
		rows := make([][]string, len(page.Worklogs))
		for i, w := range page.Worklogs {
			rows[i] = []string{w.ID, output.UserName(w.Author), shortTime(w.Started), w.TimeSpent, client.ADFToText(w.Comment)}
		}
		if err := p.Table([]string{"ID", "Author", "Started", "Spent", "Comment"}, rows); err != nil {
			return err
		}
		if !flagQuiet {
			p.Msg("total %d", page.Total)
		}
		return nil
	},
}

var worklogAddCmd = &cobra.Command{
	Use:   "add <issueKey> <timeSpent>",
	Short: "Log time (Jira duration syntax: '2h 30m', '1d') ",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		var comment any
		if worklogComment != "" {
			comment = client.TextToADF(worklogComment)
		}
		w, err := svc.Client.AddWorklog(c, args[0], args[1], worklogStarted, comment)
		if err != nil {
			return err
		}
		printer().Msg("logged %s on %s (worklog %s)", w.TimeSpent, args[0], w.ID)
		return nil
	},
}

func init() {
	worklogAddCmd.Flags().StringVar(&worklogComment, "comment", "", "worklog comment (plain text -> ADF)")
	worklogAddCmd.Flags().StringVar(&worklogStarted, "started", "", "start timestamp (e.g. 2026-06-12T10:00:00.000+0200; default: now)")
	addPagingFlags(worklogListCmd, 50)
	worklogCmd.AddCommand(worklogListCmd, worklogAddCmd)
	rootCmd.AddCommand(worklogCmd)
}
