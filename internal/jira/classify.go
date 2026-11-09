// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package jira

import (
	"flag"
	"fmt"
	"io"
	"strings"

	"workctl/internal/api"
)

// commands are the `workctl jira` command words; "comment" is a group whose only command is "comment add".
var commands = [][]string{
	{"issue"}, {"search"}, {"comments"}, {"transitions"}, {"transition"}, {"comment", "add"},
	{"myself"}, {"server-info"}, {"raw"},
}

// ReadCommands are the `workctl jira` commands that only read; `raw` is classified by its method (see RawIsWrite).
var ReadCommands = []string{"issue", "search", "comments", "transitions", "myself", "server-info"}

// Resolve maps a `workctl jira` command line to its command words, the rest of the arguments and whether it names
// only a group, for the agent surface's read/write classification.
func Resolve(args []string) (path []string, group bool, rest []string, err error) {
	if len(args) == 0 {
		return nil, true, nil, nil
	}
	for _, c := range commands {
		if args[0] != c[0] {
			continue
		}
		if len(c) == 1 {
			return c, false, args[1:], nil
		}
		if len(args) > 1 && args[1] == c[1] {
			return c, false, args[2:], nil
		}
		return c[:1], true, args[1:], nil
	}
	return nil, false, nil, fmt.Errorf("unknown jira command %q", args[0])
}

// Leaves lists every `workctl jira` command.
func Leaves() [][]string { return commands }

// RawIsWrite classifies `jira raw [--body X] [--query K=V]... METHOD PATH`: GET and HEAD without a body read,
// everything else writes. On the strict surface PATH must start with a single "/", so the request stays on the
// profile's host.
func RawIsWrite(rest []string, strict bool) (bool, error) {
	fs := flag.NewFlagSet("raw", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	body := fs.String("body", "", "")
	api.QueryFlag(fs)
	if err := fs.Parse(rest); err != nil {
		return false, fmt.Errorf("jira raw: %w", err)
	}
	if fs.NArg() != 2 {
		return false, fmt.Errorf("jira raw: want [--body JSON|@file] [--query KEY=VALUE]... METHOD PATH")
	}
	path := fs.Arg(1)
	if strict && (!strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//")) {
		return false, fmt.Errorf("jira raw: the path must start with / (full URLs are not allowed here)")
	}
	return *body != "" || !api.IsReadMethod(strings.ToUpper(fs.Arg(0))), nil
}
