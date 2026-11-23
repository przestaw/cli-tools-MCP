// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package skills installs the agent skills embedded in the binary into the skills directories of the supported
// agents and tells whether an installed copy is still the one this binary carries. It never deletes anything and
// touches only the directories of the embedded skills.
package skills

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"workctl/plugins"
)

// State of one skill in one target.
type State string

// The states Status reports.
const (
	Missing State = "missing" // no directory of that skill in the target
	Current State = "current" // every embedded file is there with the same content
	Differs State = "differs" // the directory exists but a file is absent or has other content
)

// DefaultSkills are installed when no other skill is asked for; the others are optional.
var DefaultSkills = []string{"workctl"}

// Target is an agent and the directory it reads skills from.
type Target struct {
	Name string
	Dir  string
}

// Entry is the state of one skill in one target.
type Entry struct {
	Target string
	Skill  string
	State  State
	Path   string
}

// Embedded is the skills tree of the binary: one directory per skill.
func Embedded() fs.FS {
	sub, err := fs.Sub(plugins.Skills, "workctl/skills")
	if err != nil {
		panic(err) // the embedded path is fixed at build time
	}
	return sub
}

// TargetNames are the valid values of --target besides "all".
var TargetNames = []string{"vscode", "claude", "codex"}

// Targets resolves the skills directories of the named targets ("all" means every one) under the current home.
func Targets(names []string) ([]Target, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	codex := os.Getenv("CODEX_HOME")
	if codex == "" {
		codex = filepath.Join(home, ".codex")
	}
	dirs := map[string]string{
		"vscode": filepath.Join(home, ".copilot", "skills"),
		"claude": filepath.Join(home, ".claude", "skills"),
		"codex":  filepath.Join(codex, "skills"),
	}
	if slices.Contains(names, "all") {
		names = TargetNames
	}
	var out []Target
	for _, n := range names {
		dir, ok := dirs[n]
		if !ok {
			return nil, fmt.Errorf("unknown target %q (use %s or all)", n, strings.Join(TargetNames, ", "))
		}
		if !slices.ContainsFunc(out, func(t Target) bool { return t.Name == n }) {
			out = append(out, Target{Name: n, Dir: dir})
		}
	}
	return out, nil
}

// Names lists the skills of src, sorted.
func Names(src fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// Select returns the default skills plus the extra ones, rejecting names src does not have.
func Select(src fs.FS, extra []string) ([]string, error) {
	have, err := Names(src)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range append(slices.Clone(DefaultSkills), extra...) {
		if !slices.Contains(have, n) {
			return nil, fmt.Errorf("unknown skill %q (available: %s)", n, strings.Join(have, ", "))
		}
		if !slices.Contains(out, n) {
			out = append(out, n)
		}
	}
	return out, nil
}

// skillFiles lists the files of one skill as paths relative to the skill directory.
func skillFiles(src fs.FS, name string) ([]string, error) {
	var files []string
	err := fs.WalkDir(src, name, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, strings.TrimPrefix(p, name+"/"))
		}
		return err
	})
	return files, err
}

// stale returns the files of the skill that are absent from dir or have other content.
func stale(src fs.FS, name, dir string) ([]string, error) {
	files, err := skillFiles(src, name)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		want, err := fs.ReadFile(src, name+"/"+f)
		if err != nil {
			return nil, err
		}
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(f)))
		if err != nil || !bytes.Equal(got, want) {
			out = append(out, f)
		}
	}
	return out, nil
}

// Status reports every skill of src in the target.
func Status(src fs.FS, t Target) ([]Entry, error) {
	names, err := Names(src)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, n := range names {
		dir := filepath.Join(t.Dir, n)
		e := Entry{Target: t.Name, Skill: n, State: Missing, Path: dir}
		if _, err := os.Stat(dir); err == nil {
			diff, err := stale(src, n, dir)
			if err != nil {
				return nil, err
			}
			e.State = Current
			if len(diff) > 0 {
				e.State = Differs
			}
		}
		out = append(out, e)
	}
	return out, nil
}

// Install copies the named skills into the target and returns the files it wrote (or would write with dryRun).
// Files that already have the right content are left alone, so a second run changes nothing.
func Install(src fs.FS, t Target, names []string, dryRun bool) ([]string, error) {
	var written []string
	for _, n := range names {
		dir := filepath.Join(t.Dir, n)
		files, err := stale(src, n, dir)
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			path := filepath.Join(dir, filepath.FromSlash(f))
			written = append(written, path)
			if dryRun {
				continue
			}
			data, err := fs.ReadFile(src, n+"/"+f)
			if err != nil {
				return nil, err
			}
			if err := writeAtomic(path, data); err != nil {
				return nil, err
			}
		}
	}
	return written, nil
}

// writeAtomic writes through a temporary file in the same directory and renames it into place.
func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".workctl-skill-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // fails harmlessly after a successful rename
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ClaudePluginInstalled reports whether Claude Code lists the workctl plugin as installed. claudeDir is the
// Claude Code directory (~/.claude, the parent of the claude target); the "plugins" keys of
// plugins/installed_plugins.json look like "workctl@marketplace".
func ClaudePluginInstalled(claudeDir string) bool {
	b, err := os.ReadFile(filepath.Join(claudeDir, "plugins", "installed_plugins.json"))
	if err != nil {
		return false
	}
	var data struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if json.Unmarshal(b, &data) != nil {
		return false
	}
	for key := range data.Plugins {
		if strings.HasPrefix(key, "workctl@") {
			return true
		}
	}
	return false
}
