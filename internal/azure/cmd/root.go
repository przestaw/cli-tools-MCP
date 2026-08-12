// Package cmd implements the `azure` command tree.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"workctl/internal/azure/config"
	"workctl/internal/azure/output"
	"workctl/internal/azure/service"
)

// Global flag values (bound on the root command's persistent flags).
var (
	flagConfig   string
	flagProfile  string
	flagProject  string
	flagJSON     bool
	flagFields   []string
	flagInsecure bool
	flagQuiet    bool
	flagNoColor  bool
	flagExpand   bool
)

// Shared list flag values (registered per command that lists).
var (
	flagAll bool
	flagTop int
)

// rootCmd is the base `azure` command.
var rootCmd = &cobra.Command{
	Use:           "azure",
	Short:         "Interactive CLI for Azure DevOps (repos, pipelines, pull requests)",
	Long:          "azure is a command-line interface for Azure DevOps: Git repositories, pull requests, Pipelines, Build and Release.\nConfigure it with `azure config init`, then explore with subcommands or `azure shell`.",
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command. Called by main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.StringVar(&flagConfig, "config", "", "config file path (default: OS config dir)")
	pf.StringVar(&flagProfile, "profile", "", "config profile to use (default: current_profile)")
	pf.StringVarP(&flagProject, "project", "p", "", "team project (default: profile project)")
	pf.BoolVar(&flagJSON, "json", false, "output raw JSON")
	pf.StringSliceVar(&flagFields, "fields", nil, "table columns to show (comma-separated)")
	pf.BoolVar(&flagInsecure, "insecure", false, "skip TLS certificate verification")
	pf.BoolVarP(&flagQuiet, "quiet", "q", false, "suppress status messages")
	pf.BoolVar(&flagNoColor, "no-color", false, "disable ANSI colors")
	pf.BoolVarP(&flagExpand, "expand", "e", false, "do not truncate long values")
}

// loadConfig loads the config from the resolved path.
func loadConfig() (*config.Config, error) {
	return config.Load(flagConfig)
}

// newService builds a configured Service from the active profile.
func newService() (*service.Service, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	return service.New(cfg, service.Overrides{
		Profile:  flagProfile,
		Project:  flagProject,
		Insecure: flagInsecure,
	})
}

// printer returns an output.Printer honoring --json / --fields / --no-color.
func printer() *output.Printer {
	p := output.New(flagJSON, flagFields)
	p.Color = !flagNoColor && !flagJSON && term.IsTerminal(int(os.Stdout.Fd()))
	p.Expand = flagExpand
	return p
}

// ctx returns a context with a sensible default timeout for one operation.
func ctx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 60*time.Second)
}

// addListFlags registers the shared --all / --top flags.
func addListFlags(cmd *cobra.Command, defaultTop int) {
	cmd.Flags().BoolVar(&flagAll, "all", false, "fetch all pages (follow continuation tokens)")
	cmd.Flags().IntVar(&flagTop, "top", defaultTop, "max items to fetch (0 = server default)")
}
