// Command workctl is one CLI/MCP binary for Azure DevOps, Jira, Confluence, GitLab and GitHub.
package main

import (
	"os"

	"workctl/internal/app"
)

func main() {
	os.Exit(app.Main(os.Args[1:]))
}
