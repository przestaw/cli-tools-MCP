// Copyright (c) 2026 PrzemysĹ‚aw Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package auth manages profile credentials: `workctl auth login|set-token|status|logout`
// and the git-credential helper that hands a profile token to git.
package auth

import (
	"bufio"
	"context"
	"encoding/json"
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

// result is what `auth login` and `auth set-token` report. With --json it is the only output; it holds no secret.
type result struct {
	Profile  string `json:"profile"`
	Action   string `json:"action"` // token_stored, oauth_stored or bound
	Username string `json:"username,omitempty"`
	Warning  string `json:"warning,omitempty"`
	lines    []string
}

func (r *result) say(format string, a ...any) { r.lines = append(r.lines, fmt.Sprintf(format, a...)) }

func (r *result) print(asJSON bool) error {
	if asJSON {
		b, err := json.Marshal(r)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	}
	for _, line := range r.lines {
		fmt.Println(line)
	}
	return nil
}

// loginCommand logs a cli profile in through gh or glab (see api.BindCLI), or runs an OAuth 2.0 loopback login.
// --existing binds a cli profile to the account gh or glab is already logged in as, without starting a login.
func loginCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth login", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	noBrowser := fs.Bool("no-browser", false, "print the OAuth2 authorization URL without opening a browser")
	existing := fs.Bool("existing", false, "gh/glab profile: bind to the current gh/glab login, never start a login")
	asJSON := fs.Bool("json", false, "print the result as one JSON document")
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
		return loginOAuth2Profile(cfg, name, profile, !*noBrowser, *asJSON)
	}
	if !profile.UsesCLI() {
		return fmt.Errorf("profile %q keeps a token in the keyring: use `workctl auth set-token --profile %s`", name, name)
	}
	profile, err := api.BindCLI(cfg, name, profile, !*existing)
	if err != nil {
		return err
	}
	cfg.Profiles[name] = profile
	if err := cfg.Save(); err != nil {
		return err
	}
	res := &result{Profile: name, Action: "bound", Username: profile.Username}
	res.say("profile %s is bound to %s", name, profile.Username)
	return res.print(*asJSON)
}

func loginOAuth2Profile(cfg *config.Config, name string, profile config.Profile, openBrowser, asJSON bool) error {
	if err := config.ValidateProfile(name, profile); err != nil {
		return err
	}
	// With --json stdout carries only the result, so the URL goes to stderr.
	urlOut := io.Writer(os.Stdout)
	if asJSON {
		urlOut = os.Stderr
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
		fmt.Fprintf(urlOut, "Open this URL to authorize profile %s:\n%s\n", name, authURL)
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
	res := &result{Profile: name, Action: "oauth_stored"}
	res.say("OAuth2 refresh token stored in keyring for profile %s (%s)", name, account)
	return res.print(asJSON)
}

func setTokenCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth set-token", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	fromStdin := fs.Bool("stdin", false, "read token from stdin")
	asJSON := fs.Bool("json", false, "print the result as one JSON document")
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
	res := &result{Profile: name, Action: "token_stored", Username: profile.Username}
	res.say("token stored in keyring for profile %s (%s)", name, account)
	if err != nil {
		res.Warning = fmt.Sprintf("account not checked: %v", err)
		res.say("%s", res.Warning)
		return res.print(*asJSON)
	}
	res.Username = bound.Username
	if bound.Username != profile.Username {
		cfg.Profiles[name] = bound
		if err := cfg.Save(); err != nil {
			return err
		}
		res.say("profile %s is bound to %s", name, bound.Username)
	}
	return res.print(*asJSON)
}

// statusEntry is one profile of `auth status --json`.
type statusEntry struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Username string `json:"username,omitempty"`
	Credential
}

func statusCommand(cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	asJSON := fs.Bool("json", false, "print one JSON array with the state of each profile")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(cfg.Profiles) == 0 && !*asJSON {
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

	entries := []statusEntry{}
	for _, n := range names {
		prof := cfg.Profiles[n]
		entries = append(entries, statusEntry{Name: n, Kind: prof.Kind, Username: prof.Username, Credential: ProfileCredential(cfg, n, prof)})
	}
	if *asJSON {
		b, err := json.Marshal(entries)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	}
	for _, e := range entries {
		fmt.Printf("%s (%s): %s\n", e.Name, e.Kind, e.Detail)
	}
	return nil
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
