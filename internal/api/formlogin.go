// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Form login for web applications that have no API token (profiles with auth "form"): the client posts the login
// form once, like a browser, and the session cookie it gets back authenticates the later requests. Pages come back
// as the server renders them (usually HTML); reading them is up to the caller. Pair it with "read_only": true when
// the profile should only read.

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"workctl/internal/config"
)

// formSession is the login state of a form-login client. The password stays in memory and is only ever sent in the
// body of the login POST, never in a URL.
type formSession struct {
	login    config.FormLogin
	username string
	password string
	loggedIn bool
}

// logIn opens the login page, posts the login form and checks that the answer set the session cookie. Opening the
// page first collects the cookies a browser would have (many applications refuse a login without them). A wrong
// password usually comes back as the login page again (HTTP 200), so the cookie, not the status, tells whether the
// login worked.
func (c *Client) logIn(ctx context.Context) error {
	f := c.form
	if _, _, err := c.send(ctx, http.MethodGet, f.login.Path, c.headers.Clone(), nil, ""); err != nil {
		return fmt.Errorf("form login page: %w", err)
	}
	form := url.Values{}
	for key, value := range f.login.Fields {
		form.Set(key, value)
	}
	form.Set(f.login.UserField, f.username)
	form.Set(f.login.PasswordField, f.password)
	if _, _, err := c.send(ctx, http.MethodPost, f.login.Path, c.headers.Clone(), []byte(form.Encode()), "application/x-www-form-urlencoded"); err != nil {
		return fmt.Errorf("form login: %w", err)
	}
	loginURL, err := url.Parse(c.baseURL + f.login.Path)
	if err != nil {
		return err
	}
	for _, cookie := range c.client.Jar.Cookies(loginURL) {
		if cookie.Name == f.login.SessionCookie {
			f.loggedIn = true
			return nil
		}
	}
	return fmt.Errorf("form login as %q failed: the server set no %s cookie (check the user name and the stored password)", f.username, f.login.SessionCookie)
}
