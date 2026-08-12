package cmd

import "github.com/spf13/cobra"

// Run executes the command tree with args (no process arguments, no os.Exit). Call it once per process.
func Run(args []string) error {
	if args == nil {
		args = []string{}
	}
	rootCmd.SetArgs(args)
	return rootCmd.Execute()
}

func initBuiltins() {
	rootCmd.InitDefaultHelpCmd()
	rootCmd.InitDefaultCompletionCmd()
}

// Resolve returns the canonical words of the command that args select (aliases followed, flags skipped), whether
// that command is only a group of subcommands (it cannot run itself, so running it prints help), and the arguments
// left after the command words. Nothing is run.
func Resolve(args []string) (path []string, group bool, rest []string, err error) {
	initBuiltins()
	c, rest, err := rootCmd.Find(args)
	if err != nil {
		return nil, false, nil, err
	}
	for x := c; x != nil && x.HasParent(); x = x.Parent() {
		path = append([]string{x.Name()}, path...)
	}
	return path, c.HasSubCommands() && !c.Runnable(), rest, nil
}

// Leaves lists every runnable command as its canonical words (a command can be runnable and have subcommands).
func Leaves() [][]string {
	initBuiltins()
	var out [][]string
	var walk func(c *cobra.Command, path []string)
	walk = func(c *cobra.Command, path []string) {
		if len(path) > 0 && c.Runnable() {
			out = append(out, path)
		}
		for _, s := range c.Commands() {
			walk(s, append(append([]string{}, path...), s.Name()))
		}
	}
	walk(rootCmd, nil)
	return out
}
