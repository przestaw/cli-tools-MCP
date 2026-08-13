package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"workctl/internal/atlassian/service"
)

var jqlCmd = &cobra.Command{
	Use:     "jql [jql]",
	Aliases: []string{"q", "query"},
	Short:   "Run a JQL query (or build one from flags)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		jql := ""
		if len(args) > 0 {
			jql = args[0]
		} else {
			b := &service.JQLBuilder{}
			jql = b.Project(jqlProject).Status(jqlStatus).Type(jqlType).
				Assignee(jqlAssignee).Label(jqlLabel).Text(jqlText).String()
		}
		if jql == "" {
			return fmt.Errorf("provide a JQL string or builder flags (--project/--status/...)")
		}
		return runJQL(svc, jql)
	},
}

var jqlSaveCmd = &cobra.Command{
	Use:   "save <name> <jql>",
	Short: "Save a named JQL query",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if cfg.SavedJQL == nil {
			cfg.SavedJQL = map[string]string{}
		}
		cfg.SavedJQL[args[0]] = args[1]
		return cfg.Save()
	},
}

var jqlRunCmd = &cobra.Command{
	Use:   "run <name>",
	Short: "Run a saved query",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		jql, ok := cfg.SavedJQL[args[0]]
		if !ok {
			return fmt.Errorf("no saved query %q", args[0])
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		return runJQL(svc, jql)
	},
}

var jqlListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved queries",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		for name, q := range cfg.SavedJQL {
			fmt.Printf("%s\t%s\n", name, q)
		}
		return nil
	},
}

var (
	jqlProject  string
	jqlStatus   string
	jqlType     string
	jqlAssignee string
	jqlLabel    string
	jqlText     string
	jqlSelect   []string
)

// runJQL executes a JQL search honoring the shared paging flags.
func runJQL(svc *service.Service, jql string) error {
	c, cancel := ctx()
	defer cancel()
	p := printer()
	if flagAll {
		issues, err := svc.Client.SearchJQLAll(c, jql, jqlSelect, flagPageSize)
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
			p.Msg("%d issues", len(issues))
		}
		return nil
	}
	page, err := svc.Client.SearchJQLPage(c, jql, jqlSelect, flagPage, flagPageSize)
	if err != nil {
		return err
	}
	if p.JSON {
		return p.JSONValue(page)
	}
	if err := p.IssueTable(page.Issues); err != nil {
		return err
	}
	if !flagQuiet {
		more := ""
		if !page.IsLast && page.NextPageToken != "" {
			more = "+"
		}
		if count, err := svc.Client.ApproximateCount(c, jql); err == nil {
			p.Msg("page %d (%d issues%s)  total ~%d", flagPage, len(page.Issues), more, count)
		} else {
			p.Msg("page %d (%d issues%s)", flagPage, len(page.Issues), more)
		}
	}
	return nil
}

func init() {
	jqlCmd.Flags().StringVar(&jqlProject, "project", "", "filter by project key")
	jqlCmd.Flags().StringVar(&jqlStatus, "status", "", "filter by status")
	jqlCmd.Flags().StringVar(&jqlType, "type", "", "filter by issue type")
	jqlCmd.Flags().StringVar(&jqlAssignee, "assignee", "", "filter by assignee ('me' = current user)")
	jqlCmd.Flags().StringVar(&jqlLabel, "label", "", "filter by label")
	jqlCmd.Flags().StringVar(&jqlText, "text", "", "full-text filter (text ~)")
	jqlCmd.Flags().StringSliceVar(&jqlSelect, "select", nil, "issue fields to fetch (default: table columns; '*all' = everything)")
	addPagingFlags(jqlCmd, 50)

	jqlCmd.AddCommand(jqlSaveCmd, jqlRunCmd, jqlListCmd)
	rootCmd.AddCommand(jqlCmd)
}
