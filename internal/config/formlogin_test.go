package config_test

import (
	"strings"
	"testing"

	"workctl/internal/config"
)

func TestValidateFormLoginAndReadOnly(t *testing.T) {
	login := &config.FormLogin{Path: "/login", UserField: "User", PasswordField: "Password", SessionCookie: "sid"}
	ok := config.Profile{Kind: "api", URL: "https://fleet.example.com", Auth: "form", Username: "me", VerifyTLS: true, ReadOnly: true, FormLogin: login}
	with := func(edit func(*config.Profile)) config.Profile { p := ok; edit(&p); return p }
	for name, tc := range map[string]struct {
		profile config.Profile
		wantErr string
	}{
		"valid":              {ok, ""},
		"no block":           {with(func(p *config.Profile) { p.FormLogin = nil }), "form_login is required"},
		"block without form": {with(func(p *config.Profile) { p.Auth = "basic" }), "not allowed otherwise"},
		"no username":        {with(func(p *config.Profile) { p.Username = "" }), "username is required"},
		"no cookie": {with(func(p *config.Profile) {
			p.FormLogin = &config.FormLogin{Path: "/login", UserField: "U", PasswordField: "P"}
		}), "session_cookie"},
		"form on jira":     {with(func(p *config.Profile) { p.Kind = "jira" }), "unsupported for jira"},
		"read-only gitlab": {config.Profile{Kind: "gitlab", URL: "https://gitlab.example.com", VerifyTLS: true, ReadOnly: true}, ""},
	} {
		err := config.ValidateProfile("p", tc.profile)
		if (tc.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.wantErr)) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.wantErr)
		}
	}
}
