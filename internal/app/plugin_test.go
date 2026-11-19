// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Keeps the agent package (plugins/workctl, the marketplace, the installers) in step with the code:
// the SKILL.md tool names are checked against mcpTools and the installer must use the embedded skills.

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"

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
	mentioned := regexp.MustCompile("`((?:(?:github|gitlab|jira|task|pr)_[a-z_]+)|accounts)`").FindAllStringSubmatch(skill, -1)
	if len(mentioned) == 0 {
		t.Fatal("the skill names no tools")
	}
	for _, m := range mentioned {
		if !advertised[m[1]] {
			t.Errorf("the skill names %s, which the MCP server does not offer", m[1])
		}
	}
}

func TestInstallerUsesTheEmbeddedSkills(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(testutil.RepoRoot(t), "scripts/install.py"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"skills", "install", "--target"`) || strings.Contains(string(b), "copyfile") {
		t.Error("scripts/install.py must install skills with `workctl skills install`, not copy files itself")
	}
}

func TestSkillFrontMatter(t *testing.T) {
	root := testutil.RepoRoot(t)
	files, err := filepath.Glob(filepath.Join(root, "plugins/workctl/skills/*/SKILL.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no skills found: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkSkillFrontMatter(string(b), filepath.Base(filepath.Dir(f))); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
}

var skillNameRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

// checkSkillFrontMatter parses the YAML front matter of a SKILL.md and checks the values, not their quoting.
func checkSkillFrontMatter(doc, dir string) error {
	lines := strings.Split(strings.ReplaceAll(doc, "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != "---" {
		return errors.New("missing front matter")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return errors.New("unterminated front matter")
	}
	var fm struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &fm); err != nil {
		return fmt.Errorf("front matter is not valid YAML: %w", err)
	}
	if fm.Name != dir {
		return fmt.Errorf("name %q must equal the directory %q", fm.Name, dir)
	}
	if !skillNameRe.MatchString(fm.Name) {
		return fmt.Errorf("name %q must be 1-64 lowercase letters, digits and hyphens", fm.Name)
	}
	if n := utf8.RuneCountInString(fm.Description); n == 0 || n > 1024 {
		return fmt.Errorf("description must be 1-1024 characters, has %d", n)
	}
	return nil
}

func TestCheckSkillFrontMatter(t *testing.T) {
	fm := func(name, desc string) string {
		return "---\nname: " + name + "\ndescription: " + desc + "\n---\nbody\n"
	}
	pl := strings.Repeat("ł", 1024)
	cases := []struct {
		name, doc, dir string
		ok             bool
	}{
		{"single quoted", fm("workctl", "'Use it. It''s fine.'"), "workctl", true},
		{"double quoted with escaped quote", fm("workctl", `"Say \"hi\" here"`), "workctl", true},
		{"plain scalar", fm("workctl", "Plain text description"), "workctl", true},
		{"folded block", "---\nname: workctl\ndescription: >\n  first line\n  second line\n---\n", "workctl", true},
		{"1024 characters over 1024 bytes", fm("workctl", "'"+pl+"'"), "workctl", true},
		{"1025 characters", fm("workctl", "'"+pl+"x'"), "workctl", false},
		{"missing front matter", "# Title\n", "workctl", false},
		{"missing name", "---\ndescription: text\n---\n", "workctl", false},
		{"name differs from directory", fm("other", "text"), "workctl", false},
		{"uppercase in name", fm("Workctl", "text"), "Workctl", false},
		{"space in name", fm("'work ctl'", "text"), "work ctl", false},
		{"empty description", fm("workctl", "''"), "workctl", false},
		{"unbalanced quote", fm("workctl", "'unclosed"), "workctl", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := checkSkillFrontMatter(c.doc, c.dir); (err == nil) != c.ok {
				t.Errorf("err = %v, want ok=%v", err, c.ok)
			}
		})
	}
}

func TestVersionsAgree(t *testing.T) {
	root := testutil.RepoRoot(t)
	b, err := os.ReadFile(filepath.Join(root, "internal/api/client.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"User-Agent", "workctl/`+Version+`"`) {
		t.Errorf("the User-Agent in internal/api/client.go must be workctl/%s", Version)
	}
}
