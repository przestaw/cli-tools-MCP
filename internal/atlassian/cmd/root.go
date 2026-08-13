// Package cmd implements the `atlassian` command tree.
package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
	"workctl/internal/atlassian/config"
	"workctl/internal/atlassian/output"
	"workctl/internal/atlassian/service"
)

// Global flag values (bound on the root command's persistent flags).
var (
	flagConfig   string
	flagProfile  string
	flagJSON     bool
	flagFields   []string
	flagInsecure bool
	flagQuiet    bool
	flagNoColor  bool
	flagExpand   bool
)

// Shared pagination flag values (registered per command that pages).
var (
	flagPage     int
	flagPageSize int
	flagAll      bool
)

// rootCmd is the base `atlassian` command.
var rootCmd = &cobra.Command{
	Use:           "atlassian",
	Short:         "Interactive CLI for Atlassian Jira & Confluence",
	Long:          "atlassian is a command-line interface for Atlassian Cloud: Jira (REST API v3, Agile) and Confluence (v2).\nConfigure it with `atlassian config init`, then explore with subcommands or `atlassian shell`.",
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
	return service.New(cfg, service.Overrides{Profile: flagProfile, Insecure: flagInsecure})
}

// printer returns an output.Printer honoring --json / --fields / --no-color.
// Colors are enabled only on a real terminal and when not piping or --json.
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

// addPagingFlags registers the shared --page/--page-size/--all flags.
func addPagingFlags(cmd *cobra.Command, defaultSize int) {
	cmd.Flags().IntVar(&flagPage, "page", 1, "page number")
	cmd.Flags().IntVar(&flagPageSize, "page-size", defaultSize, "items per page")
	cmd.Flags().BoolVar(&flagAll, "all", false, "fetch all pages")
}
