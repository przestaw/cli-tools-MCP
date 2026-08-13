package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"workctl/internal/atlassian/client"
	"workctl/internal/atlassian/output"
	"workctl/internal/atlassian/service"
	"workctl/internal/atlassian/wizard"
)

var issueCmd = &cobra.Command{
	Use:     "issue",
	Aliases: []string{"i", "it", "item"},
	Short:   "Jira issues (work items)",
}

// issue flags
var (
	issueProject     string
	issueType        string
	issueSummary     string
	issueDesc        string
	issueParent      string
	issueAssignee    string
	issuePriority    string
	issueLabels      []string
	issueFields      []string
	issueFile        string
	issueYes         bool
	issueToStatus    string
	issueInteractive bool
	issueCascade     bool
	issueMarkdown    bool
)

var issueGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		is, err := svc.Client.Issue(c, args[0], "")
		if err != nil {
			return err
		}
		if p.JSON {
			return p.Raw(is.Raw)
		}
		return p.Issue(is, svc.BaseURL)
	},
}

var issueCreateCmd = &cobra.Command{
	Use:   "create --project <key> --type <name> [--summary ...] [--field k=v ...] [-f file] [-i]",
	Short: "Create an issue",
	RunE: func(cmd *cobra.Command, args []string) error {
		if issueProject == "" {
			return fmt.Errorf("--project is required")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()

		if issueInteractive {
			ref, err := wizard.Create(svc, issueProject, issueType, issueParent)
			if err != nil {
				return err
			}
			printer().Msg("created issue %s", ref.Key)
			return nil
		}

		if issueType == "" {
			return fmt.Errorf("--type is required (see `atlassian project types %s`)", issueProject)
		}
		typeID, err := resolveIssueTypeID(svc, issueProject, issueType)
		if err != nil {
			return err
		}

		fields := map[string]any{}
		if issueFile != "" {
			fields, err = readPayloadFile(issueFile)
			if err != nil {
				return err
			}
		}
		fields["project"] = map[string]any{"key": issueProject}
		fields["issuetype"] = map[string]any{"id": typeID}
		if issueSummary != "" {
			fields["summary"] = issueSummary
		}
		if issueDesc != "" {
			if issueMarkdown {
				fields["description"] = client.MarkdownToADF(issueDesc)
			} else {
				fields["description"] = client.TextToADF(issueDesc)
			}
		}
		if issueParent != "" {
			fields["parent"] = map[string]any{"key": issueParent}
		}
		if issuePriority != "" {
			fields["priority"] = map[string]any{"name": issuePriority}
		}
		if len(issueLabels) > 0 {
			fields["labels"] = issueLabels
		}
		if issueAssignee != "" {
			acc, err := svc.ResolveAccountID(c, issueAssignee)
			if err != nil {
				return err
			}
			fields["assignee"] = map[string]any{"accountId": acc}
		}
		if len(issueFields) > 0 {
			specs, err := service.ParseFieldArgs(issueFields)
			if err != nil {
				return err
			}
			extra, err := svc.BuildCreateFields(c, issueProject, typeID, specs)
			if err != nil {
				return err
			}
			for k, v := range extra {
				fields[k] = v
			}
		}
		ref, err := svc.Client.CreateIssue(c, fields)
		if err != nil {
			return err
		}
		printer().Msg("created issue %s", ref.Key)
		return nil
	},
}

var issueUpdateCmd = &cobra.Command{
	Use:   "update <key> [--summary ...] [--field k=v ...] [-f file]",
	Short: "Update issue fields",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		fields := map[string]any{}
		if issueFile != "" {
			fields, err = readPayloadFile(issueFile)
			if err != nil {
				return err
			}
		}
		if issueSummary != "" {
			fields["summary"] = issueSummary
		}
		if issueDesc != "" {
			if issueMarkdown {
				fields["description"] = client.MarkdownToADF(issueDesc)
			} else {
				fields["description"] = client.TextToADF(issueDesc)
			}
		}
		if len(issueFields) > 0 {
			specs, err := service.ParseFieldArgs(issueFields)
			if err != nil {
				return err
			}
			extra, err := svc.BuildEditFields(c, args[0], specs)
			if err != nil {
				return err
			}
			for k, v := range extra {
				fields[k] = v
			}
		}
		if len(fields) == 0 {
			return fmt.Errorf("nothing to update (use --summary/--description/--field/-f)")
		}
		if err := svc.Client.UpdateIssue(c, args[0], fields); err != nil {
			return err
		}
		printer().Msg("updated %s", args[0])
		return nil
	},
}

