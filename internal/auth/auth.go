// Copyright (c) 2026 PrzemysĹ‚aw Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package auth manages profile credentials: `workctl auth login|set-token|status|logout`
// and the git-credential helper that hands a profile token to git.
package auth

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"golang.org/x/term"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/prompt"
	"workctl/internal/secret"
)

// Command is `workctl [--profile NAME] auth login|set-token|status|logout`; the global profile is the default of each.
func Command(configPath, profile string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: workctl auth login|set-token|status|logout|setup-git [options]")
	}
	cfg, err := config.LoadOrDefault(configPath)
	if err != nil {
		return err
	}
	cfg.SetProfileName(profile)
	switch args[0] {
	case "login":
		return loginCommand(cfg, args[1:])
	case "set-token":
		return setTokenCommand(cfg, args[1:])
	case "status":
		return statusCommand(cfg, args[1:])
	case "logout":
		return logoutCommand(cfg, args[1:])
	case "setup-git":
		return setupGitCommand(cfg, args[1:])
	default:
		return fmt.Errorf("unknown auth command %q (use login, set-token, status, logout, or setup-git)", args[0])
	}
}

// loginCommand logs a cli profile in through gh or glab (see api.BindCLI), or runs an OAuth 2.0 loopback login.
func loginCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	noBrowser := fs.Bool("no-browser", false, "print the OAuth2 authorization URL without opening a browser")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := resolveProfileName(cfg, *profileName)
	if name == "" {
		return fmt.Errorf("specify profile with --profile NAME")
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found in config", name)
	}
	if strings.EqualFold(profile.Auth, "oauth2") {
		return loginOAuth2Profile(cfg, name, profile, !*noBrowser)
	}
	if !profile.UsesCLI() {
		return fmt.Errorf("profile %q keeps a token in the keyring: use `workctl auth set-token --profile %s`", name, name)
	}
	profile, err := api.BindCLI(cfg, name, profile, true)
	if err != nil {
		return err
	}
	cfg.Profiles[name] = profile
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Printf("profile %s is bound to %s\n", name, profile.Username)
	return nil
}

func loginOAuth2Profile(cfg *config.Config, name string, profile config.Profile, openBrowser bool) error {
	if err := config.ValidateProfile(name, profile); err != nil {
		return err
	}
	account := secret.Account(profile.Kind, name)
	var existing api.OAuth2Credential
	if raw, err := config.TokenStore.Get(account); err == nil && raw != "" {
		existing = api.ParseOAuth2Credential(raw)
	} else if profile.TokenEnv != "" && os.Getenv(profile.TokenEnv) != "" {
		existing = api.ParseOAuth2Credential(os.Getenv(profile.TokenEnv))
	}
	if existing.ClientSecret == "" && term.IsTerminal(int(os.Stdin.Fd())) {
		r := bufio.NewReader(cliutil.Stdin)
		sec, err := prompt.AskSecret(r, "OAuth2 client_secret for "+name+" (leave empty for public PKCE clients)")
		if err != nil {
			return fmt.Errorf("read client_secret: %w", err)
		}
		existing.ClientSecret = strings.TrimSpace(sec)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cred, err := api.LoginOAuth2(ctx, cfg, name, profile, existing, func(authURL string) {
		fmt.Printf("Open this URL to authorize profile %s:\n%s\n", name, authURL)
		if openBrowser {
			if err := cliutil.OpenURL(authURL); err != nil {
				fmt.Fprintf(os.Stderr, "could not open a browser (%v); open the URL above by hand\n", err)
			}
		}
	})
	if err != nil {
		return err
	}
	encoded, err := cred.Encode()
	if err != nil {
		return err
	}
	if err := config.TokenStore.Set(account, encoded); err != nil {
		return err
	}
	fmt.Printf("OAuth2 refresh token stored in keyring for profile %s (%s)\n", name, account)
	return nil
}

func setTokenCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth set-token", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	fromStdin := fs.Bool("stdin", false, "read token from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := resolveProfileName(cfg, *profileName)
	if name == "" {
		return fmt.Errorf("specify profile with --profile NAME")
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found in config", name)
	}
	if err := config.NoTokenError(name, profile); err != nil {
		return err
	}

	var tok string
	r := bufio.NewReader(cliutil.Stdin)
	if *fromStdin || !term.IsTerminal(int(os.Stdin.Fd())) {
		line, err := r.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("read token: %w", err)
		}
		tok = strings.TrimSpace(line)
	} else {
		var err error
		tok, err = prompt.AskSecret(r, "Token for "+name)
		if err != nil {
			return fmt.Errorf("read token: %w", err)
		}
	}
	if tok == "" {
		return fmt.Errorf("token cannot be empty")
	}

	account := secret.Account(profile.Kind, name)
	bound, err := api.BindToken(cfg, name, profile, tok)
	if errors.Is(err, api.ErrOtherAccount) {
		return fmt.Errorf("token not stored: %w", err)
	}
	if storeErr := config.TokenStore.Set(account, tok); storeErr != nil {
		return storeErr
	}
	if _, forgetErr := api.ForgetOAuth2Token(name, profile); forgetErr != nil {
		return forgetErr
	}
	fmt.Printf("token stored in keyring for profile %s (%s)\n", name, account)
	if err != nil {
		fmt.Printf("account not checked: %v\n", err)
		return nil
	}
	if bound.Username != profile.Username {
		cfg.Profiles[name] = bound
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("profile %s is bound to %s\n", name, bound.Username)
	}
	return nil
}

func statusCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(cfg.Profiles) == 0 {
		fmt.Println("no profiles configured")
		return nil
	}

	var names []string
	if only := cliutil.FirstNonEmpty(*profileName, cfg.ProfileName()); only != "" {
		if _, ok := cfg.Profiles[only]; !ok {
			return fmt.Errorf("profile %q not found in config", only)
		}
		names = append(names, only)
	} else {
		names = slices.Sorted(maps.Keys(cfg.Profiles))
	}

	for _, n := range names {
		prof := cfg.Profiles[n]
		fmt.Printf("%s (%s): %s\n", n, prof.Kind, CredentialStatus(cfg, n, prof))
	}
	return nil
}

// cliStatus says which account the CLI of a cli profile is logged in as.
func cliStatus(cfg *config.Config, prof config.Profile) string {
	tool, who, err := api.CLIWhoami(cfg, prof)
	if err != nil {
		if tool == "" {
			return err.Error()
		}
		return "not logged in: " + err.Error()
	}
	return fmt.Sprintf("logged in as %s via %s", who, tool)
}

func logoutCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := resolveProfileName(cfg, *profileName)
	if name == "" {
		return fmt.Errorf("specify profile with --profile NAME")
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found in config", name)
	}
	if err := config.NoTokenError(name, profile); err != nil {
		return err
	}

	if removed, err := api.ForgetSession(name, profile); err != nil {
		return err
	} else if removed {
		fmt.Printf("saved form-login session removed for profile %s\n", name)
	}
	if removed, err := api.ForgetOAuth2Token(name, profile); err != nil {
		return err
	} else if removed {
		fmt.Printf("cached OAuth2 access token removed for profile %s\n", name)
	}
	account := secret.Account(profile.Kind, name)
	if err := config.TokenStore.Delete(account); err != nil {
		return err
	}
	fmt.Printf("token removed from keyring for profile %s\n", name)
	return nil
}

func resolveProfileName(cfg *config.Config, requested string) string {
	if requested != "" {
		return requested
	}
	if cfg.ProfileName() != "" {
		return cfg.ProfileName()
	}
	if env := strings.TrimSpace(os.Getenv("WORKCTL_PROFILE")); env != "" {
		return env
	}
	if len(cfg.Profiles) == 1 {
		for n := range cfg.Profiles {
			return n
		}
	}
	return ""
}
