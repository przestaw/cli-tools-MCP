// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"
)

// GoogleKind is the profile kind that selects a Google service preset. Load turns it into an api profile with
// auth "oauth2", so the HTTP, upload and request code has one path for every OAuth service.
const GoogleKind = "google"

// GoogleService is the preset behind one `service` of a google profile. BaseURL is where relative request paths go.
// Hosts are the only hosts the profile's token may be sent to ("*.googleapis.com" matches every subdomain);
// requests to them may use absolute URLs, so one profile reaches several Google APIs.
type GoogleService struct {
	BaseURL string
	Hosts   []string
	Scopes  []string
}

const (
	scopeDrive     = "https://www.googleapis.com/auth/drive"
	scopeCloud     = "https://www.googleapis.com/auth/cloud-platform"
	scopeColab     = "https://www.googleapis.com/auth/colaboratory"
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
)

// googleServices is the single table of Google presets: drive (profile gdrive), cloud (gcloud) and all (google).
// drive also reaches the Sheets and Docs APIs: both accept the drive scope, so the token gains no new rights.
// The scopes are defaults; oauth2.scopes in the profile replaces them, for example with narrower read-only ones.
var googleServices = map[string]GoogleService{
	"drive": {BaseURL: "https://www.googleapis.com/drive/v3", Hosts: []string{"www.googleapis.com", "sheets.googleapis.com", "docs.googleapis.com"}, Scopes: []string{scopeDrive}},
	"cloud": {BaseURL: "https://cloudresourcemanager.googleapis.com/v1", Hosts: []string{"*.googleapis.com"}, Scopes: []string{scopeCloud}},
	"all":   {BaseURL: "https://www.googleapis.com/drive/v3", Hosts: []string{"*.googleapis.com"}, Scopes: []string{scopeDrive, scopeCloud, scopeColab}},
}

// GoogleServiceNames lists the valid `service` values.
func GoogleServiceNames() []string {
	return slices.Sorted(maps.Keys(googleServices))
}

// HasDrive reports whether the profile talks to Google Drive (base URL is the Drive v3 API).
func (p Profile) HasDrive() bool {
	return strings.HasSuffix(strings.ToLower(NormalizeBaseURL(p.URL)), "/drive/v3")
}

// AbsoluteHosts are the hosts that may be addressed with an absolute URL; empty for every non-Google profile.
func (p Profile) AbsoluteHosts() []string { return googleServices[p.Service].Hosts }

// HostAllowed reports whether host matches one of the patterns (exact name or "*.suffix").
func HostAllowed(patterns []string, host string) bool {
	host = strings.ToLower(host)
	for _, pattern := range patterns {
		if suffix, ok := strings.CutPrefix(pattern, "*"); ok {
			if strings.HasSuffix(host, suffix) {
				return true
			}
		} else if host == pattern {
			return true
		}
	}
	return false
}

// ExpandGoogle fills a google profile from its service preset and turns it into a kind api profile.
// The user supplies the service and oauth2.client_id; everything else has a default that the profile may override.
func ExpandGoogle(name string, p Profile) (Profile, error) {
	original := p
	svc, ok := googleServices[p.Service]
	if !ok {
		return p, fmt.Errorf("profiles.%s.service %q is unsupported for kind google (use %s)", name, p.Service, strings.Join(GoogleServiceNames(), ", "))
	}
	if p.Auth != "" && !strings.EqualFold(p.Auth, "oauth2") {
		return p, fmt.Errorf("profiles.%s.auth must be \"oauth2\" for kind google", name)
	}
	if p.OAuth2 == nil {
		return p, fmt.Errorf("profiles.%s.oauth2.client_id is required for kind google", name)
	}
	o := *p.OAuth2
	o.Scopes = slices.Clone(o.Scopes)
	o.AuthParams = maps.Clone(o.AuthParams)
	if o.AuthURL == "" {
		o.AuthURL = googleAuthURL
	}
	if o.TokenURL == "" {
		o.TokenURL = googleTokenURL
	}
	if len(o.Scopes) == 0 {
		o.Scopes = svc.Scopes
	}
	if o.AuthParams == nil {
		// Without access_type=offline and prompt=consent Google issues no refresh token.
		o.AuthParams = map[string]string{"access_type": "offline", "prompt": "consent"}
	}
	if p.URL == "" {
		p.URL = svc.BaseURL
	}
	if u, err := url.Parse(p.URL); err != nil || !HostAllowed(svc.Hosts, u.Hostname()) {
		return p, fmt.Errorf("profiles.%s.url must be a host of the %q service (%s)", name, p.Service, strings.Join(svc.Hosts, ", "))
	}
	p.googleDeclaration = &original
	p.Kind, p.Auth, p.OAuth2, p.VerifyTLS = "api", "oauth2", &o, true
	return p, nil
}

// declaredGoogle removes only unchanged runtime defaults, retaining edits such as account bindings and scopes.
func (p Profile) declaredGoogle() (Profile, error) {
	source := p.googleDeclaration
	if source == nil || p.Kind != "api" || p.Service != source.Service {
		return p, nil
	}
	baseline, err := ExpandGoogle("", *source)
	if err != nil {
		return p, err
	}
	p.Kind = source.Kind
	if p.URL == baseline.URL {
		p.URL = source.URL
	}
	if p.Auth == baseline.Auth {
		p.Auth = source.Auth
	}
	if p.VerifyTLS == baseline.VerifyTLS {
		p.VerifyTLS = source.VerifyTLS
	}
	if p.OAuth2 != nil {
		o := *p.OAuth2
		if o.AuthURL == baseline.OAuth2.AuthURL {
			o.AuthURL = source.OAuth2.AuthURL
		}
		if o.TokenURL == baseline.OAuth2.TokenURL {
			o.TokenURL = source.OAuth2.TokenURL
		}
		if slices.Equal(o.Scopes, baseline.OAuth2.Scopes) {
			o.Scopes = source.OAuth2.Scopes
		}
		if maps.Equal(o.AuthParams, baseline.OAuth2.AuthParams) {
			o.AuthParams = source.OAuth2.AuthParams
		}
		p.OAuth2 = &o
	}
	return p, nil
}
