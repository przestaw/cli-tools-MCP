package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"testing"

	"workctl/internal/api"
	"workctl/internal/config"
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
