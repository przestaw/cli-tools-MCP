// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"workctl/internal/azure/repl"
	"workctl/internal/clikit"
)

var shellCmd = clikit.Action("shell", "Interactive shell (navigate projects, repos, PRs, pipelines)", func() error {
	svc, err := newService()
	if err != nil {
		return err
	}
	repl.New(svc, printer()).Run()
	return nil
})

func init() {
	rootCmd.AddCommand(shellCmd)
}
