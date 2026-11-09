// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"workctl/internal/cliutil"
	"workctl/internal/config"
)

// ErrOtherAccount is returned by BindToken when the token belongs to another account than the profile is bound to.
var ErrOtherAccount = errors.New("the token belongs to another account")

// Account asks GitHub or GitLab which account the profile's token belongs to.
func (c *Client) Account(ctx context.Context) (string, error) {
	path, field := "/user", "login"
	switch c.kind {
	case "github":
	case "gitlab":
		path, field = "user", "username"
	default:
		return "", fmt.Errorf("no account check for %s profiles", c.kind)
	}
	// The account lives on the profile's API host, also for a copy pointed at another host (GitHub uploads).
	home := *c
	home.baseURL = cliutil.FirstNonEmpty(c.home, c.baseURL)
	out, err := BodyOnly(home.Do(ctx, http.MethodGet, path, nil, nil, ""))
	if err != nil {
		return "", err
	}
	if who := cliutil.JSONField(out, field); who != "" {
		return who, nil
	}
	return "", fmt.Errorf("%s returned no %s", c.kind, field)
}

// BindToken binds a native github or gitlab profile to the account of its token (Profile.Username), as BindCLI does
// for cli profiles; writes are then refused when the token belongs to someone else. It fails when the profile is
// already bound to another account; to move a profile to another account, clear its "username" first.
func BindToken(cfg *config.Config, name string, profile config.Profile) (_ config.Profile, resultErr error) {
	return bindToken(cfg, name, profile, nil)
}

// BindTokenValue checks the supplied token rather than the current environment or keyring credential.
func BindTokenValue(cfg *config.Config, name string, profile config.Profile, token string) (config.Profile, error) {
	return bindToken(cfg, name, profile, &token)
}

func bindToken(cfg *config.Config, name string, profile config.Profile, token *string) (_ config.Profile, resultErr error) {
	if (profile.Kind != "github" && profile.Kind != "gitlab") || profile.UsesCLI() {
		return profile, nil
	}
	c, err := newProfileClient(cfg, name, profile, token)
	if err != nil {
		return profile, err
	}
	defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
	ctx, cancel := context.WithTimeout(context.Background(), cfg.ProfileTimeout(profile))
	defer cancel()
	who, err := c.Account(ctx)
	if err != nil {
		return profile, fmt.Errorf("profile %q: %w", name, err)
	}
	if profile.Username != "" && !strings.EqualFold(who, profile.Username) {
		return profile, fmt.Errorf("%w: %q, but profile %q is bound to %q; clear its \"username\" to rebind it", ErrOtherAccount, who, name, profile.Username)
	}
	profile.Username = who
	return profile, nil
}
