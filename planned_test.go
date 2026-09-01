package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPlannedModulesSayTheyAreNotImplemented(t *testing.T) {
	t.Setenv("WORKCTL_CONFIG", filepath.Join(t.TempDir(), "absent.json"))
	for _, name := range []string{"bitbucket", "asana"} {
		err := run([]string{name, "list"})
		if err == nil || !strings.Contains(err.Error(), "not implemented yet") || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: err = %v", name, err)
		}
		if _, real := cliModules[name]; real {
			t.Errorf("%s is a placeholder and must not be a runnable module", name)
		}
	}
	for _, tool := range mcpTools(defaultConfig()) {
		if _, planned := plannedModules[tool.Name]; planned || strings.HasPrefix(tool.Name, "bitbucket") || strings.HasPrefix(tool.Name, "asana") {
			t.Errorf("a placeholder must not advertise the MCP tool %q", tool.Name)
		}
	}
}