var issueSetFieldCmd = &cobra.Command{
	Use:   "set-field <key> <field> <value>",
	Short: "Set one field (field by name or id; value coerced via editmeta)",
	Args:  cobra.ExactArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		fields, err := svc.BuildEditFields(c, args[0], []service.FieldSpec{{Key: args[1], Value: args[2]}})
		if err != nil {
			return err
		}
		if err := svc.Client.UpdateIssue(c, args[0], fields); err != nil {
			return err
		}
		printer().Msg("updated %s", args[0])
		return nil
	},
}

var issueDeleteCmd = &cobra.Command{
	Use:   "delete <key>",
	Short: "Delete an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if !issueYes {
			if !confirm(fmt.Sprintf("Delete issue %s?", args[0])) {
				return fmt.Errorf("aborted")
			}
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		if err := svc.Client.DeleteIssue(c, args[0], issueCascade); err != nil {
			return err
		}
		printer().Msg("deleted %s", args[0])
		return nil
	},
}

var issueTransitionsCmd = &cobra.Command{
	Use:   "transitions <key>",
	Short: "List the transitions currently available on an issue",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		trs, err := svc.Client.Transitions(c, args[0])
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(trs)
		}
		rows := make([][]string, len(trs))
		for i, t := range trs {
			to := ""
			if t.To != nil {
				to = t.To.Name
			}
			rows[i] = []string{t.ID, t.Name, to}
		}
		return p.Table([]string{"ID", "Name", "To status"}, rows)
	},
}

var issueTransitionCmd = &cobra.Command{
	Use:   "transition <key> --to <status>",
	Short: "Change workflow status (transition by name or id)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if issueToStatus == "" {
			return fmt.Errorf("--to is required")
		}
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		id, name, err := svc.ResolveTransitionID(c, args[0], issueToStatus)
		if err != nil {
			return err
		}
		if err := svc.Client.DoTransition(c, args[0], id); err != nil {
			return err
		}
		printer().Msg("transitioned %s via %q", args[0], name)
		return nil
	},
}

var issueAssignCmd = &cobra.Command{
	Use:   "assign <key> <assignee>",
	Short: "Assign an issue ('me', a name/email query, an accountId, or 'none')",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		acc := ""
		if args[1] != "none" {
			acc, err = svc.ResolveAccountID(c, args[1])
			if err != nil {
				return err
			}
		}
		if err := svc.Client.Assign(c, args[0], acc); err != nil {
			return err
		}
		printer().Msg("assigned %s", args[0])
		return nil
	},
}

var issueSubtasksCmd = &cobra.Command{
	Use:   "subtasks <key>",
	Short: "List an issue's subtasks",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		is, err := svc.Client.Issue(c, args[0], "")
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(is.Fields.Subtasks)
		}
		rows := make([][]string, len(is.Fields.Subtasks))
		for i, st := range is.Fields.Subtasks {
			summary, status := "", ""
			if st.Fields != nil {
				summary = st.Fields.Summary
				if st.Fields.Status != nil {
					status = st.Fields.Status.Name
				}
			}
			rows[i] = []string{st.Key, summary, status}
		}
		return p.Table([]string{"Key", "Summary", "Status"}, rows)
	},
}

