package cmd

import "workctl/internal/clikit"

func Run(args []string) error                                 { return clikit.Run(rootCmd, args) }
func Resolve(args []string) ([]string, bool, []string, error) { return clikit.Resolve(rootCmd, args) }
func Leaves() [][]string                                      { return clikit.Leaves(rootCmd) }
