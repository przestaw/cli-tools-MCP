package main

import "fmt"

// plannedModules are names reserved for modules that do not exist yet. `workctl <name>` says so instead of
// "unknown command", which keeps the command line stable. To implement one, replace its entry with the real module
// (a package under internal/ or a file like jira.go), dispatch to it in run(), and add its MCP tools.
var plannedModules = map[string]string{
	"bitbucket": "Bitbucket: repositories and pull requests",
	"asana":     "Asana: tasks and projects",
}

func cmdPlanned(name string) error {
	return fmt.Errorf("the %s module is not implemented yet (reserved for: %s)", name, plannedModules[name])
}
