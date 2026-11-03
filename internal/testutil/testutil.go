// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package testutil holds helpers shared by the test suites of several packages:
// fake gh/glab and module child processes (no network, no real tools) and the
// repository root lookup for tests that read checked-in files.
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"workctl/internal/proc"
)

// CLIHelperProcess is the body of the fake gh/glab. Each test package that uses FakeCLI declares
//
//	func TestCLIHelperProcess(t *testing.T) { testutil.CLIHelperProcess() }
//
// so the re-run of its own test binary can act as the CLI. The mode comes from WORKCTL_TEST_CLI;
// it is a no-op in a normal test run.
func CLIHelperProcess() {
	mode := os.Getenv("WORKCTL_TEST_CLI")
	if mode == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch mode {
	case "ok":
		stdin, _ := io.ReadAll(os.Stdin)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": args, "stdin": string(stdin), "ghToken": os.Getenv("GH_TOKEN")})
	case "404":
		_, _ = os.Stdout.WriteString(`{"message":"Not Found"}`)
		_, _ = os.Stderr.WriteString("gh: Not Found (HTTP 404)")
		os.Exit(1)
	case "auth":
		_, _ = os.Stderr.WriteString("To get started with GitHub CLI, please run:  gh auth login")
		os.Exit(4)
	case "user": // `auth login` succeeds silently, any API call answers with the logged-in account
		if args[1] != "auth" {
			_, _ = os.Stdout.WriteString(`{"login":"przestaw","username":"przestaw"}`)
		}
	case "token": // `gh auth token` prints the token of the requested account
		_, _ = os.Stdout.WriteString("gho_faketoken\n")
	case "sleep":
		time.Sleep(10 * time.Second)
	case "ssh-w": // `ssh -W host:port ...` forwards its stdio to host:port
		for i, a := range args {
			if a == "-W" {
				forwardStdio(args[i+1])
			}
		}
	case "ssh-denied":
		_, _ = os.Stderr.WriteString("stawczyk@bastion: Permission denied (publickey).")
		os.Exit(255)
	}
	os.Exit(0)
}

// forwardStdio connects to addr and copies stdin to it and its answers to stdout, as `ssh -W` does.
func forwardStdio(addr string) {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(255)
	}
	go func() { _, _ = io.Copy(conn, os.Stdin) }()
	_, _ = io.Copy(os.Stdout, conn)
}

// FakeCLI makes proc.Command start the test binary as the fake CLI in the given mode and records every call
// (executable name first). The calling package must declare TestCLIHelperProcess (see CLIHelperProcess).
func FakeCLI(t *testing.T, mode string) *[][]string {
	t.Helper()
	var calls [][]string
	old := proc.Command
	proc.Command = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		calls = append(calls, append([]string{name}, arg...))
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestCLIHelperProcess", "--", name}, arg...)...)
		cmd.Env = append(os.Environ(), "WORKCTL_TEST_CLI="+mode)
		return cmd
	}
	t.Cleanup(func() { proc.Command = old })
	return &calls
}

// ChildHelperProcess is the body of the fake module child process. Each test package that uses FakeChildProcess
// declares
//
//	func TestHelperProcess(t *testing.T) { testutil.ChildHelperProcess() }
//
// It echoes the arguments it received and fails when one of them is "fail".
func ChildHelperProcess() {
	if os.Getenv("WORKCTL_TEST_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	fmt.Fprintf(os.Stdout, "ARGS:%s", strings.Join(args, "|"))
	for _, a := range args {
		if a == "fail" {
			fmt.Fprint(os.Stderr, "boom: no token")
			os.Exit(3)
		}
	}
	os.Exit(0)
}

// FakeChildProcess makes module child processes start the test binary as a helper instead of workctl, and records
// the calls. The calling package must declare TestHelperProcess (see ChildHelperProcess).
func FakeChildProcess(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := proc.Command
	proc.Command = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		calls = append(calls, arg)
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, arg...)...)
		cmd.Env = append(os.Environ(), "WORKCTL_TEST_HELPER=1")
		return cmd
	}
	t.Cleanup(func() { proc.Command = old })
	return &calls
}

// Request is one request seen by RecordingServer.
type Request struct {
	Method string
	Path   string // escaped path, so %2F in a GitLab project path stays visible
	Query  string
	Body   string
}

// RecordingServer starts a test server that records every request and answers with the given status and body.
// It returns the server URL and the recorded requests.
func RecordingServer(t *testing.T, status int, body string) (string, *[]Request) {
	t.Helper()
	var seen []Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		seen = append(seen, Request{Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery, Body: string(data)})
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &seen
}

// RepoRoot walks up from the working directory to the directory that holds go.mod, for tests that read
// checked-in files such as config.example.json or the plugin package.
func RepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test working directory")
		}
		dir = parent
	}
}
