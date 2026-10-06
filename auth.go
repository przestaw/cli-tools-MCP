package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"

	"golang.org/x/term"
	"workctl/internal/prompt"
	"workctl/internal/secret"
)

func cmdAuth(configPath string, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: workctl auth set-token|status|logout [options]")
	}
	cfg, err := loadConfigOrDefault(configPath)
	if err != nil {
		return err
	}
	switch args[0] {
	case "set-token":
		return cmdAuthSetToken(cfg, args[1:])
	case "status":
		return cmdAuthStatus(cfg, args[1:])
	case "logout":
		return cmdAuthLogout(cfg, args[1:])
	default:
		return fmt.Errorf("unknown auth command %q (use set-token, status, or logout)", args[0])
	}
}

func cmdAuthSetToken(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("auth set-token", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	fromStdin := fs.Bool("stdin", false, "read token from stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := resolveAuthProfileName(cfg, *profileName)
	if name == "" {
		return fmt.Errorf("specify profile with --profile NAME")
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found in config", name)
	}

	var tok string
	r := bufio.NewReader(stdin)
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
	if err := tokenStore.Set(account, tok); err != nil {
		return err
	}
	fmt.Printf("token stored in keyring for profile %s (%s)\n", name, account)
	return nil
}

func cmdAuthStatus(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("auth status", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(cfg.Profiles) == 0 {
		fmt.Println("no profiles configured")
		return nil
	}

	names := make([]string, 0, len(cfg.Profiles))
	if *profileName != "" {
		if _, ok := cfg.Profiles[*profileName]; !ok {
			return fmt.Errorf("profile %q not found in config", *profileName)
		}
		names = append(names, *profileName)
	} else {
		for n := range cfg.Profiles {
			names = append(names, n)
		}
		sort.Strings(names)
	}

	for _, n := range names {
		prof := cfg.Profiles[n]
		status := "missing"
		if prof.TokenEnv != "" && os.Getenv(prof.TokenEnv) != "" {
			status = fmt.Sprintf("set via environment variable %s", prof.TokenEnv)
		} else {
			account := secret.Account(prof.Kind, n)
			val, err := tokenStore.Get(account)
			if err == nil && val != "" {
				status = "stored in keyring"
			} else if err != nil && !errors.Is(err, secret.ErrNotFound) {
				status = fmt.Sprintf("keyring error: %v", err)
			}
		}
		fmt.Printf("%s (%s): %s\n", n, prof.Kind, status)
	}
	return nil
}

func cmdAuthLogout(cfg *Config, args []string) error {
	fs := flag.NewFlagSet("auth logout", flag.ContinueOnError)
	profileName := fs.String("profile", "", "profile name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	name := resolveAuthProfileName(cfg, *profileName)
	if name == "" {
		return fmt.Errorf("specify profile with --profile NAME")
	}
	profile, ok := cfg.Profiles[name]
	if !ok {
		return fmt.Errorf("profile %q not found in config", name)
	}

	account := secret.Account(profile.Kind, name)
	if err := tokenStore.Delete(account); err != nil {
		return err
	}
	fmt.Printf("token removed from keyring for profile %s\n", name)
	return nil
}

func resolveAuthProfileName(cfg *Config, requested string) string {
	if requested != "" {
		return requested
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

// cmdGitCredential is a git credential helper: git gets the profile token straight from the keyring.
func cmdGitCredential(cfg *Config, args []string, in io.Reader, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: workctl --profile NAME git-credential get")
	}
	if args[0] != "get" {
		return nil // store/erase: the token is managed with `workctl auth`
	}
	profile, ok := cfg.Profiles[cfg.profile]
	if !ok || !prKinds[profile.Kind] {
		return fmt.Errorf("git-credential needs --profile with a github, gitlab or azure profile")
	}
	req := map[string]string{}
	sc := bufio.NewScanner(in)
	for sc.Scan() && sc.Text() != "" {
		k, v, _ := strings.Cut(sc.Text(), "=")
		req[k] = v
	}
	u, err := url.Parse(normalizeBaseURL(profile.URL))
	if err != nil {
		return err
	}
	// api.github.com serves the API; git talks to github.com.
	if req["protocol"] != "https" || !strings.EqualFold(req["host"], strings.TrimPrefix(u.Host, "api.")) {
		return nil
	}
	token, err := resolveProfileSecret(cfg.profile, profile)
	if err != nil {
		return err
	}
	user := map[string]string{"github": "x-access-token", "gitlab": "oauth2"}[profile.Kind]
	_, err = fmt.Fprintf(out, "username=%s\npassword=%s\n", firstNonEmpty(user, "workctl"), token)
	return err
}
