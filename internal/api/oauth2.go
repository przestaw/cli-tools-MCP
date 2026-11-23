// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// OAuth 2.0 refresh-token grant and loopback PKCE authorisation for profiles with auth "oauth2" (for example Google
// Drive and Google Colab APIs). The refresh token and optional client secret stay in the profile's keyring entry;
// short-lived access tokens are cached in the user cache directory with mode 0600.

package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/httpx"
)

// OAuth2Credential is the JSON secret stored in the keyring (or token_env) for an auth "oauth2" profile.
// A plain string secret is also accepted and treated as ClientSecret before login or RefreshToken when prefixed.
type OAuth2Credential struct {
	ClientSecret string `json:"client_secret,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
}

// ParseOAuth2Credential decodes the stored credential of an oauth2 profile.
// If raw is JSON, client_secret and refresh_token are read from it; otherwise a non-empty plain value is treated as
// client_secret so `workctl auth set-token` followed by `workctl auth login` works with a single pasted secret.
func ParseOAuth2Credential(raw string) OAuth2Credential {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return OAuth2Credential{}
	}
	if strings.HasPrefix(raw, "{") {
		var cred OAuth2Credential
		if json.Unmarshal([]byte(raw), &cred) == nil {
			return cred
		}
	}
	return OAuth2Credential{ClientSecret: raw}
}

// Encode serialises the credential for keyring storage.
func (c OAuth2Credential) Encode() (string, error) {
	b, err := json.Marshal(c)
	return string(b), err
}

type oauth2Session struct {
	mu          sync.Mutex
	profileName string
	cfg         config.OAuth2
	cred        OAuth2Credential
	httpClient  *http.Client
	cacheFile   string
	accessToken string
	expiresAt   time.Time
}

type cachedAccessToken struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func newOAuth2Session(name string, profile config.Profile, rawSecret string, base *http.Client) (*oauth2Session, error) {
	cred := ParseOAuth2Credential(rawSecret)
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return nil, fmt.Errorf("profile %q has no OAuth2 refresh token yet: run `workctl auth login --profile %s`", name, name)
	}
	clientID, _, err := config.ResolveClientID(name, profile)
	if err != nil {
		return nil, err
	}
	profile = withClientID(profile, clientID)
	tc := *base
	tc.Jar = nil
	return &oauth2Session{
		profileName: name,
		cfg:         *profile.OAuth2,
		cred:        cred,
		httpClient:  &tc,
		cacheFile:   oauth2CacheFile(name, profile),
	}, nil
}

// withClientID returns the profile with its OAuth client_id replaced by the resolved one (own or built-in).
func withClientID(p config.Profile, id string) config.Profile {
	o := *p.OAuth2
	o.ClientID = id
	p.OAuth2 = &o
	return p
}

func oauth2CacheFile(name string, p config.Profile) string {
	dir, err := os.UserCacheDir()
	if err != nil || p.OAuth2 == nil {
		return ""
	}
	if id, _, err := config.ResolveClientID(name, p); err == nil {
		p = withClientID(p, id)
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{name, p.URL, p.OAuth2.ClientID, p.OAuth2.TokenURL, strings.Join(p.OAuth2.Scopes, " ")}, "\x00")))
	return filepath.Join(dir, "workctl", "oauth2-"+hex.EncodeToString(sum[:8])+".json")
}

// ForgetOAuth2Token deletes any cached access token of an oauth2 profile (`workctl auth logout`).
func ForgetOAuth2Token(name string, p config.Profile) (bool, error) {
	f := oauth2CacheFile(name, p)
	if f == "" {
		return false, nil
	}
	err := os.Remove(f)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

const tokenExpirySkew = 60 * time.Second

func (s *oauth2Session) token(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	if s.accessToken != "" && now.Add(tokenExpirySkew).Before(s.expiresAt) {
		return s.accessToken, nil
	}
	if s.loadCached(now) {
		return s.accessToken, nil
	}
	return s.refresh(ctx, now)
}

// invalidate forgets the current access token, in memory and on disk, so the next call refreshes it.
func (s *oauth2Session) invalidate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accessToken, s.expiresAt = "", time.Time{}
	if s.cacheFile != "" {
		if err := os.Remove(s.cacheFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove rejected OAuth token cache: %w", err)
		}
	}
	return nil
}

func (s *oauth2Session) loadCached(now time.Time) bool {
	if s.cacheFile == "" {
		return false
	}
	b, err := os.ReadFile(s.cacheFile)
	if err != nil {
		return false
	}
	var c cachedAccessToken
	if json.Unmarshal(b, &c) != nil || c.AccessToken == "" || !now.Add(tokenExpirySkew).Before(c.ExpiresAt) {
		return false
	}
	s.accessToken = c.AccessToken
	s.expiresAt = c.ExpiresAt
	return true
}

func (s *oauth2Session) saveCached() {
	if s.cacheFile == "" || os.MkdirAll(filepath.Dir(s.cacheFile), 0o700) != nil {
		return
	}
	b, err := json.Marshal(cachedAccessToken{AccessToken: s.accessToken, ExpiresAt: s.expiresAt})
	if err == nil {
		_ = os.WriteFile(s.cacheFile, b, 0o600)
	}
}

func (s *oauth2Session) refresh(ctx context.Context, now time.Time) (string, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {s.cfg.ClientID},
		"refresh_token": {s.cred.RefreshToken},
	}
	if s.cred.ClientSecret != "" {
		form.Set("client_secret", s.cred.ClientSecret)
	}
	tok, err := exchangeOAuth2Token(ctx, s.httpClient, config.NormalizeBaseURL(s.cfg.TokenURL), form)
	if err != nil {
		if strings.Contains(err.Error(), "invalid_grant") {
			return "", fmt.Errorf("login for profile %q expired or was revoked: run `workctl auth login --profile %s` (%w)", s.profileName, s.profileName, err)
		}
		return "", fmt.Errorf("oauth2 token refresh for profile %q: %w", s.profileName, err)
	}
	s.accessToken = tok.AccessToken
	s.expiresAt = tok.expiry(now)
	s.saveCached()
	return s.accessToken, nil
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
}

func (t tokenResponse) expiry(now time.Time) time.Time {
	return now.Add(time.Duration(max(t.ExpiresIn, 0)) * time.Second)
}

func exchangeOAuth2Token(ctx context.Context, hc *http.Client, tokenURL string, form url.Values) (tokenResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return tokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return tokenResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("read token response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var oauthErr struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &oauthErr)
		if oauthErr.Error != "" {
			if oauthErr.Description != "" {
				return tokenResponse{}, fmt.Errorf("HTTP %d: %s (%s)", resp.StatusCode, oauthErr.Error, oauthErr.Description)
			}
			return tokenResponse{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, oauthErr.Error)
		}
		return tokenResponse{}, fmt.Errorf("HTTP %d from token endpoint", resp.StatusCode)
	}
	var tok tokenResponse
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		return tokenResponse{}, fmt.Errorf("token endpoint did not return an access_token")
	}
	return tok, nil
}

// LoginOAuth2 runs an interactive OAuth 2.0 Authorization Code + PKCE loopback flow on 127.0.0.1 and returns the
// updated credential with its refresh_token. openURL receives the authorisation URL to show or open for the user.
func LoginOAuth2(ctx context.Context, cfg *config.Config, name string, profile config.Profile, existing OAuth2Credential, openURL func(string)) (OAuth2Credential, error) {
	if profile.OAuth2 == nil || strings.TrimSpace(profile.OAuth2.AuthURL) == "" {
		return OAuth2Credential{}, fmt.Errorf("profile %q needs oauth2.auth_url in config.json for interactive login", name)
	}
	clientID, _, err := config.ResolveClientID(name, profile)
	if err != nil {
		return OAuth2Credential{}, err
	}
	profile = withClientID(profile, clientID)
	verifier, challenge, err := pkcePair()
	if err != nil {
		return OAuth2Credential{}, err
	}
	state, err := randomURLToken(18)
	if err != nil {
		return OAuth2Credential{}, err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return OAuth2Credential{}, fmt.Errorf("listen on loopback callback: %w", err)
	}
	defer func() { _ = ln.Close() }()
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	type callbackResult struct {
		code string
		err  error
	}
	ch := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			// Not our redirect (stray request): answer it but keep waiting, so it cannot abort the login.
			http.Error(w, "Invalid OAuth2 callback state.", http.StatusBadRequest)
			return
		}
		res := callbackResult{code: q.Get("code")}
		switch errCode := q.Get("error"); {
		case errCode != "":
			http.Error(w, "Authorization declined: "+errCode, http.StatusBadRequest)
			res = callbackResult{err: fmt.Errorf("authorization failed: %s", errCode)}
		case res.code == "":
			http.Error(w, "Missing authorization code.", http.StatusBadRequest)
			res = callbackResult{err: errors.New("authorization callback carried no code")}
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, "<!doctype html><html><body><h2>workctl: authorization complete</h2><p>You can close this tab and return to the terminal.</p></body></html>")
		}
		select {
		case ch <- res:
		default:
		}
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer func() { _ = srv.Close() }()

	authURL, err := url.Parse(config.NormalizeBaseURL(profile.OAuth2.AuthURL))
	if err != nil {
		return OAuth2Credential{}, err
	}
	q := authURL.Query()
	q.Set("client_id", profile.OAuth2.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	for k, v := range profile.OAuth2.AuthParams {
		q.Set(k, v)
	}
	if len(profile.OAuth2.Scopes) > 0 {
		q.Set("scope", strings.Join(profile.OAuth2.Scopes, " "))
	}
	authURL.RawQuery = q.Encode()

	if openURL != nil {
		openURL(authURL.String())
	}

	var res callbackResult
	select {
	case <-ctx.Done():
		return OAuth2Credential{}, ctx.Err()
	case res = <-ch:
	}
	if res.err != nil {
		return OAuth2Credential{}, res.err
	}

	tr, err := httpx.Transport(profile.VerifyTLS, profile.CAFile)
	if err != nil {
		return OAuth2Credential{}, err
	}
	tr.Proxy = http.ProxyFromEnvironment
	hc := &http.Client{Timeout: cfg.ProfileTimeout(profile), Transport: tr}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {res.code},
		"client_id":     {profile.OAuth2.ClientID},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	if existing.ClientSecret != "" {
		form.Set("client_secret", existing.ClientSecret)
	}
	tok, err := exchangeOAuth2Token(ctx, hc, config.NormalizeBaseURL(profile.OAuth2.TokenURL), form)
	if err != nil {
		return OAuth2Credential{}, fmt.Errorf("exchange authorization code: %w", err)
	}
	refresh := cliutil.FirstNonEmpty(tok.RefreshToken, existing.RefreshToken)
	if refresh == "" {
		return OAuth2Credential{}, fmt.Errorf("token endpoint returned no refresh_token (for Google add auth_params access_type=offline and prompt=consent)")
	}
	out := OAuth2Credential{ClientSecret: existing.ClientSecret, RefreshToken: refresh}

	// Cache the initial access token immediately so the next call needs no extra round-trip.
	sess := &oauth2Session{
		profileName: name,
		cfg:         *profile.OAuth2,
		cred:        out,
		cacheFile:   oauth2CacheFile(name, profile),
		accessToken: tok.AccessToken,
		expiresAt:   tok.expiry(time.Now()),
	}
	sess.saveCached()
	return out, nil
}

func pkcePair() (verifier, challenge string, err error) {
	verifier, err = randomURLToken(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

func randomURLToken(numBytes int) (string, error) {
	b := make([]byte, numBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
