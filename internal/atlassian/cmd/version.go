package cmd

import "workctl/internal/clikit"

// version is overridable at build time via -ldflags "-X workctl/internal/atlassian/cmd.version=..."
var version = "dev"

var versionCmd = clikit.Version("atlassian", &version)

func init() {
	rootCmd.AddCommand(versionCmd)
}
