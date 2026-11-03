// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package clikit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Root creates the root command of a CLI. Cobra prints neither usage nor errors itself; the caller decides.
func Root(name, short, long string) *cobra.Command {
	return &cobra.Command{Use: name, Short: short, Long: long, SilenceUsage: true, SilenceErrors: true}
}

// Run executes root with the given arguments.
func Run(root *cobra.Command, args []string) error {
	if args == nil {
		args = []string{}
	}
	root.SetArgs(args)
	return root.Execute()
}

func initBuiltins(root *cobra.Command) {
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()
}

// Resolve finds the command the arguments select, without running it: its canonical path (aliases resolved),
// whether it is only a group of subcommands, and the arguments left over.
func Resolve(root *cobra.Command, args []string) (path []string, group bool, rest []string, err error) {
	initBuiltins(root)
	cmd, rest, err := root.Find(args)
	if err != nil {
		return nil, false, nil, err
	}
	for current := cmd; current != nil && current.HasParent(); current = current.Parent() {
		path = append([]string{current.Name()}, path...)
	}
	return path, cmd.HasSubCommands() && !cmd.Runnable(), rest, nil
}

// Leaves lists every runnable command as a path of words, including cobra's built-in help and completion.
func Leaves(root *cobra.Command) [][]string {
	initBuiltins(root)
	var out [][]string
	var walk func(*cobra.Command, []string)
	walk = func(cmd *cobra.Command, path []string) {
		if len(path) > 0 && cmd.Runnable() {
			out = append(out, path)
		}
		for _, child := range cmd.Commands() {
			walk(child, append(append([]string{}, path...), child.Name()))
		}
	}
	walk(root, nil)
	return out
}

// Action makes a command without arguments that just calls run.
func Action(name, short string, run func() error) *cobra.Command {
	return &cobra.Command{Use: name, Short: short, RunE: func(*cobra.Command, []string) error { return run() }}
}

// Version makes the `version` command, which prints "name version".
func Version(name string, version *string) *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print " + name + " version", RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", name, *version)
		return err
	}}
}

// Context returns the context, with a 60-second timeout, that command handlers run under.
func Context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// Raw makes the `raw` debug command: it sends one request through call and prints the JSON answer.
// Absolute http(s) URLs are accepted only when absolute is set.
func Raw(use, long string, absolute bool, call func(context.Context, string, string, any) (json.RawMessage, error), print func([]byte) error) *cobra.Command {
	method, data := "GET", ""
	cmd := &cobra.Command{
		Use: use, Short: "Send a raw request and print the JSON (debug/probe)", Long: long,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			path := args[0]
			absoluteURL := absolute && strings.HasPrefix(path, "http")
			if !strings.HasPrefix(path, "/") && !absoluteURL {
				path = "/" + path
			}
			var body any
			if data != "" {
				if err := json.Unmarshal([]byte(data), &body); err != nil {
					return fmt.Errorf("invalid --data JSON: %w", err)
				}
			}
			ctx, cancel := Context()
			defer cancel()
			result, err := call(ctx, strings.ToUpper(method), path, body)
			if err != nil {
				return err
			}
			return print(result)
		},
	}
	cmd.Flags().StringVarP(&method, "method", "X", "GET", "HTTP method")
	cmd.Flags().StringVarP(&data, "data", "d", "", "JSON request body")
	return cmd
}
