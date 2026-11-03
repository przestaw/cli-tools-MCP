// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"slices"
	"sort"
	"strings"

	"workctl/internal/config"
	"workctl/internal/mcp"
	"workctl/internal/secret"
)

// AccountEntry is one profile's non-secret summary returned by the accounts MCP tool.
type AccountEntry struct {
	Name           string   `json:"name"`
	Kind           string   `json:"kind"`
	Host           string   `json:"host,omitempty"`
	URL            string   `json:"url,omitempty"`
	Backend        string   `json:"backend"`
	Auth           string   `json:"auth"`
	Username       string   `json:"username,omitempty"`
	ReadOnly       bool     `json:"read_only,omitempty"`
	Default        bool     `json:"default,omitempty"`
	DefaultRepo    string   `json:"default_repo,omitempty"`
	Repos          []string `json:"repos,omitempty"`
	DefaultProject string   `json:"default_project,omitempty"`
	Projects       []string `json:"projects,omitempty"`
	Credential     string   `json:"credential"`
}

// AccountsReport is the JSON payload returned by the accounts MCP tool.
type AccountsReport struct {
	Profiles        []AccountEntry    `json:"profiles"`
	Defaults        map[string]string `json:"defaults,omitempty"`
	AllowWrite      bool              `json:"allow_write"`
	ResolvedProfile string            `json:"resolved_profile,omitempty"`
	ResolutionError string            `json:"resolution_error,omitempty"`
}

// CredentialStatus reports whether a profile's credential is present without revealing any secret.
func CredentialStatus(cfg *config.Config, name string, prof config.Profile) string {
	if prof.UsesCLI() {
		return cliStatus(cfg, prof)
	}
	if err := config.NoTokenError(name, prof); err != nil {
		return err.Error()
	}
	if prof.TokenEnv != "" && os.Getenv(prof.TokenEnv) != "" {
		return fmt.Sprintf("set via environment variable %s", prof.TokenEnv)
	}
	account := secret.Account(prof.Kind, name)
	val, err := config.TokenStore.Get(account)
	if err == nil && val != "" {
		return "stored in keyring"
	}
	if err != nil && !errors.Is(err, secret.ErrNotFound) {
		return fmt.Sprintf("keyring error: %v", err)
	}
	return "missing"
}

// Tools returns the MCP tools of the auth package.
func Tools() []mcp.Tool {
	return []mcp.Tool{{
		Name:        "accounts",
		Description: "List configured workctl profiles with their kind, host, username, optional repos/projects patterns, credential status (never the secret) and write policy, or check which profile resolves for a repository/project/issue key. Optionally bind a repo or project pattern (e.g. 'owner/*' or 'PROJ') to an existing profile.",
		InputSchema: mcp.ObjSchema(map[string]any{
			"kind":         mcp.StrProp("Optional provider kind filter or resolution hint: github, gitlab, jira, azure, atlassian, api"),
			"resource":     mcp.StrProp("Optional repository (owner/name), project (group/name) or Jira key (KEY-123) to check which profile resolves for it"),
			"bind_profile": mcp.StrProp("Optional existing profile name to add bind_repo or bind_project to"),
			"bind_repo":    mcp.StrProp("Optional repository or prefix pattern (e.g. 'owner/repo' or 'owner/*') to add to bind_profile.repos"),
			"bind_project": mcp.StrProp("Optional project or prefix pattern (e.g. 'group/*' or 'PROJ') to add to bind_profile.projects"),
		}),
	}}
}

