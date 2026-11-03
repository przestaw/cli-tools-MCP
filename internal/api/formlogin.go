// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Form login for web applications that have no API token (profiles with auth "form"): the client posts the login
// form once, like a browser, and the session cookie it gets back authenticates the later requests. Pages come back
// as the server renders them (usually HTML); reading them is up to the caller. Pair it with "read_only": true when
// the profile should only read.

package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/net/html"

	"workctl/internal/config"
)

// formSession is the login state of a form-login client. The password stays in memory and is only ever sent in the
// body of the login POST, never in a URL.
type formSession struct {
	login         config.FormLogin
	username      string
	password      string
	loggedIn      bool
	logoutToken   string
	logoutCookies []*http.Cookie
	cache         string // file that keeps the session cookies between runs (profiles without logout_path); empty when none
	cached        bool   // the session came from that file and has not been used yet
}

// logIn opens the login page, posts the login form and checks that the answer set the session cookie. Opening the
// page first collects the cookies a browser would have (many applications refuse a login without them). A wrong
// password usually comes back as the login page again (HTTP 200), so the cookie, not the status, tells whether the
// login worked.
func (c *Client) logIn(ctx context.Context) error {
	f := c.form
	if _, _, err := c.send(ctx, http.MethodGet, f.login.Path, c.headers.Clone(), nil, ""); err != nil {
		return sessionError("form login page", err)
	}
	form := url.Values{}
	for key, value := range f.login.Fields {
		form.Set(key, value)
	}
	form.Set(f.login.UserField, f.username)
	form.Set(f.login.PasswordField, f.password)
	data, _, loginErr := c.send(ctx, http.MethodPost, f.login.Path, c.headers.Clone(), []byte(form.Encode()), "application/x-www-form-urlencoded")
	f.logoutToken, _ = formHTML(data, f.login.LogoutTokenField, f.login.PasswordField)
	loginURL, err := url.Parse(c.baseURL + f.login.Path)
	if err != nil {
		return err
	}
	for _, cookie := range c.client.Jar.Cookies(loginURL) {
		if cookie.Name == f.login.SessionCookie {
			f.loggedIn = true
			if loginErr == nil {
				c.saveSession()
			}
			return sessionError("form login", loginErr)
		}
	}
	if loginErr != nil {
		return sessionError("form login", loginErr)
	}
	return fmt.Errorf("form login as %q failed: the server set no %s cookie (check the user name and the stored password)", f.username, f.login.SessionCookie)
}

func (c *Client) Close() error {
	if c.form == nil || !c.form.loggedIn || c.form.login.LogoutPath == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f := c.form
	verifyPath := f.login.LogoutVerifyPath
	if verifyPath == "" {
		verifyPath = f.login.Path
	}
	verifyURL, err := url.Parse(c.baseURL + verifyPath)
	if err != nil {
		return fmt.Errorf("form logout: invalid verification path")
	}
	if f.logoutCookies == nil {
		f.logoutCookies = c.client.Jar.Cookies(verifyURL)
	}
	if f.login.LogoutTokenField != "" && f.logoutToken == "" {
		data, _, err := c.send(ctx, http.MethodGet, verifyPath, c.headers.Clone(), nil, "")
		if err != nil {
			return sessionError("form logout token", err)
		}
		f.logoutToken, _ = formHTML(data, f.login.LogoutTokenField, f.login.PasswordField)
		if f.logoutToken == "" {
			return fmt.Errorf("form logout: required token is missing; session cleanup is unverified")
		}
	}
	target, err := url.Parse(f.login.LogoutPath)
	if err != nil {
		return fmt.Errorf("form logout: invalid logout path")
	}
	if f.login.LogoutTokenField != "" {
		query, err := url.ParseQuery(strings.ReplaceAll(target.RawQuery, ";", "&"))
		if err != nil {
			return fmt.Errorf("form logout: invalid logout query")
		}
		query.Set(f.login.LogoutTokenField, f.logoutToken)
		target.RawQuery = query.Encode()
	}
	if _, _, err := c.send(ctx, http.MethodGet, target.String(), c.headers.Clone(), nil, ""); err != nil {
		return sessionError("form logout", err)
	}
	if f.login.LogoutVerifyPath != "" {
		probe := *c
		transport := *c.client
		probe.client = &transport
		transport.Jar, _ = cookiejar.New(nil)
		transport.Jar.SetCookies(verifyURL, f.logoutCookies)
		data, _, err := probe.send(ctx, http.MethodGet, verifyPath, c.headers.Clone(), nil, "")
		if err != nil {
			return sessionError("form logout verification", err)
		}
		if _, loginForm := formHTML(data, "", f.login.PasswordField); !loginForm {
			return fmt.Errorf("form logout: original session was not rejected by the verification page")
		}
	} else {
		for _, cookie := range c.client.Jar.Cookies(verifyURL) {
			if cookie.Name == f.login.SessionCookie {
				return fmt.Errorf("form logout: session cookie remains; configure logout_verify_path to verify invalidation")
			}
		}
	}
	f.loggedIn = false
	f.logoutToken = ""
	f.logoutCookies = nil
	c.token = nil
	return nil
}

