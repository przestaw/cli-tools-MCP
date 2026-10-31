// @file plugin_test.go
// @brief Keeps the agent package (plugins/workctl, the marketplace, the installers) in step with the code.
// @see plugins/workctl/skills/workctl/SKILL.md  the skill whose tool names are checked against mcpTools
// @see scripts/install.sh, scripts/install.ps1  the installers that copy the skill

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// readJSON decodes a repository file into out.
func readJSON(t *testing.T, path string, out any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
}

func TestPluginPackageMatchesTheCode(t *testing.T) {
	var plugin struct{ Name, Version string }
	readJSON(t, "plugins/workctl/.claude-plugin/plugin.json", &plugin)
	if plugin.Name != "workctl" || plugin.Version != version {
		t.Fatalf("plugin.json = %+v, want name workctl and version %s", plugin, version)
	}

	var market struct {
		Name    string
		Plugins []struct{ Name, Source string }
	}
	readJSON(t, ".claude-plugin/marketplace.json", &market)
	if len(market.Plugins) != 1 || market.Plugins[0].Name != plugin.Name {
		t.Fatalf("marketplace = %+v: the entry name must equal the manifest name", market)
	}
	src := market.Plugins[0].Source
	if !strings.HasPrefix(src, "./") || strings.Contains(src, "..") {
		t.Fatalf("marketplace source %q must be a path inside the repository", src)
	}
	if _, err := os.Stat(filepath.Join(src, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("marketplace source %q: %v", src, err)
	}

	var mcp struct {
		McpServers map[string]struct {
			Command string
			Args    []string
		}
	}
	readJSON(t, "plugins/workctl/.mcp.json", &mcp)
	if s := mcp.McpServers["workctl"]; s.Command != "workctl" || strings.Join(s.Args, " ") != "mcp" {
		t.Fatalf(".mcp.json server = %+v, want `workctl mcp`", s)
	}
}

func TestSkillNamesOnlyExistingMCPTools(t *testing.T) {
	b, err := os.ReadFile("plugins/workctl/skills/workctl/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	skill := string(b)
	front := regexp.MustCompile(`(?s)^---\nname: (\S+)\ndescription: '(.*?)'\n---\n`).FindStringSubmatch(skill)
	if front == nil || front[1] != "workctl" || len(front[2]) > 1024 {
		t.Fatalf("front matter: name must be workctl (the folder name) and the description at most 1024 characters: %q", front)
	}

	// Every backend and write access on, so that every tool is advertised.
	cfg := defaultConfig()
	cfg.Policy.AllowWrite = true
	for _, kind := range []string{"github", "gitlab", "jira", "azure"} {
		cfg.Profiles[kind] = Profile{Kind: kind, URL: "https://" + kind + ".example.test"}
	}
	advertised := map[string]bool{}
	for _, tool := range mcpTools(cfg) {
		advertised[tool.Name] = true
	}
	mentioned := regexp.MustCompile("`((?:github|gitlab|jira|task|pr)_[a-z_]+)`").FindAllStringSubmatch(skill, -1)
	if len(mentioned) == 0 {
		t.Fatal("the skill names no tools")
	}
	for _, m := range mentioned {
		if !advertised[m[1]] {
			t.Errorf("the skill names %s, which the MCP server does not offer", m[1])
		}
	}
}

func TestInstallersCopyTheSkillThatExists(t *testing.T) {
	const skill = "plugins/workctl/skills/workctl/SKILL.md"
	if _, err := os.Stat(skill); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"scripts/install.sh", "scripts/install.ps1"} {
		b, err := os.ReadFile(script)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ReplaceAll(string(b), `\`, "/"), "plugins/workctl/skills/workctl/SKILL.md") {
			t.Errorf("%s does not copy %s", script, skill)
		}
	}
}
