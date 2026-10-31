package cmd

import (
	"workctl/internal/atlassian/repl"
	"workctl/internal/clikit"
)

var shellCmd = clikit.Action("shell", "Interactive shell (navigate projects/issues and spaces/pages)", func() error {
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
