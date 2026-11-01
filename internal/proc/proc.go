// Package proc is the single place where workctl starts child processes (gh, glab and its own
// module subprocesses). Tests replace Command to run fakes instead of real tools.
package proc

import "os/exec"

// Command is exec.CommandContext.
var Command = exec.CommandContext
