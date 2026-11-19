// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"

	"workctl/internal/api"
	azureclient "workctl/internal/azure/client"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/github"
	"workctl/internal/gitlab"
	"workctl/internal/jira"
	"workctl/internal/modules"
	"workctl/internal/skills"
)

type doctorCheck struct {
	OK     bool   `json:"ok"`
	Detail any    `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
	Code   string `json:"code,omitempty"` // dns, timeout, tls, refused, unauthorized, forbidden or other (see classifyError)
}

// classifyError names the likely cause of a failed profile check, so that a reader need not guess from the message.
func classifyError(err error) string {
	var dnsErr *net.DNSError
	var netErr net.Error
	var certErr *tls.CertificateVerificationError
	switch {
	case err == nil:
		return ""
	case api.IsStatus(err, http.StatusUnauthorized):
		return "unauthorized"
	case api.IsStatus(err, http.StatusForbidden):
		return "forbidden"
	case errors.As(err, &dnsErr):
		return "dns"
	case errors.As(err, &certErr):
		return "tls"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return "timeout"
	}
	return "other"
}

func doctorCommand(cfg *config.Config) error {
	result := map[string]doctorCheck{}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.MaxTimeout())
	defer cancel()
	allOK := true

	for _, name := range slices.Sorted(maps.Keys(cfg.Profiles)) {
		if cfg.Profiles[name].Kind == "atlassian" {
			continue // Atlassian Cloud is verified via the embedded module below
		}
		detail, err := doctorCheckProfile(ctx, cfg, name, cfg.Profiles[name])
		if err != nil {
			result[name] = doctorCheck{Error: err.Error(), Code: classifyError(err)}
			allOK = false
		} else {
			result[name] = doctorCheck{OK: true, Detail: detail}
		}
	}

	// The embedded CLIs and the skills check are informational and do not change the exit code.
	for _, name := range []string{"azure", "atlassian"} {
		out, err := modules.RunProcess(ctx, cfg, name, []string{"config", "test"})
		key := name + " (module)"
		if err != nil {
			result[key] = doctorCheck{Error: err.Error()}
		} else {
			result[key] = doctorCheck{OK: true, Detail: strings.TrimSpace(string(out))}
		}
	}
	result["skills"] = doctorSkills()
	b, _ := json.MarshalIndent(result, "", "  ")
	_ = cliutil.PrintBody(b)
	if !allOK {
		return fmt.Errorf("one or more connectivity checks failed")
	}
	return nil
}

// doctorSkills compares the installed workctl skill with the embedded one. Agents without an installed copy are
// skipped: the user may use the plugin or no skill at all.
func doctorSkills() doctorCheck {
	targets, err := skills.Targets([]string{"all"})
	if err != nil {
		return doctorCheck{Error: err.Error()}
	}
	var current []string
	var outdated []string
	for _, t := range targets {
		entries, err := skills.Status(skills.Embedded(), t)
		if err != nil {
			return doctorCheck{Error: err.Error()}
		}
		for _, e := range entries {
			switch {
			case e.Skill != "workctl" || e.State == skills.Missing:
			case e.State == skills.Differs:
				outdated = append(outdated, t.Name)
			default:
				current = append(current, t.Name)
			}
		}
	}
	if len(outdated) > 0 {
		fixes := make([]string, len(outdated))
		for i, name := range outdated {
			fixes[i] = "workctl skills install --target " + name
		}
		return doctorCheck{Error: "outdated skill copy: " + strings.Join(fixes, "; "), Code: "outdated"}
	}
	return doctorCheck{OK: true, Detail: map[string][]string{"current": current}}
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
		return doctorCheckAPI(ctx, c, profile)
	default:
		return nil, fmt.Errorf("unsupported profile kind %q", profile.Kind)
	}
}

// doctorCheckAPI probes a generic API profile; Google Drive profiles also report the signed-in user.
func doctorCheckAPI(ctx context.Context, c *api.Client, profile config.Profile) (any, error) {
	checkPath := "/"
	var query url.Values
	if profile.HasDrive() {
		checkPath = "/about"
		query = url.Values{"fields": {"user(emailAddress,displayName)"}}
	}
	out, contentType, err := c.Do(ctx, http.MethodGet, checkPath, query, nil, "")
	if err != nil {
		return nil, err
	}
	detail := map[string]string{"kind": "api", "content_type": contentType}
	if checkPath == "/about" {
		var about struct {
			User struct {
				EmailAddress string `json:"emailAddress"`
				DisplayName  string `json:"displayName"`
			} `json:"user"`
		}
		if json.Unmarshal(out, &about) == nil && (about.User.EmailAddress != "" || about.User.DisplayName != "") {
			detail["user"] = cliutil.FirstNonEmpty(about.User.EmailAddress, about.User.DisplayName)
		}
	}
	return detail, nil
}
