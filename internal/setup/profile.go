// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package setup

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"strings"

	"workctl/internal/cliutil"
	"workctl/internal/config"
)

const profileUsage = "usage: workctl profile add NAME --kind KIND [flags] | workctl profile edit NAME [flags]"

// profileOptions are the flags that are not profile fields.
type profileOptions struct {
	makeDefault bool
	asJSON      bool
}

// ProfileCommand is `workctl profile add|edit NAME [flags]`: the non-interactive way to write any kind of profile
// (api, google, azure, atlassian, ... as well as github, gitlab and jira) without editing config.json by hand.
// It never stores a credential: see `workctl auth set-token|login`.
func ProfileCommand(path string, args []string) error {
	if len(args) < 2 || (args[0] != "add" && args[0] != "edit") {
		return errors.New(profileUsage)
	}
	action, name, flags := args[0], args[1], args[2:]
	cfg, err := config.LoadOrDefault(path)
	if err != nil {
		return err
	}
	p, exists := cfg.Profiles[name]
	switch {
	case action == "add" && exists:
		return fmt.Errorf("profile %q already exists (change it with `workctl profile edit %s`)", name, name)
	case action == "edit" && !exists:
		return fmt.Errorf("profile %q is not configured (create it with `workctl profile add %s --kind KIND`)", name, name)
	case action == "add":
		p = config.Profile{VerifyTLS: true}
	}

	fs := flag.NewFlagSet("profile "+action, flag.ContinueOnError)
	var opts profileOptions
	profileFlags(fs, &p, &opts, action == "add")
	if err := fs.Parse(flags); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	if action == "add" {
		if p, err = newProfile(cfg, name, p); err != nil {
			return err
		}
	}
	return saveProfile(cfg, name, p, opts)
}

// profileFlags registers one flag per profile field; parsing writes straight into p, so a flag that is not given
// leaves the field as it was. --kind exists only when a profile is added.
func profileFlags(fs *flag.FlagSet, p *config.Profile, opts *profileOptions, adding bool) {
	list := func(name, usage string, dst func() *[]string) {
		fs.Func(name, usage+" (comma separated; an empty value clears the list)", func(v string) error {
			*dst() = cliutil.SplitCommaList(v)
			return nil
		})
	}
	host := func(v string) error {
		p.URL = hostURL(v)
		return nil
	}
	oauth := func() *config.OAuth2 {
		if p.OAuth2 == nil {
			p.OAuth2 = &config.OAuth2{}
		}
		return p.OAuth2
	}

	if adding {
		fs.StringVar(&p.Kind, "kind", "", "github, gitlab, jira, azure, atlassian, api or google (required)")
	}
	fs.Func("host", "server host or base URL (https:// is added to a bare host)", host)
	fs.Func("url", "same as --host", host)
	fs.StringVar(&p.Auth, "auth", p.Auth, "authentication scheme (bearer, basic, private-token, header, form, oauth2, none)")
	fs.StringVar(&p.AuthHeader, "auth-header", p.AuthHeader, "header that carries the secret with --auth header")
	fs.StringVar(&p.Backend, "backend", p.Backend, "native, cli (gh or glab), ssh-tunnel")
	fs.StringVar(&p.SSHDestination, "ssh-destination", p.SSHDestination, "ssh host or ~/.ssh/config alias for backend ssh-tunnel")
	fs.StringVar(&p.Username, "username", p.Username, "account name (basic auth; the bound account of a gh/glab profile)")
	fs.StringVar(&p.TokenEnv, "token-env", p.TokenEnv, "environment variable that holds the token")
	fs.StringVar(&p.CAFile, "ca", p.CAFile, "custom PEM CA bundle (absolute path)")
	fs.BoolVar(&p.VerifyTLS, "verify-tls", p.VerifyTLS, "verify TLS certificates")
	fs.BoolVar(&p.ReadOnly, "read-only", p.ReadOnly, "refuse writes through this profile")
	fs.IntVar(&p.TimeoutSeconds, "timeout", p.TimeoutSeconds, "request timeout in seconds (0: the global one)")
	fs.StringVar(&p.Service, "service", p.Service, "kind google: "+strings.Join(config.GoogleServiceNames(), ", "))
	fs.Func("client-id", "OAuth 2.0 client id (kind google, or api with --auth oauth2)", func(v string) error { oauth().ClientID = v; return nil })
	fs.Func("auth-url", "OAuth 2.0 authorization endpoint", func(v string) error { oauth().AuthURL = v; return nil })
	fs.Func("token-url", "OAuth 2.0 token endpoint", func(v string) error { oauth().TokenURL = v; return nil })
	list("scopes", "OAuth 2.0 scopes", func() *[]string { return &oauth().Scopes })
	list("repos", "repositories (owner/name or owner/*) this profile serves", func() *[]string { return &p.Repos })
	list("projects", "projects (group/project, PROJ) this profile serves", func() *[]string { return &p.Projects })
	fs.StringVar(&p.DefaultRepo, "default-repo", p.DefaultRepo, "default repository")
	fs.StringVar(&p.DefaultProject, "default-project", p.DefaultProject, "default project (Drive: the default folder id)")
	fs.BoolVar(&opts.makeDefault, "default", false, "make this the default profile of its kind")
	fs.BoolVar(&opts.asJSON, "json", false, "print the saved profile as JSON (identifiers redacted)")
}

// hostURL turns a bare host into an https URL and leaves a full URL alone.
func hostURL(host string) string {
	if host = strings.TrimSpace(host); host != "" && !strings.Contains(host, "://") {
		return "https://" + host
	}
	return host
}

// newProfile completes a profile read from the flags of `profile add`: the kind decides the defaults.
func newProfile(cfg *config.Config, name string, p config.Profile) (config.Profile, error) {
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	if p.Kind == "" {
		return p, fmt.Errorf("--kind is required (github, gitlab, jira, azure, atlassian, api or google)")
	}
	base := blankProfile(cfg, p.Kind)
	p.URL = cliutil.FirstNonEmpty(p.URL, base.URL)
	if !p.UsesCLI() { // gh and glab keep their own credentials
		p.Auth = cliutil.FirstNonEmpty(p.Auth, base.Auth)
		p.TokenEnv = cliutil.FirstNonEmpty(p.TokenEnv, base.TokenEnv)
	}
	if p.Kind == config.GoogleKind {
		return config.ExpandGoogle(name, p)
	}
	return p, nil
}

// saveProfile stores the profile, validates the whole config, saves it and reports the result.
func saveProfile(cfg *config.Config, name string, p config.Profile, opts profileOptions) error {
	cfg.Profiles[name] = p
	if opts.makeDefault || cfg.Defaults[p.Kind] == "" {
		cfg.Defaults[p.Kind] = name
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := cfg.Save(); err != nil {
		return err
	}
	if opts.asJSON {
		b, err := json.Marshal(map[string]any{"name": name, "path": cfg.Path(), "profile": cfg.Redacted().Profiles[name]})
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	}
	fmt.Printf("saved: %s\nprofile %s (%s) is configured\n", cfg.Path(), name, p.Kind)
	return nil
}
