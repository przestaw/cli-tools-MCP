package clikit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func Root(name, short, long string) *cobra.Command {
	return &cobra.Command{Use: name, Short: short, Long: long, SilenceUsage: true, SilenceErrors: true}
}

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

func Action(name, short string, run func() error) *cobra.Command {
	return &cobra.Command{Use: name, Short: short, RunE: func(*cobra.Command, []string) error { return run() }}
}

func Version(name string, version *string) *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print " + name + " version", RunE: func(cmd *cobra.Command, _ []string) error {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", name, *version)
		return err
	}}
}

func Context() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

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
