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
	Use:   "raw <path|url>",
	Short: "Send a raw request and print the JSON (debug/probe)",
	Long: "Sends an arbitrary request and prints the JSON response.\n" +
		"A leading-slash path is relative to the org root (e.g. /<project>/_apis/git/repositories?api-version=7.1);\n" +
		"a full https:// URL is used verbatim (use this for the vsrm host).",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newService()
		if err != nil {
			return err
		}
		c, cancel := ctx()
		defer cancel()
		path := args[0]
		if !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "http") {
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
