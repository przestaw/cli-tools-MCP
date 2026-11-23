// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package auth

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/secret"
)

// Credential states of a profile.
const (
	StateOK        = "ok"             // a credential is present (or gh/glab is logged in)
	StateMissing   = "missing"        // no credential: store one
	StateLoginNeed = "login_required" // an OAuth or gh/glab login is needed
	StateNotNeeded = "not_needed"     // the profile keeps no workctl token (auth none, ssh key)
	StateError     = "error"          // the credential store or gh/glab could not be read
)

// Where a credential comes from (Credential.Source).
const (
	sourceKeyring = "keyring"
	sourceEnv     = "env"
	sourceCLI     = "cli"
)

// Credential is the credential state of one profile. It never holds a secret.
type Credential struct {
	State  string `json:"state"`
	Source string `json:"source,omitempty"` // keyring, env or cli
	Detail string `json:"detail"`           // the human-readable text of `accounts` and `auth status`
	Fix    string `json:"fix,omitempty"`    // the command that repairs a missing or incomplete credential
	// ClientIDSource is "own" or "built-in" for an OAuth profile whose client_id resolves (see config.ResolveClientID).
	ClientIDSource string `json:"client_id_source,omitempty"`
}

// LoginHint is the command that gives a profile a credential: a login for OAuth and gh/glab profiles, else a token.
// It is empty for profiles that keep no workctl token.
func LoginHint(name string, prof config.Profile) string {
	switch {
	case prof.UsesCLI():
	case config.NoTokenError(name, prof) != nil:
		return ""
	case !strings.EqualFold(prof.Auth, "oauth2"):
		return "workctl auth set-token --profile " + name
	}
	return "workctl auth login --profile " + name
}

// ClientIDSource says whether the OAuth client_id of a profile is "own" or "built-in"; it is empty for other profiles
// and when no client_id resolves.
func ClientIDSource(name string, prof config.Profile) string {
	if prof.OAuth2 == nil || !strings.EqualFold(prof.Auth, "oauth2") {
		return ""
	}
	_, src, _ := config.ResolveClientID(name, prof)
	return src
}

// clientIDNote is the suffix of an `auth status` line that names the client_id source.
func clientIDNote(source string) string {
	if source == "" {
		return ""
	}
	return " (client_id: " + source + ")"
}

// CredentialStatus reports whether a profile's credential is present without revealing any secret.
func CredentialStatus(cfg *config.Config, name string, prof config.Profile) string {
	return ProfileCredential(cfg, name, prof).Detail
}

// ProfileCredential looks at the credential of a profile without revealing any secret.
func ProfileCredential(cfg *config.Config, name string, prof config.Profile) Credential {
	c := credentialState(cfg, name, prof)
	c.ClientIDSource = ClientIDSource(name, prof)
	if c.State == StateMissing || c.State == StateLoginNeed {
		c.Fix = LoginHint(name, prof)
	}
	return c
}

func credentialState(cfg *config.Config, name string, prof config.Profile) Credential {
	if prof.UsesCLI() {
		return cliCredential(cfg, prof)
	}
	if err := config.NoTokenError(name, prof); err != nil {
		return Credential{State: StateNotNeeded, Detail: err.Error()}
	}
	oauthWithoutRefresh := func(raw string) bool {
		return strings.EqualFold(prof.Auth, "oauth2") && api.ParseOAuth2Credential(raw).RefreshToken == ""
	}
	if prof.TokenEnv != "" && os.Getenv(prof.TokenEnv) != "" {
		if oauthWithoutRefresh(os.Getenv(prof.TokenEnv)) {
			return Credential{State: StateLoginNeed, Source: sourceEnv, Detail: fmt.Sprintf("client_secret in %s; run `workctl auth login --profile %s` for refresh token", prof.TokenEnv, name)}
		}
		return Credential{State: StateOK, Source: sourceEnv, Detail: "set via environment variable " + prof.TokenEnv}
	}
	val, err := config.TokenStore.Get(secret.Account(prof.Kind, name))
	switch {
	case err == nil && val != "":
		if oauthWithoutRefresh(val) {
			return Credential{State: StateLoginNeed, Source: sourceKeyring, Detail: fmt.Sprintf("client_secret stored; run `workctl auth login --profile %s`", name)}
		}
		return Credential{State: StateOK, Source: sourceKeyring, Detail: "stored in keyring"}
	case err != nil && !errors.Is(err, secret.ErrNotFound):
		return Credential{State: StateError, Detail: fmt.Sprintf("keyring error: %v", err)}
	}
	return Credential{State: StateMissing, Detail: "missing"}
}

// cliCredential says which account the CLI of a cli profile is logged in as.
func cliCredential(cfg *config.Config, prof config.Profile) Credential {
	tool, who, err := api.CLIWhoami(cfg, prof)
	switch {
	case err != nil && tool == "":
		return Credential{State: StateError, Source: sourceCLI, Detail: err.Error()}
	case err != nil:
		return Credential{State: StateLoginNeed, Source: sourceCLI, Detail: "not logged in: " + err.Error()}
	}
	return Credential{State: StateOK, Source: sourceCLI, Detail: fmt.Sprintf("logged in as %s via %s", who, tool)}
}
