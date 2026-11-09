// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"workctl/internal/api"
	azureclient "workctl/internal/azure/client"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/github"
	"workctl/internal/gitlab"
	"workctl/internal/jira"
	"workctl/internal/modules"
)

type doctorCheck struct {
	OK     bool   `json:"ok"`
	Detail any    `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

func doctorCommand(cfg *config.Config) error {
	result := map[string]doctorCheck{}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.MaxTimeout())
	defer cancel()
	allOK := true

	names := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if cfg.Profiles[name].Kind == "atlassian" {
			continue // Atlassian Cloud is verified via the embedded module below
		}
		detail, err := doctorCheckProfile(ctx, cfg, name, cfg.Profiles[name])
		if err != nil {
			result[name] = doctorCheck{Error: err.Error()}
			allOK = false
		} else {
			result[name] = doctorCheck{OK: true, Detail: detail}
		}
	}

	// The embedded CLIs report their own connectivity; informational, does not change the exit code.
	for _, name := range []string{"azure", "atlassian"} {
		out, err := modules.RunProcess(ctx, cfg, name, []string{"config", "test"})
		key := name + " (module)"
		if err != nil {
			result[key] = doctorCheck{Error: err.Error()}
		} else {
			result[key] = doctorCheck{OK: true, Detail: strings.TrimSpace(string(out))}
		}
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	_ = cliutil.PrintBody(b)
	if !allOK {
		return fmt.Errorf("one or more connectivity checks failed")
	}
	return nil
}

// doctorCheckProfile makes one lightweight authenticated call and returns a compact identity detail.
func doctorCheckProfile(ctx context.Context, cfg *config.Config, name string, profile config.Profile) (any, error) {
	if profile.Kind == "azure" {
		cred, err := config.ResolveProfileSecret(name, profile)
		if err != nil {
			return nil, err
		}
		c := azureclient.New(azureclient.Options{OrgURL: profile.URL, Token: cred, Timeout: cfg.ProfileTimeout(profile), VerifyTLS: profile.VerifyTLS})
		cd, err := c.ConnectionData(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "azure", "user": cd.AuthenticatedUser.ProviderDisplayName}, nil
	}
	if profile.UsesSSHExec() {
		return map[string]string{"kind": "api", "backend": "ssh-exec", "check": "skipped: no command is known to be safe to run"}, nil
	}
	c, err := api.NewProfileClient(cfg, name, profile)
	if err != nil {
		return nil, err
	}
	switch profile.Kind {
	case "github":
		u, err := github.User(ctx, c)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "github", "login": cliutil.JSONField(u, "login")}, nil
	case "gitlab":
		ver, err := gitlab.Version(ctx, c)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "gitlab", "version": cliutil.JSONField(ver, "version")}, nil
	case "jira":
		me, err := jira.Myself(ctx, c)
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "jira", "user": cliutil.FirstNonEmpty(cliutil.JSONField(me, "name"), cliutil.JSONField(me, "displayName"))}, nil
	case "api":
		_, contentType, err := c.Do(ctx, http.MethodGet, "/", nil, nil, "")
		if err != nil {
			return nil, err
		}
		return map[string]string{"kind": "api", "content_type": contentType}, nil
	default:
		return nil, fmt.Errorf("unsupported profile kind %q", profile.Kind)
	}
}
