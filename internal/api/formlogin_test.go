// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"workctl/internal/config"
)

// formApp is a fake web application: a login page that sets a cookie check, a login form that needs that cookie and
// sets the session cookie "sid" for the right password, and a page that is served only with the session cookie.
func formApp(t *testing.T, logins *int) *httptest.Server {
	t.Helper()
	// Keep the saved session out of the real user cache.
	for _, env := range []string{"XDG_CACHE_HOME", "HOME", "LocalAppData"} {
		t.Setenv(env, t.TempDir())
	}
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
				_, _ = w.Write([]byte(`<html><form><input name="User"><input name="Password" type="password"></form></html>`))
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

func TestFormLoginClosesTheSession(t *testing.T) {
	logins, logouts := 0, 0
	app := formApp(t, &logins)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/app/logout" {
			logouts++
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "", Path: "/app/", MaxAge: -1})
			return
		}
		app.Config.Handler.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
	profile := formProfile(srv.URL)
	profile.FormLogin.LogoutPath = "/logout"
	c, err := NewProfileClient(config.Default(), "app", profile)
	if err != nil {
		t.Fatal(err)
	}
	c.Close() // never logged in: nothing to end
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/ticket?id=7", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if logins != 1 || logouts != 1 {
		t.Fatalf("logins = %d, logouts = %d, want 1 and 1", logins, logouts)
	}
}

func TestFormLogoutRequiresSessionInvalidation(t *testing.T) {
	logins := 0
	srv := formApp(t, &logins)
	t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
	profile := formProfile(srv.URL)
	profile.FormLogin.LogoutPath = "/logout"
	c, err := NewProfileClient(config.Default(), "app", profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/home", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if !c.form.loggedIn {
		t.Fatal("HTTP 200 without invalidating the session must not mark it logged out")
	}
}

func TestFormLogoutWithChallengeToken(t *testing.T) {
	for _, tokenHTML := range []string{
		`<a href="/app/index.pl?Action=Logout;ChallengeToken=test%2Btoken&amp;Other=1">Logout</a>`,
		`<input value="test+token" name="ChallengeToken" type="hidden">`,
	} {
		for _, broken := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/broken=%v", tokenHTML[:2], broken), func(t *testing.T) {
				var active atomic.Bool
				var logouts, probes atomic.Int32
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == http.MethodPost {
						active.Store(true)
						http.SetCookie(w, &http.Cookie{Name: "sid", Value: "old-session", Path: "/app/"})
						_, _ = fmt.Fprint(w, tokenHTML)
						return
					}
					if r.URL.Query().Get("Action") == "Logout" {
						logouts.Add(1)
						cookie, err := r.Cookie("sid")
						if err != nil || cookie.Value != "old-session" || r.URL.Query().Get("ChallengeToken") != "test+token" {
							http.Error(w, "bad logout credentials", http.StatusForbidden)
							return
						}
						if !broken {
							active.Store(false)
						}
						http.SetCookie(w, &http.Cookie{Name: "sid", Path: "/app/", MaxAge: -1})
						return
					}
					if cookie, err := r.Cookie("sid"); err == nil && cookie.Value == "old-session" {
						probes.Add(1)
						if active.Load() {
							_, _ = fmt.Fprint(w, tokenHTML)
							return
						}
					}
					_, _ = fmt.Fprint(w, `<form><input name="Password" type="password"></form>`)
				}))
				t.Cleanup(srv.Close)
				t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
				profile := formProfile(srv.URL)
				profile.FormLogin.LogoutPath = "/index.pl?Action=Logout"
				profile.FormLogin.LogoutTokenField = "ChallengeToken"
				profile.FormLogin.LogoutVerifyPath = "/index.pl?Action=AgentPreferences"
				client, err := NewProfileClient(config.Default(), "app", profile)
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(context.Background())
				if _, _, err := client.Do(ctx, http.MethodGet, "/home", nil, nil, ""); err != nil {
					t.Fatal(err)
				}
				cancel()
				err = client.Close()
				if (err != nil) != broken || client.form.loggedIn != broken {
					t.Fatalf("Close = %v, loggedIn = %v, broken = %v", err, client.form.loggedIn, broken)
				}
				if logouts.Load() != 1 || probes.Load() != 2 {
					t.Fatalf("logouts = %d, probes with old cookie = %d", logouts.Load(), probes.Load())
				}
				if !broken {
					if err := client.Close(); err != nil || logouts.Load() != 1 {
						t.Fatalf("second Close = %v, logouts = %d", err, logouts.Load())
					}
					if err := client.CheckSession(context.Background()); err != nil {
						t.Fatalf("session-check = %v", err)
					}
					if logouts.Load() != 2 || probes.Load() != 4 {
						t.Fatalf("session-check logouts = %d, probes with old cookie = %d", logouts.Load(), probes.Load())
					}
				}
			})
		}
	}
}

