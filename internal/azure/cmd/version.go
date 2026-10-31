package cmd

import "workctl/internal/clikit"

// version is overridable at build time via -ldflags "-X workctl/internal/azure/cmd.version=..."
var version = "dev"

var versionCmd = clikit.Version("azure", &version)

func init() {
	rootCmd.AddCommand(versionCmd)
}
