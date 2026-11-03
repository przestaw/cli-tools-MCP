// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/config"
)

// formApp is a fake web application: a login page that sets a cookie check, a login form that needs that cookie and
// sets the session cookie "sid" for the right password, and a page that is served only with the session cookie.
func formApp(t *testing.T, logins *int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app/login":
			http.SetCookie(w, &http.Cookie{Name: "has_cookies", Value: "1", Path: "/app/"})
			_, _ = w.Write([]byte("<html>login form</html>"))
		case r.Method == http.MethodPost && r.URL.Path == "/app/login":
			*logins++
			if _, err := r.Cookie("has_cookies"); err != nil {
				http.Error(w, "browser without cookies", http.StatusBadRequest)
				return
			}
			if r.PostFormValue("Action") != "Login" || r.PostFormValue("User") != "me" || r.PostFormValue("Password") != "pw" {
				_, _ = w.Write([]byte("<html>login form</html>"))
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1", Path: "/app/"})
			http.Redirect(w, r, "/app/home", http.StatusFound)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/app/"):
			if cookie, err := r.Cookie("sid"); err != nil || cookie.Value != "s1" {
				_, _ = w.Write([]byte("<html>login form</html>"))
				return
			}
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><h1>Ticket 7</h1></html>"))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func formProfile(url string) config.Profile {
	return config.Profile{Kind: "api", URL: url + "/app", Auth: "form", Username: "me", TokenEnv: "WORKCTL_TEST_FORM_PASSWORD", VerifyTLS: true, ReadOnly: true,
		FormLogin: &config.FormLogin{Path: "/login", Fields: map[string]string{"Action": "Login"}, UserField: "User", PasswordField: "Password", SessionCookie: "sid"}}
}

func TestFormLoginReadsPagesAsHTML(t *testing.T) {
	logins := 0
	srv := formApp(t, &logins)
	t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
	c, err := NewProfileClient(config.Default(), "app", formProfile(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for range 2 {
		b, contentType, err := c.Do(ctx, http.MethodGet, "/ticket?id=7", nil, nil, "")
		if err != nil || string(b) != "<html><h1>Ticket 7</h1></html>" || contentType != "text/html" {
			t.Fatalf("page = %q %q %v", b, contentType, err)
		}
	}
	if logins != 1 {
		t.Fatalf("logged in %d times, want once per client", logins)
	}
	if _, _, err := c.Do(ctx, http.MethodPost, "/ticket", nil, []byte("{}"), ""); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("POST on a read-only profile: %v", err)
	}
}

func TestFormLoginReportsAWrongPassword(t *testing.T) {
	logins := 0
	srv := formApp(t, &logins)
	t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "wrong")
	c, err := NewProfileClient(config.Default(), "app", formProfile(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Do(context.Background(), http.MethodGet, "/ticket", nil, nil, "")
	if err == nil || !strings.Contains(err.Error(), "no sid cookie") || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("err = %v", err)
	}
}