func TestFormLogoutErrorsDoNotExposeCredentials(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			logins := 0
			app := formApp(t, &logins)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/app/logout" {
					http.Error(w, "private-cookie-value", status)
					return
				}
				app.Config.Handler.ServeHTTP(w, r)
			}))
			t.Cleanup(srv.Close)
			t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
			profile := formProfile(srv.URL)
			profile.FormLogin.LogoutPath = "/logout?ChallengeToken=private-token-value"
			client, err := NewProfileClient(config.Default(), "app", profile)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := client.Do(context.Background(), http.MethodGet, "/home", nil, nil, ""); err != nil {
				t.Fatal(err)
			}
			if err := client.Close(); err == nil || strings.Contains(err.Error(), "private-") || !client.form.loggedIn {
				t.Fatalf("Close = %v, loggedIn = %v", err, client.form.loggedIn)
			}
		})
	}
}

func TestFormLoginReusesTheSavedSession(t *testing.T) {
	logins := 0
	srv := formApp(t, &logins)
	t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
	profile := formProfile(srv.URL)
	ctx := context.Background()
	for range 2 { // two runs: the second one has to find the session of the first
		c, err := NewProfileClient(config.Default(), "app", profile)
		if err != nil {
			t.Fatal(err)
		}
		if b, _, err := c.Do(ctx, http.MethodGet, "/ticket?id=7", nil, nil, ""); err != nil || !strings.Contains(string(b), "Ticket 7") {
			t.Fatalf("page = %q %v", b, err)
		}
	}
	if logins != 1 {
		t.Fatalf("logged in %d times, want once for both runs", logins)
	}
	// The server forgets the session: the saved cookie is refused, so the run logs in again and repeats the request.
	if err := os.WriteFile(sessionFile("app", profile), []byte(`{"sid":"stale"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := NewProfileClient(config.Default(), "app", profile)
	if err != nil {
		t.Fatal(err)
	}
	if b, _, err := c.Do(ctx, http.MethodGet, "/ticket?id=7", nil, nil, ""); err != nil || !strings.Contains(string(b), "Ticket 7") {
		t.Fatalf("page after expiry = %q %v", b, err)
	}
	if logins != 2 {
		t.Fatalf("logged in %d times, want 2 after the session expired", logins)
	}
}

func TestFormLoginWithLogoutKeepsNoSessionFile(t *testing.T) {
	logins := 0
	srv := formApp(t, &logins)
	t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
	profile := formProfile(srv.URL)
	profile.FormLogin.LogoutPath = "/logout"
	if file := sessionFile("app", profile); file != "" {
		t.Fatalf("a profile that logs out must not keep a session file, got %s", file)
	}
}

func TestForgetSessionDeletesTheSavedSession(t *testing.T) {
	logins := 0
	srv := formApp(t, &logins)
	t.Setenv("WORKCTL_TEST_FORM_PASSWORD", "pw")
	profile := formProfile(srv.URL)
	c, err := NewProfileClient(config.Default(), "app", profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Do(context.Background(), http.MethodGet, "/ticket?id=7", nil, nil, ""); err != nil {
		t.Fatal(err)
	}
	if removed, err := ForgetSession("app", profile); err != nil || !removed {
		t.Fatalf("first forget: removed=%v err=%v", removed, err)
	}
	if removed, err := ForgetSession("app", profile); err != nil || removed {
		t.Fatalf("second forget: removed=%v err=%v", removed, err)
	}
}
