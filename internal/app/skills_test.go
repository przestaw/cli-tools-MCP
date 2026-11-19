// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillsHome points every agent directory at a fresh temporary home.
func skillsHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	return home
}

func runSkillsCmd(args ...string) (string, string, error) {
	var out, errOut bytes.Buffer
	err := runSkills(&out, &errOut, args)
	return out.String(), errOut.String(), err
}

func TestSkillsCommand(t *testing.T) {
	home := skillsHome(t)
	claudeSkill := filepath.Join(home, ".claude/skills/workctl/SKILL.md")

	out, _, err := runSkillsCmd("status", "--target", "claude")
	if err != nil || !strings.Contains(out, "claude  compact-output  missing") || !strings.Contains(out, "claude  workctl         missing") {
		t.Fatalf("status of an empty home: %q %v", out, err)
	}
	if _, _, err := runSkillsCmd("status", "--check"); err == nil || !strings.Contains(err.Error(), "workctl skills install --target all") {
		t.Errorf("--check on missing skills: %v", err)
	}

	out, _, err = runSkillsCmd("install", "--target", "claude", "--dry-run")
	if err != nil || !strings.Contains(out, "would write 1 file(s)") {
		t.Fatalf("dry run: %q %v", out, err)
	}
	if _, err := os.Stat(claudeSkill); err == nil {
		t.Fatal("--dry-run wrote a file")
	}

	if _, _, err := runSkillsCmd("install", "--target", "all"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude/skills/compact-output")); err == nil {
		t.Error("compact-output installed by default")
	}
	// Optional skills that are not selected do not count for --check.
	if out, _, err := runSkillsCmd("status", "--check"); err != nil || strings.Count(out, "current") != 3 {
		t.Errorf("status after install: %q %v", out, err)
	}

	if err := os.WriteFile(claudeSkill, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err = runSkillsCmd("status", "--check", "--target", "claude")
	if err == nil || !strings.Contains(out, "differs") {
		t.Errorf("edited skill: %q %v", out, err)
	}
	if _, _, err = runSkillsCmd("status", "--target", "claude"); err != nil {
		t.Errorf("status without --check must exit 0: %v", err)
	}

	if _, _, err := runSkillsCmd("install", "--with", "compact-output", "--target", "claude"); err != nil {
		t.Fatal(err)
	}
	if out, _, err = runSkillsCmd("status", "--check", "--with", "compact-output", "--target", "claude"); err != nil || strings.Count(out, "current") != 2 {
		t.Errorf("after reinstall with compact-output: %q %v", out, err)
	}
}

func TestSkillsCommandRejects(t *testing.T) {
	skillsHome(t)
	for _, args := range [][]string{
		nil, {"remove"}, {"status", "--target", "emacs"}, {"install", "--with", "nope"},
		{"status", "extra"}, {"status", "--dry-run"}, {"install", "--check"},
	} {
		if _, _, err := runSkillsCmd(args...); err == nil {
			t.Errorf("%v accepted", args)
		}
	}
}

func TestSkillsWarnsAboutPluginDuplicate(t *testing.T) {
	home := skillsHome(t)
	if _, _, err := runSkillsCmd("install", "--target", "claude"); err != nil {
		t.Fatal(err)
	}
	if _, errOut, _ := runSkillsCmd("status", "--target", "claude"); errOut != "" {
		t.Errorf("warning without a plugin: %q", errOut)
	}
	list := filepath.Join(home, ".claude/plugins/installed_plugins.json")
	if err := os.MkdirAll(filepath.Dir(list), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(list, []byte(`{"version":2,"plugins":{"workctl@workctl":[{}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errOut, _ := runSkillsCmd("status", "--target", "claude"); !strings.Contains(errOut, "two skills named workctl") {
		t.Errorf("no warning: %q", errOut)
	}
}