var issueHistoryCmd = &cobra.Command{
	Use:   "history <key>",
	Short: "Issue change history (changelog)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		is, err := svc.Client.Issue(c, args[0], "changelog")
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(is.Changelog)
		}
		if is.Changelog == nil {
			p.Msg("no changelog")
			return nil
		}
		var rows [][]string
		for _, h := range is.Changelog.Histories {
			for _, it := range h.Items {
				rows = append(rows, []string{
					shortTime(h.Created), output.UserName(h.Author), it.Field, it.FromString, it.ToString,
				})
			}
		}
		return p.Table([]string{"Time", "Author", "Field", "From", "To"}, rows)
	},
}

var issueOpenCmd = &cobra.Command{
	Use:   "open <key>",
	Short: "Open an issue in the browser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		return openBrowser(svc.Client.IssueURL(args[0]))
	},
}

func init() {
	issueCreateCmd.Flags().StringVar(&issueProject, "project", "", "project key (required)")
	issueCreateCmd.Flags().StringVar(&issueType, "type", "", "issue type name or id (required unless -i)")
	issueCreateCmd.Flags().StringVar(&issueSummary, "summary", "", "summary")
	issueCreateCmd.Flags().StringVar(&issueDesc, "description", "", "description (plain text; converted to ADF, or Markdown with --markdown)")
	issueCreateCmd.Flags().BoolVar(&issueMarkdown, "markdown", false, "parse --description as Markdown (headings, lists, **bold**, `code`, links) into ADF")
	issueCreateCmd.Flags().StringVar(&issueParent, "parent", "", "parent issue key (epic/parent)")
	issueCreateCmd.Flags().StringVar(&issueAssignee, "assignee", "", "assignee ('me', query, or accountId)")
	issueCreateCmd.Flags().StringVar(&issuePriority, "priority", "", "priority name")
	issueCreateCmd.Flags().StringArrayVar(&issueLabels, "label", nil, "label (repeatable)")
	issueCreateCmd.Flags().StringArrayVar(&issueFields, "field", nil, "field name=value (repeatable)")
	issueCreateCmd.Flags().StringVarP(&issueFile, "file", "f", "", "fields payload file (YAML or JSON)")
	issueCreateCmd.Flags().BoolVarP(&issueInteractive, "interactive", "i", false, "prompt for required fields")

	issueUpdateCmd.Flags().StringVar(&issueSummary, "summary", "", "new summary")
	issueUpdateCmd.Flags().StringVar(&issueDesc, "description", "", "new description (plain text; converted to ADF, or Markdown with --markdown)")
	issueUpdateCmd.Flags().BoolVar(&issueMarkdown, "markdown", false, "parse --description as Markdown into ADF")
	issueUpdateCmd.Flags().StringArrayVar(&issueFields, "field", nil, "field name=value (repeatable)")
	issueUpdateCmd.Flags().StringVarP(&issueFile, "file", "f", "", "fields payload file (YAML or JSON)")

	issueDeleteCmd.Flags().BoolVar(&issueYes, "yes", false, "skip confirmation")
	issueDeleteCmd.Flags().BoolVar(&issueCascade, "cascade", false, "also delete subtasks")

	issueTransitionCmd.Flags().StringVar(&issueToStatus, "to", "", "target transition (name, target status name, or id)")

	issueCmd.AddCommand(issueGetCmd, issueCreateCmd, issueUpdateCmd, issueSetFieldCmd,
		issueDeleteCmd, issueTransitionsCmd, issueTransitionCmd, issueAssignCmd,
		issueSubtasksCmd, issueHistoryCmd, issueOpenCmd)
	rootCmd.AddCommand(issueCmd)
}

// readPayloadFile reads a YAML or JSON object file into a map.
func readPayloadFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	if json.Unmarshal(data, &out) == nil {
		return out, nil
	}
	if err := yaml.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("parse %s as JSON or YAML: %w", path, err)
	}
	return out, nil
}

// confirm asks a y/N question on stdin.
func confirm(question string) bool {
	fmt.Printf("%s [y/N]: ", question)
	var line string
	fmt.Scanln(&line)
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// shortTime trims a Jira ISO timestamp to minute precision.
func shortTime(s string) string {
	if len(s) >= 16 {
		return strings.Replace(s[:16], "T", " ", 1)
	}
	return s
}

// openBrowser opens a URL in the default browser cross-platform.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
