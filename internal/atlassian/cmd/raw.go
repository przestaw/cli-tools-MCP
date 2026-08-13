package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var (
	rawMethod string
	rawData   string
)

var rawCmd = &cobra.Command{
	Use:   "raw <path>",
	Short: "Send a raw request and print the JSON (debug/probe)",
	Long: "Sends an arbitrary request against the site root and prints the JSON response.\n" +
		"Paths: Jira /rest/api/3/..., Agile /rest/agile/1.0/..., Confluence /wiki/api/v2/... or /wiki/rest/api/...",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		path := args[0]
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		var body any
		if rawData != "" {
			var v any
			if err := json.Unmarshal([]byte(rawData), &v); err != nil {
				return fmt.Errorf("invalid --data JSON: %w", err)
			}
			body = v
		}
		raw, err := svc.Client.Raw(c, strings.ToUpper(rawMethod), path, body)
		if err != nil {
			return err
		}
		return printer().Raw(raw)
	},
}

func init() {
	rawCmd.Flags().StringVarP(&rawMethod, "method", "X", "GET", "HTTP method")
	rawCmd.Flags().StringVarP(&rawData, "data", "d", "", "JSON request body")
	rootCmd.AddCommand(rawCmd)
}
