package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var boardCmd = &cobra.Command{
	Use:   "board",
	Short: "Jira software boards",
}

var (
	boardProject     string
	boardSprintState string
	boardJQL         string
)

var boardListCmd = &cobra.Command{
	Use:   "list [--project KEY]",
	Short: "List boards",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		page, err := svc.Client.Boards(c, boardProject, (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(page)
		}
		rows := make([][]string, len(page.Values))
		for i, b := range page.Values {
			rows[i] = []string{strconv.Itoa(b.ID), b.Name, b.Type, b.Location.ProjectKey}
		}
		if err := p.Table([]string{"ID", "Name", "Type", "Project"}, rows); err != nil {
			return err
		}
		if !flagQuiet {
			p.Msg("total %d", page.Total)
		}
		return nil
	},
}

var boardSprintsCmd = &cobra.Command{
	Use:   "sprints <boardId> [--state active|future|closed]",
	Short: "List a board's sprints",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		page, err := svc.Client.BoardSprints(c, id, boardSprintState, (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(page)
		}
		rows := make([][]string, len(page.Values))
		for i, s := range page.Values {
			rows[i] = []string{strconv.Itoa(s.ID), s.Name, s.State, shortDateStr(s.StartDate), shortDateStr(s.EndDate), s.Goal}
		}
		return p.Table([]string{"ID", "Name", "State", "Start", "End", "Goal"}, rows)
	},
}

var boardIssuesCmd = &cobra.Command{
	Use:   "issues <boardId> [--jql extra]",
	Short: "List a board's issues",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		issues, total, err := svc.Client.BoardIssues(c, id, boardJQL, (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(issues)
		}
		if err := p.IssueTable(issues); err != nil {
			return err
		}
		if !flagQuiet {
			p.Msg("total %d", total)
		}
		return nil
	},
}

var sprintCmd = &cobra.Command{
	Use:   "sprint",
	Short: "Jira scrum sprints",
}

var sprintGetCmd = &cobra.Command{
	Use:   "get <sprintId>",
	Short: "Get a sprint",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		s, err := svc.Client.Sprint(c, id)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(s)
		}
		return p.Table([]string{"Field", "Value"}, [][]string{
			{"ID", strconv.Itoa(s.ID)}, {"Name", s.Name}, {"State", s.State},
			{"Start", s.StartDate}, {"End", s.EndDate}, {"Goal", s.Goal},
		})
	},
}

var sprintAddCmd = &cobra.Command{
	Use:   "add <sprintId> <issueKey>...",
	Short: "Add one or more issues to a sprint",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("sprint id must be a number (find it with `atlassian board sprints <boardId>`): %w", err)
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		keys := args[1:]
		if err := svc.Client.AddIssuesToSprint(c, id, keys...); err != nil {
			return err
		}
		printer().Msg("added %d issue(s) to sprint %d", len(keys), id)
		return nil
	},
}

var sprintIssuesCmd = &cobra.Command{
	Use:   "issues <sprintId> [--jql extra]",
	Short: "List a sprint's issues",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return err
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		issues, total, err := svc.Client.SprintIssues(c, id, boardJQL, (flagPage-1)*flagPageSize, flagPageSize)
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(issues)
		}
		if err := p.IssueTable(issues); err != nil {
			return err
		}
		if !flagQuiet {
			p.Msg("total %d", total)
		}
		return nil
	},
}

// shortDateStr trims an ISO timestamp to the date part.
func shortDateStr(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func init() {
	boardListCmd.Flags().StringVar(&boardProject, "project", "", "filter by project key")
	addPagingFlags(boardListCmd, 50)
	boardSprintsCmd.Flags().StringVar(&boardSprintState, "state", "", "sprint state filter (future,active,closed)")
	addPagingFlags(boardSprintsCmd, 50)
	boardIssuesCmd.Flags().StringVar(&boardJQL, "jql", "", "extra JQL filter")
	addPagingFlags(boardIssuesCmd, 50)
	sprintIssuesCmd.Flags().StringVar(&boardJQL, "jql", "", "extra JQL filter")
	addPagingFlags(sprintIssuesCmd, 50)

	boardCmd.AddCommand(boardListCmd, boardSprintsCmd, boardIssuesCmd)
	sprintCmd.AddCommand(sprintGetCmd, sprintIssuesCmd, sprintAddCmd)
	rootCmd.AddCommand(boardCmd, sprintCmd)
}
