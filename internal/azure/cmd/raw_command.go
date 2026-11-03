// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"context"
	"encoding/json"
	"workctl/internal/clikit"
)

var rawCmd = clikit.Raw("raw <path|url>",
	"Sends an arbitrary request and prints the JSON response.\n"+
		"A leading-slash path is relative to the org root (e.g. /<project>/_apis/git/repositories?api-version=7.1);\n"+
		"a full https:// URL is used verbatim (use this for the vsrm host).", true,
	func(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
		svc, err := newService()
		if err != nil {
			return nil, err
		}
		return svc.Client.Raw(ctx, method, path, body)
	}, func(data []byte) error { return printer().Raw(data) })

func init() { rootCmd.AddCommand(rawCmd) }
