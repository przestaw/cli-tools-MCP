// Keeps the agent package (plugins/workctl, the marketplace, the installers) in step with the code:
// the SKILL.md tool names are checked against mcpTools and the installers must copy the skill that exists.

package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"workctl/internal/config"
	"workctl/internal/testutil"
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
	root := testutil.RepoRoot(t)
	var plugin struct{ Name, Version string }
	readJSON(t, filepath.Join(root, "plugins/workctl/.claude-plugin/plugin.json"), &plugin)
	if plugin.Name != "workctl" || plugin.Version != Version {
		t.Fatalf("plugin.json = %+v, want name workctl and version %s", plugin, Version)
	}

	var market struct {
		Name    string
		Plugins []struct{ Name, Source string }
	}
	readJSON(t, filepath.Join(root, ".claude-plugin/marketplace.json"), &market)
	if len(market.Plugins) != 1 || market.Plugins[0].Name != plugin.Name {
		t.Fatalf("marketplace = %+v: the entry name must equal the manifest name", market)
	}
	src := market.Plugins[0].Source
	if !strings.HasPrefix(src, "./") || strings.Contains(src, "..") {
		t.Fatalf("marketplace source %q must be a path inside the repository", src)
	}
	if _, err := os.Stat(filepath.Join(root, src, ".claude-plugin", "plugin.json")); err != nil {
		t.Fatalf("marketplace source %q: %v", src, err)
	}

	var mcpManifest struct {
		McpServers map[string]struct {
			Command string
			Args    []string
		}
	}
	readJSON(t, filepath.Join(root, "plugins/workctl/.mcp.json"), &mcpManifest)
	if s := mcpManifest.McpServers["workctl"]; s.Command != "workctl" || strings.Join(s.Args, " ") != "mcp" {
		t.Fatalf(".mcp.json server = %+v, want `workctl mcp`", s)
	}
}

func TestSkillNamesOnlyExistingMCPTools(t *testing.T) {
	root := testutil.RepoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "plugins/workctl/skills/workctl/SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(b)
	front := regexp.MustCompile(`(?s)^---\nname: (\S+)\ndescription: '(.*?)'\n---\n`).FindStringSubmatch(skill)
	if front == nil || front[1] != "workctl" || len(front[2]) > 1024 {
		t.Fatalf("front matter: name must be workctl (the folder name) and the description at most 1024 characters: %q", front)
	}

	// Every backend and write access on, so that every tool is advertised.
	cfg := config.Default()
	cfg.Policy.AllowWrite = true
	for _, kind := range []string{"github", "gitlab", "jira", "azure"} {
		cfg.Profiles[kind] = config.Profile{Kind: kind, URL: "https://" + kind + ".example.test"}
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
	root := testutil.RepoRoot(t)
	skill := filepath.Join(root, "plugins/workctl/skills/workctl/SKILL.md")
	if _, err := os.Stat(skill); err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"scripts/install.sh", "scripts/install.ps1"} {
		b, err := os.ReadFile(filepath.Join(root, script))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.ReplaceAll(string(b), `\`, "/"), "plugins/workctl/skills/workctl/SKILL.md") {
			t.Errorf("%s does not copy plugins/workctl/skills/workctl/SKILL.md", script)
		}
	}
}
