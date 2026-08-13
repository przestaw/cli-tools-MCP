package cmd

import (
	"github.com/spf13/cobra"
	"workctl/internal/atlassian/repl"
)

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Interactive shell (navigate projects/issues and spaces/pages)",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		repl.New(svc, printer()).Run()
		return nil
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}
