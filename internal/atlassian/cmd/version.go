// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import "workctl/internal/clikit"

// version is overridable at build time via -ldflags "-X workctl/internal/atlassian/cmd.version=..."
var version = "dev"

var versionCmd = clikit.Version("atlassian", &version)

func init() {
	rootCmd.AddCommand(versionCmd)
}
