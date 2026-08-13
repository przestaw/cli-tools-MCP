package cmd

import (
	"github.com/spf13/cobra"
	"workctl/internal/atlassian/service"
)

var (
	traceDepth     int
	traceDirection string
	traceFlat      bool
	traceHierarchy bool
)

var traceCmd = &cobra.Command{
	Use:   "trace <issueKey>",
	Short: "Traceability tree over issue links",
	Long:  "Walks an issue's links (and with --hierarchy its parent/subtasks) into an indented tree.\nDirections: down = outward links (this issue's phrase, e.g. 'blocks'),\nup = inward links (e.g. 'is blocked by'), both = default.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		p := printer()
		if traceFlat {
			links, err := svc.FlatLinks(c, args[0])
			if err != nil {
				return err
			}
			if p.JSON {
				return p.JSONValue(links)
			}
			return p.TraceLinks(links)
		}
		root, err := svc.Trace(c, args[0], service.TraceOptions{
			Depth:     traceDepth,
			Direction: traceDirection,
			Hierarchy: traceHierarchy,
		})
		if err != nil {
			return err
		}
		if p.JSON {
			return p.JSONValue(root)
		}
		p.Trace(root)
		return nil
	},
}

func init() {
	traceCmd.Flags().IntVar(&traceDepth, "depth", 2, "maximum link depth")
	traceCmd.Flags().StringVar(&traceDirection, "direction", "both", "link direction to follow: up|down|both")
	traceCmd.Flags().BoolVar(&traceFlat, "flat", false, "one-level table instead of a tree")
	traceCmd.Flags().BoolVar(&traceHierarchy, "hierarchy", false, "also follow parent/subtask edges")
	rootCmd.AddCommand(traceCmd)
}
