package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is overridable at build time via -ldflags "-X workctl/internal/atlassian/cmd.version=..."
var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print atlassian version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("atlassian %s\n", version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
