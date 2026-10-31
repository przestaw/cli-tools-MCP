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