// CallTool runs the accounts MCP tool.
func CallTool(_ context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	if name != "accounts" {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	if err := bindResource(cfg, mcp.ArgString(a, "bind_profile"), mcp.ArgString(a, "bind_repo"), mcp.ArgString(a, "bind_project")); err != nil {
		return nil, err
	}
	kind := strings.ToLower(strings.TrimSpace(mcp.ArgString(a, "kind")))
	resource := strings.TrimSpace(mcp.ArgString(a, "resource"))

	names := make([]string, 0, len(cfg.Profiles))
	for n, p := range cfg.Profiles {
		if kind != "" && resource == "" && p.Kind != kind {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	entries := make([]AccountEntry, 0, len(names))
	for _, n := range names {
		p := cfg.Profiles[n]
		backend := p.Backend
		if backend == "" {
			backend = "native"
		}
		authScheme := strings.ToLower(p.Auth)
		switch {
		case p.UsesCLI():
			authScheme = "cli"
		case authScheme == "":
			if p.Kind == "gitlab" {
				authScheme = "private-token"
			} else {
				authScheme = "bearer"
			}
		}
		entries = append(entries, AccountEntry{
			Name:           n,
			Kind:           p.Kind,
			Host:           profileHost(p),
			URL:            config.NormalizeBaseURL(p.URL),
			Backend:        backend,
			Auth:           authScheme,
			Username:       p.Username,
			ReadOnly:       p.ReadOnly,
			Default:        cfg.Defaults[p.Kind] == n,
			DefaultRepo:    p.DefaultRepo,
			Repos:          p.Repos,
			DefaultProject: p.DefaultProject,
			Projects:       p.Projects,
			Credential:     CredentialStatus(cfg, n, p),
		})
	}

	report := AccountsReport{
		Profiles:   entries,
		Defaults:   cfg.Defaults,
		AllowWrite: cfg.Policy.AllowWrite,
	}
	if resource != "" || kind != "" {
		resolved, _, err := cfg.ResolveFor(kind, resource)
		if err != nil {
			report.ResolutionError = err.Error()
		} else {
			report.ResolvedProfile = resolved
		}
	}
	return json.MarshalIndent(report, "", "  ")
}

func profileHost(p config.Profile) string {
	u, err := url.Parse(config.NormalizeBaseURL(p.URL))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(u.Host, "api."))
}

func validatePattern(pat string, requireSlash bool) error {
	if pat == "" || strings.ContainsFunc(pat, func(r rune) bool { return r <= ' ' || r == 0x7f }) {
		return fmt.Errorf("invalid resource pattern %q", pat)
	}
	for _, seg := range strings.Split(pat, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid resource pattern %q", pat)
		}
	}
	if requireSlash && !strings.Contains(pat, "/") {
		return fmt.Errorf("invalid repository pattern %q: expected owner/repo or owner/*", pat)
	}
	return nil
}

func containsFold(list []string, target string) bool {
	return slices.ContainsFunc(list, func(item string) bool {
		return strings.EqualFold(strings.Trim(strings.TrimSpace(item), "/"), target)
	})
}

func bindResource(cfg *config.Config, profileName, repoPattern, projectPattern string) error {
	profileName = strings.TrimSpace(profileName)
	repoPattern = strings.Trim(strings.TrimSpace(repoPattern), "/")
	projectPattern = strings.Trim(strings.TrimSpace(projectPattern), "/")
	if repoPattern == "" && projectPattern == "" {
		if profileName != "" {
			return fmt.Errorf("bind_repo or bind_project is required when bind_profile is set")
		}
		return nil
	}
	if profileName == "" {
		return fmt.Errorf("bind_profile is required when bind_repo or bind_project is set")
	}
	prof, ok := cfg.Profiles[profileName]
	if !ok {
		return fmt.Errorf("profile %q is not configured", profileName)
	}
	host := profileHost(prof)

	if repoPattern != "" {
		if !config.PRKinds[prof.Kind] {
			return fmt.Errorf("profile %q (%s) does not use repos", profileName, prof.Kind)
		}
		if err := validatePattern(repoPattern, true); err != nil {
			return err
		}
		for otherName, other := range cfg.Profiles {
			if otherName == profileName || other.Kind != prof.Kind || profileHost(other) != host {
				continue
			}
			if strings.EqualFold(other.DefaultRepo, repoPattern) || containsFold(other.Repos, repoPattern) {
				return fmt.Errorf("repository pattern %q is already bound to profile %q", repoPattern, otherName)
			}
		}
		if !strings.EqualFold(prof.DefaultRepo, repoPattern) && !containsFold(prof.Repos, repoPattern) {
			prof.Repos = append(prof.Repos, repoPattern)
		}
	}

	if projectPattern != "" {
		switch prof.Kind {
		case "gitlab", "azure", "jira", "atlassian":
		default:
			return fmt.Errorf("profile %q (%s) does not use projects", profileName, prof.Kind)
		}
		if err := validatePattern(projectPattern, false); err != nil {
			return err
		}
		for otherName, other := range cfg.Profiles {
			if otherName == profileName || other.Kind != prof.Kind || profileHost(other) != host {
				continue
			}
			if strings.EqualFold(other.DefaultProject, projectPattern) || containsFold(other.Projects, projectPattern) {
				return fmt.Errorf("project pattern %q is already bound to profile %q", projectPattern, otherName)
			}
		}
		if !strings.EqualFold(prof.DefaultProject, projectPattern) && !containsFold(prof.Projects, projectPattern) {
			prof.Projects = append(prof.Projects, projectPattern)
		}
	}

	cfg.Profiles[profileName] = prof
	if cfg.Path() != "" {
		return cfg.Save()
	}
	return nil
}