func (c *Client) CheckSession(ctx context.Context) error {
	if c.form == nil || c.form.login.LogoutPath == "" || c.form.login.LogoutVerifyPath == "" {
		return fmt.Errorf("session-check requires form login with logout_path and logout_verify_path")
	}
	data, _, requestErr := c.Do(ctx, http.MethodGet, c.form.login.LogoutVerifyPath, nil, nil, "")
	if requestErr == nil {
		if _, loginForm := formHTML(data, "", c.form.login.PasswordField); loginForm {
			requestErr = fmt.Errorf("verification page is not authenticated")
		}
	}
	return errors.Join(sessionError("session check", requestErr), c.Close())
}

func sessionError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var status *Error
	if errors.As(err, &status) {
		return fmt.Errorf("%s: HTTP %d (response and URL withheld)", operation, status.Status)
	}
	return fmt.Errorf("%s failed (details withheld to protect session credentials)", operation)
}

func formHTML(data []byte, tokenField, passwordField string) (string, bool) {
	parser := html.NewTokenizer(strings.NewReader(string(data)))
	var token string
	var loginForm bool
	for {
		switch parser.Next() {
		case html.ErrorToken:
			return token, loginForm
		case html.StartTagToken, html.SelfClosingTagToken:
			tag := parser.Token()
			attrs := make(map[string]string, len(tag.Attr))
			for _, attr := range tag.Attr {
				attrs[attr.Key] = attr.Val
			}
			if tag.Data == "input" {
				if attrs["name"] == passwordField && strings.EqualFold(attrs["type"], "password") {
					loginForm = true
				}
				if tokenField != "" && attrs["name"] == tokenField {
					token = attrs["value"]
				}
			}
			if tokenField != "" {
				for _, attr := range []string{"href", "action"} {
					link, err := url.Parse(attrs[attr])
					if err != nil {
						continue
					}
					query, err := url.ParseQuery(strings.ReplaceAll(link.RawQuery, ";", "&"))
					if err == nil && query.Get(tokenField) != "" {
						token = query.Get(tokenField)
					}
				}
			}
		}
	}
}

// sessionFile is where the session cookies of a form-login profile are kept between runs, so a profile holds one
// logged-in session instead of one per command (servers cap the sessions per user). Empty without a user cache
// directory, and for a profile with logout_path: that one logs out after every run instead.
func sessionFile(name string, p config.Profile) string {
	if p.FormLogin != nil && p.FormLogin.LogoutPath != "" {
		return ""
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(name + "\x00" + p.URL + "\x00" + p.Username))
	return filepath.Join(dir, "workctl", "session-"+hex.EncodeToString(sum[:8])+".json")
}

// loadSession puts the saved cookies into the jar and reports whether the file held the session cookie.
func (c *Client) loadSession() bool {
	f := c.form
	data, err := os.ReadFile(f.cache)
	if err != nil {
		return false
	}
	var saved map[string]string
	u, urlErr := url.Parse(c.baseURL + f.login.Path)
	if json.Unmarshal(data, &saved) != nil || urlErr != nil || saved[f.login.SessionCookie] == "" {
		return false
	}
	var cookies []*http.Cookie
	for name, value := range saved {
		cookies = append(cookies, &http.Cookie{Name: name, Value: value})
	}
	c.client.Jar.SetCookies(u, cookies)
	return true
}

// saveSession writes the jar's cookies to the session file (0600); failing to write only costs a login next time.
func (c *Client) saveSession() {
	f := c.form
	u, err := url.Parse(c.baseURL + f.login.Path)
	if err != nil || f.cache == "" || os.MkdirAll(filepath.Dir(f.cache), 0o700) != nil {
		return
	}
	saved := map[string]string{}
	for _, cookie := range c.client.Jar.Cookies(u) {
		saved[cookie.Name] = cookie.Value
	}
	if data, err := json.Marshal(saved); err == nil {
		_ = os.WriteFile(f.cache, data, 0o600)
	}
}

// dropSession forgets a session the server no longer accepts: the file, the cookies and the logged-in flag.
func (c *Client) dropSession() {
	_ = os.Remove(c.form.cache)
	c.form.loggedIn, c.form.cached = false, false
	c.client.Jar, _ = cookiejar.New(nil)
}

// ForgetSession deletes the saved session of a form-login profile (`workctl auth logout`); it reports whether
// there was one.
func ForgetSession(name string, p config.Profile) (bool, error) {
	file := sessionFile(name, p)
	if file == "" || !strings.EqualFold(p.Auth, "form") {
		return false, nil
	}
	err := os.Remove(file)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}
