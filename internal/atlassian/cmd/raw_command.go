package cmd

import (
	"context"
	"encoding/json"
	"workctl/internal/clikit"
)

var rawCmd = clikit.Raw("raw <path>",
	"Sends an arbitrary request against the site root and prints the JSON response.\n"+
		"Paths: Jira /rest/api/3/..., Agile /rest/agile/1.0/..., Confluence /wiki/api/v2/... or /wiki/rest/api/...", false,
	func(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
		svc, err := newService()
		if err != nil {
			return nil, err
		}
		return svc.Client.Raw(ctx, method, path, body)
	}, func(data []byte) error { return printer().Raw(data) })

func init() { rootCmd.AddCommand(rawCmd) }
