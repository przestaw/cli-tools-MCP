// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/skills"
)

const skillsUsage = "usage: workctl skills status|install [--target vscode|claude|codex|all] [--with compact-output] [--dry-run] [--check]"

// skillsCommand is `workctl skills status|install`: it works on the skills embedded in this binary and needs no profile.
func skillsCommand(_ *config.Config, args []string) error {
	return runSkills(os.Stdout, os.Stderr, args)
}

func runSkills(out, errOut io.Writer, args []string) error {
	if len(args) == 0 || (args[0] != "status" && args[0] != "install") {
		return errors.New(skillsUsage)
	}
	action := args[0]
	fs := flag.NewFlagSet("skills "+action, flag.ContinueOnError)
	target := fs.String("target", "all", "vscode, claude, codex or all (comma separated)")
	with := fs.String("with", "", "optional skills to include, comma separated (compact-output)")
	dryRun := fs.Bool("dry-run", false, "install: show what would be written, write nothing")
	check := fs.Bool("check", false, "status: exit 1 when a selected skill is missing or differs")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 || (action == "status" && *dryRun) || (action == "install" && *check) {
		return errors.New(skillsUsage)
	}
	targets, err := skills.Targets(cliutil.SplitCommaList(*target))
	if err != nil {
		return err
	}
	src := skills.Embedded()
	selected, err := skills.Select(src, cliutil.SplitCommaList(*with))
	if err != nil {
		return err
	}

	problems := 0
	for _, t := range targets {
		if action == "install" {
			files, err := skills.Install(src, t, selected, *dryRun)
			if err != nil {
				return fmt.Errorf("%s: %w", t.Name, err)
			}
			verb := "wrote"
			if *dryRun {
				verb = "would write"
			}
			fmt.Fprintf(out, "%-7s %s %d file(s) in %s\n", t.Name, verb, len(files), t.Dir)
			continue
		}
		entries, err := skills.Status(src, t)
		if err != nil {
			return fmt.Errorf("%s: %w", t.Name, err)
		}
		for _, e := range entries {
			fmt.Fprintf(out, "%-7s %-15s %-8s %s\n", e.Target, e.Skill, e.State, e.Path)
			if e.State != skills.Current && slices.Contains(selected, e.Skill) {
				problems++
			}
		}
	}
	warnPluginDuplicate(errOut, targets)
	if problems > 0 && *check {
		return fmt.Errorf("%d skill(s) missing or outdated; fix: workctl skills install --target %s", problems, *target)
	}
	return nil
}

// warnPluginDuplicate points out two skills named workctl in Claude Code: one from the plugin, one copied by us.
func warnPluginDuplicate(w io.Writer, targets []skills.Target) {
	for _, t := range targets {
		if t.Name != "claude" || !skills.ClaudePluginInstalled(filepath.Dir(t.Dir)) {
			continue
		}
		if _, err := os.Stat(filepath.Join(t.Dir, "workctl")); err == nil {
			fmt.Fprintf(w, "warning: Claude Code has the workctl plugin and the copied skill %s: two skills named workctl; keep one (remove the directory, or `claude plugin uninstall workctl@workctl`)\n", filepath.Join(t.Dir, "workctl"))
		}
	}
}
