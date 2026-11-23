package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/modules"
)

func TestClassifyError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"dns", &net.DNSError{Err: "no such host", Name: "x.test"}, "dns"},
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"refused", &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}, "refused"},
		{"tls", &tls.CertificateVerificationError{}, "tls"},
		{"401", &api.Error{Status: http.StatusUnauthorized}, "unauthorized"},
		{"403", &api.Error{Status: http.StatusForbidden}, "forbidden"},
		{"wrapped 401", errors.Join(errors.New("check"), &api.Error{Status: http.StatusUnauthorized}), "unauthorized"},
		{"other", errors.New("boom"), "other"},
		{"none", nil, ""},
	}
	for _, c := range cases {
		if got := classifyError(c.err); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestEveryCommandHelpIsPrintedAndToolsAreListed(t *testing.T) {
	for _, name := range []string{"api", "accounts", "pr", "mr", "tools", "config", "doctor"} {
		var out bytes.Buffer
		if !printCommandHelp(&out, name) || !strings.Contains(out.String(), "workctl") {
			t.Errorf("no help for %s", name)
		}
	}
	if !helpWanted([]string{"--help"}) || helpWanted([]string{"raw"}) {
		t.Fatal("helpWanted")
	}
	found := map[string]bool{}
	for _, tool := range toolList(config.Default(), false) {
		found[tool.Name] = true
	}
	for name := range cliEquivalent {
		if !found[name] {
			t.Errorf("cliEquivalent names unknown tool %q", name)
		}
	}
}

// captureRun runs the program with the given arguments and returns what it wrote to stdout and its exit code.
func captureRun(t *testing.T, args ...string) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	code := Main(args)
	os.Stdout = stdout
	w.Close()
	b, _ := io.ReadAll(r)
	return string(b), code
}

func TestHelpCoversEveryCommandOfTheRegistry(t *testing.T) {
	t.Setenv("WORKCTL_CONFIG", t.TempDir()+"/config.json")
	top, _ := captureRun(t)
	names := map[string]bool{"init": true, "setup": true, "auth": true, "config": true, "profile": true, "version": true, "help": true, "i": true}
	for name := range commandHandlers() {
		names[name] = true
	}
	for alias := range modules.Aliases {
		names[alias] = true
	}
	for name := range names {
		for _, args := range [][]string{{"help", name}, {name, "--help"}, {name, "-h"}} {
			out, code := captureRun(t, args...)
			if code != 0 || out == "" || out == top || !strings.Contains(strings.ToLower(out), canonicalCommand(name)) {
				t.Errorf("workctl %s: exit %d, help names the command: %v, is the top-level usage: %v", strings.Join(args, " "), code, strings.Contains(strings.ToLower(out), canonicalCommand(name)), out == top)
			}
		}
	}
	if err := run([]string{"help", "nonsense"}); err == nil || !strings.Contains(err.Error(), "unknown command") || !strings.Contains(err.Error(), "workctl help") {
		t.Errorf("help nonsense: %v", err)
	}
	if _, code := captureRun(t, "help", "nonsense"); code == 0 {
		t.Error("help nonsense exits 0")
	}
}
