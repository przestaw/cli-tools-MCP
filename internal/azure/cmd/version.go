package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version is overridable at build time via -ldflags "-X workctl/internal/azure/cmd.version=..."
var version = "dev"

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print azure version",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("azure %s\n", version)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
