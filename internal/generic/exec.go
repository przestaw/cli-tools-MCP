// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package generic

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"workctl/internal/config"
	"workctl/internal/proc"
)

// allowedCommand returns the entry of profile.Commands whose prefix starts argv, or an error when none does.
func allowedCommand(name string, profile config.Profile, argv []string) (config.SSHCommand, error) {
	for _, c := range profile.Commands {
		if len(argv) >= len(c.Prefix) && slices.Equal(argv[:len(c.Prefix)], c.Prefix) {
			return c, nil
		}
	}
	return config.SSHCommand{}, fmt.Errorf("command %q is not allowed by profiles.%s.commands", strings.Join(argv, " "), name)
}

// shellQuote quotes one word for the remote shell. ssh joins its arguments into one command line that the remote
// side parses again, so without quoting an argument like "x; rm -rf ~" would run a second command.
func shellQuote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// runSSH runs one allowed command of an ssh-exec profile and returns its stdout. The write policy applies unless the
// matched prefix is read_only. No workctl secret is involved: ssh authenticates with the user's key.
func runSSH(ctx context.Context, cfg *config.Config, name string, profile config.Profile, argv []string) ([]byte, error) {
	allowed, err := allowedCommand(name, profile, argv)
	if err != nil {
		return nil, err
	}
	if !allowed.ReadOnly {
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.ProfileTimeout(profile))
	defer cancel()
	args := []string{"-o", "BatchMode=yes", "-o", "LogLevel=ERROR", "-T", "--", profile.SSHDestination}
	for _, word := range argv {
		args = append(args, shellQuote(word))
	}
	cmd := proc.Command(ctx, "ssh", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	switch {
	case err == nil:
		return stdout.Bytes(), nil
	case ctx.Err() != nil:
		return nil, fmt.Errorf("ssh %s: %w", profile.SSHDestination, ctx.Err())
	case errors.Is(err, exec.ErrNotFound):
		return nil, fmt.Errorf("ssh not found in PATH: install the OpenSSH client")
	}
	msg := strings.TrimSpace(stderr.String())
	if len(msg) > 2000 {
		msg = msg[:2000] + "..."
	}
	if msg == "" {
		msg = err.Error()
	}
	return nil, fmt.Errorf("ssh %s: %s", profile.SSHDestination, msg)
}

// sshExecProfile picks the api profile of an exec call and checks that it is an ssh-exec one.
func sshExecProfile(cfg *config.Config) (string, config.Profile, error) {
	name, profile, err := cfg.ProfileOf(Kind)
	if err != nil {
		return "", profile, err
	}
	if !profile.UsesSSHExec() {
		return "", profile, fmt.Errorf("profile %q is not an ssh-exec profile: use `workctl api raw` for HTTP", name)
	}
	return name, profile, nil
}
