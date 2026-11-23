// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Where an OAuth client_id comes from.
const (
	ClientIDOwn     = "own"      // oauth2.client_id of the profile
	ClientIDBuiltIn = "built-in" // the default of the provider host in builtinClientIDs
)

// builtinClientIDs maps a provider host (github.com, gitlab.com) to the client_id of the workctl OAuth app registered
// there. A client_id is public, not a secret. It is empty until the apps are registered.
var builtinClientIDs = map[string]string{}

// SetBuiltinClientIDs replaces the built-in table and returns a function that restores it; for tests.
func SetBuiltinClientIDs(m map[string]string) (restore func()) {
	old := builtinClientIDs
	builtinClientIDs = m
	return func() { builtinClientIDs = old }
}

// OAuthHost is the provider host of a profile's OAuth login: the host of oauth2.auth_url, else of the profile URL.
// api.github.com is github.com.
func OAuthHost(p Profile) string {
	raw := p.URL
	if p.OAuth2 != nil && strings.TrimSpace(p.OAuth2.AuthURL) != "" {
		raw = p.OAuth2.AuthURL
	}
	u, err := url.Parse(NormalizeBaseURL(raw))
	if err != nil {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "api.github.com" {
		return "github.com"
	}
	return host
}

// BuiltinClientID is the built-in client_id for the provider host of a profile, if there is one.
func BuiltinClientID(p Profile) (string, bool) {
	id := strings.TrimSpace(builtinClientIDs[OAuthHost(p)])
	return id, id != ""
}

// ResolveClientID returns the OAuth client_id of a profile and its source: the profile's own oauth2.client_id,
// else the built-in default of its provider host, else an error that says how to set one.
func ResolveClientID(name string, p Profile) (id, source string, err error) {
	if p.OAuth2 != nil {
		if id = strings.TrimSpace(p.OAuth2.ClientID); id != "" {
			return id, ClientIDOwn, nil
		}
	}
	if id, ok := BuiltinClientID(p); ok {
		return id, ClientIDBuiltIn, nil
	}
	return "", "", fmt.Errorf("profiles.%s.oauth2.client_id is not set and workctl has no built-in client_id for %q: set client_id: workctl profile edit %s --client-id ID", name, OAuthHost(p), name)
}
