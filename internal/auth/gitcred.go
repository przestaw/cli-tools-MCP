// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package auth

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/proc"
)

// GitCredential is a git credential helper: git gets the profile token straight from the keyring (or from gh).
// With --profile it answers for that profile; without it the profile is chosen from the host and, when git sends
// it (credential.useHttpPath), the repository path, by the same repos/projects matching as every other call.
// When nothing matches it stays silent so other helpers can answer.
func GitCredential(cfg *config.Config, args []string, in io.Reader, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: workctl [--profile NAME] git-credential get")
	}
	if args[0] != "get" {
		return nil // store/erase: the token is managed with `workctl auth`
	}
	req := map[string]string{}
	sc := bufio.NewScanner(in)
	for sc.Scan() && sc.Text() != "" {
		k, v, _ := strings.Cut(sc.Text(), "=")
		req[k] = v
	}
	if req["protocol"] != "https" {
		return nil
	}

	name := cfg.ProfileName()
	var profile config.Profile
	if name != "" {
		var ok bool
		if profile, ok = cfg.Profiles[name]; !ok || !config.PRKinds[profile.Kind] {
			return fmt.Errorf("git-credential needs a github, gitlab or azure profile")
		}
		// api.github.com serves the API; git talks to github.com.
		if !strings.EqualFold(req["host"], profileHost(profile)) {
			return nil
		}
	} else {
		var err error
		if name, profile, err = gitProfileFor(cfg, req["host"], req["path"]); err != nil || name == "" {
			return err
		}
	}

	token, err := gitToken(cfg, name, profile, req["host"])
	if err != nil {
		return err
	}
	user := map[string]string{"github": "x-access-token", "gitlab": "oauth2"}[profile.Kind]
	_, err = fmt.Fprintf(out, "username=%s\npassword=%s\n", cliutil.FirstNonEmpty(user, "workctl"), token)
	return err
}

// gitProfileFor picks the profile for a git host and repository path: the best repos/projects match among the profiles
// of that host, else the only profile there, else the per-kind default. It returns "" when none applies.
func gitProfileFor(cfg *config.Config, host, path string) (string, config.Profile, error) {
	var onHost []string
	for n, p := range cfg.Profiles {
		if config.PRKinds[p.Kind] && !p.UsesSSHExec() && strings.EqualFold(profileHost(p), host) {
			onHost = append(onHost, n)
		}
	}
	sort.Strings(onHost)
	if len(onHost) == 0 {
		return "", config.Profile{}, nil
	}

	if repo := strings.TrimSuffix(strings.Trim(path, "/"), ".git"); repo != "" {
		best, bestScore := []string{}, 0
		for _, n := range onHost {
			switch score := cfg.Profiles[n].MatchResource(repo); {
			case score > bestScore:
				best, bestScore = []string{n}, score
			case score == bestScore && score > 0:
				best = append(best, n)
			}
		}
		if len(best) > 1 {
			return "", config.Profile{}, fmt.Errorf("several profiles match %s on %s (%s): bind the repository to one of them", repo, host, strings.Join(best, ", "))
		}
		if len(best) == 1 {
			return best[0], cfg.Profiles[best[0]], nil
		}
	}

	if len(onHost) == 1 {
		return onHost[0], cfg.Profiles[onHost[0]], nil
	}
	for _, n := range onHost {
		if cfg.Defaults[cfg.Profiles[n].Kind] == n {
			return n, cfg.Profiles[n], nil
		}
	}
	return "", config.Profile{}, nil
}

// gitToken returns the token for git: the keyring or token_env value, or, for a cli profile, the one gh holds for the
// account the profile is bound to.
func gitToken(cfg *config.Config, name string, profile config.Profile, host string) (string, error) {
	if !profile.UsesCLI() {
		return config.ResolveProfileSecret(name, profile)
	}
	if profile.Kind != "github" {
		return "", fmt.Errorf("profile %q: git credentials through %s are not supported, use a token profile", name, config.CLITools[profile.Kind])
	}
	args := []string{"auth", "token", "--hostname", host}
	if profile.Username != "" {
		args = append(args, "--user", profile.Username)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ProfileTimeout(profile))
	defer cancel()
	cmd := proc.Command(ctx, "gh", args...)
	cmd.Env = append(cmd.Environ(), "GH_PROMPT_DISABLED=1", "NO_COLOR=1")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("profile %q: gh auth token failed (run `workctl auth login --profile %s`): %w", name, name, err)
	}
	if token := strings.TrimSpace(string(out)); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("profile %q: gh returned no token", name)
}

// setupGitCommand points git at `workctl git-credential` for the host of every pull request profile, so plain
// git (and tools that run it, such as GitKraken and GitLens) authenticates as the profile that owns the repository.
func setupGitCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth setup-git", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "print the git config commands and change nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	hosts := map[string]bool{}
	for _, p := range cfg.Profiles {
		if config.PRKinds[p.Kind] && !p.UsesSSHExec() {
			if h := profileHost(p); h != "" {
				hosts[h] = true
			}
		}
	}
	if len(hosts) == 0 {
		return fmt.Errorf("no github, gitlab or azure profile is configured")
	}
	names := make([]string, 0, len(hosts))
	for h := range hosts {
		names = append(names, h)
	}
	sort.Strings(names)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, host := range names {
		section := "credential.https://" + host
		steps := [][]string{
			{"--global", "--unset-all", section + ".helper"}, // exit 5 means there was nothing to remove
			{"--global", "--add", section + ".helper", ""},   // an empty helper resets the list inherited from system config
			{"--global", "--add", section + ".helper", "!workctl git-credential"},
			{"--global", section + ".useHttpPath", "true"},
		}
		for _, step := range steps {
			if *dryRun {
				fmt.Printf("git config %s\n", quoteArgs(step))
				continue
			}
			if err := proc.Command(ctx, "git", append([]string{"config"}, step...)...).Run(); err != nil && step[1] != "--unset-all" {
				return fmt.Errorf("git config %s: %w", quoteArgs(step), err)
			}
		}
	}
	if !*dryRun {
		fmt.Printf("git now asks workctl for credentials of: %s\n", strings.Join(names, ", "))
	}
	return nil
}

func quoteArgs(args []string) string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = a
		if a == "" || strings.ContainsAny(a, " !") {
			q[i] = `"` + a + `"`
		}
	}
	return strings.Join(q, " ")
}
