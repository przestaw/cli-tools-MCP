// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import "workctl/internal/clikit"

// Run executes the Atlassian (Jira and Confluence) command tree with the given arguments.
func Run(args []string) error { return clikit.Run(rootCmd, args) }

// Resolve finds the command the arguments select without running it: its canonical path, whether it is only a
// group, and the arguments left over. The modules package uses it to classify commands as read or write.
func Resolve(args []string) ([]string, bool, []string, error) { return clikit.Resolve(rootCmd, args) }

// Leaves lists every runnable command as a path of words; tests use it to check the read/write classification.
func Leaves() [][]string { return clikit.Leaves(rootCmd) }
