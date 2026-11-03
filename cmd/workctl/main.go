// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Command workctl is one CLI/MCP binary for Azure DevOps, Jira, Confluence, GitLab and GitHub.
package main

import (
	"os"

	"workctl/internal/app"
)

func main() {
	os.Exit(app.Main(os.Args[1:]))
}
