package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func cmdWords(s string) []string { return strings.Fields(s) }

// The read lists are hand-written; this fails when a re-sync renames or removes a command they mention.
func TestReadAllowlistsMatchTheCommandTrees(t *testing.T) {
	for name, m := range cliModules {
		for key := range m.read {
			path, group, _, err := m.resolve(cmdWords(key))
			if err != nil || group || strings.Join(path, " ") != key {
				t.Errorf("%s: read entry %q resolves to %v (group=%v, err=%v)", name, key, path, group, err)
			}
		}
	}
}

func TestEveryCommandIsClassifiedAndWritesNeedThePolicy(t *testing.T) {
	for name, m := range cliModules {
		writes := 0
		for _, leaf := range m.leaves() {
			key := strings.Join(leaf, " ")
			write, err := m.classify(leaf, true)
			switch {
			case err != nil: // refused on the agent surface
			case write:
				writes++
				if m.read[key] {
					t.Errorf("%s %s is on the read list but classifies as a write", name, key)
				}
			case !m.read[key] && leaf[0] != "completion" && leaf[len(leaf)-1] != "open":
				t.Errorf("%s %s is neither read-listed nor a write", name, key)
			}
		}
		if writes == 0 {
			t.Errorf("%s: no write commands found, the classification is broken", name)
		}
	}
}

func TestKnownWriteCommandsNeedAllowWrite(t *testing.T) {
	cases := map[string][]string{
		"azure": {"pr create", "pr comment", "pr update", "pr vote", "pr complete", "pr abandon", "pr reply", "pr resolve",
			"pr reactivate", "pr reopen", "pr autocomplete", "build queue", "pipeline run", "release create"},
		"atlassian": {"issue create", "issue transition", "issue update", "issue set-field", "issue assign", "issue delete",
			"comment add", "comment delete", "link add", "link delete", "worklog add", "attachment upload",
			"attachment delete", "attachment download", "page create", "page update", "sprint add"},
	}
	for name, keys := range cases {
		m := cliModules[name]
		for _, key := range keys {
			write, err := m.classify(cmdWords(key), true)
			if err != nil || !write {
				t.Errorf("%s %s: write=%v err=%v, want a write", name, key, write, err)
			}
		}
	}
}

func TestAliasesAndFlagsBeforeTheCommand(t *testing.T) {
	ado, atl := cliModules["azure"], cliModules["atlassian"]
	cases := []struct {
		m     *cliModule
		args  []string
		write bool
	}{
		{ado, []string{"-p", "Some Project", "pr", "list"}, false},
		{ado, []string{"-p", "Some Project", "pr", "create", "--title", "t"}, true},
		{ado, []string{"pullrequest", "get", "1"}, false},
		{ado, []string{"pullrequest", "create"}, true},
		{atl, []string{"i", "get", "ABC-1"}, false},
		{atl, []string{"item", "transition", "ABC-1"}, true},
		{atl, []string{"--json", "jql", "project = ABC"}, false},
	}
	for _, c := range cases {
		write, err := c.m.classify(c.args, true)
		if err != nil || write != c.write {
			t.Errorf("%s %q: write=%v err=%v, want write=%v", c.m.name, c.args, write, err, c.write)
		}
	}
}

func TestHelpIsAlwaysRead(t *testing.T) {
	groups := map[string]string{"azure": "pr", "atlassian": "issue"}
	for _, m := range cliModules {
		g := groups[m.name]
		for _, args := range [][]string{nil, {g}, {g, "create", "--help"}, {g, "delete", "-h"}, {"help"}, {"version"}} {
			if write, err := m.classify(args, true); err != nil || write {
				t.Errorf("%s %q: write=%v err=%v, want read", m.name, args, write, err)
			}
		}
	}
}

func TestStrictSurfaceRefusesCredentialsShellAndRedirectingFlags(t *testing.T) {
	ado := cliModules["azure"]
	refused := [][]string{
		{"config", "show"}, {"config", "set-token"}, {"config", "set", "project", "x"}, {"setup"}, {"shell"},
		{"completion", "bash"}, {"pr", "open", "1"}, {"pr", "list", "--config", "x.yaml"}, {"pr", "list", "--config=x.yaml"},
		{"pr", "list", "--insecure"}, {"raw", "https://example.test/x"}, {"raw", "-p", "X", "/a"}, {"raw"},
	}
	for _, args := range refused {
		if _, err := ado.classify(args, true); err == nil {
			t.Errorf("strict: %q should be refused", args)
		}
	}
	// the human CLI may maintain its own configuration and open a browser, but the shell can run anything
	for args, want := range map[string]bool{"config show": false, "config set-token": false, "pr open": false, "shell": true} {
		write, err := ado.classify(cmdWords(args), false)
		if err != nil || write != want {
			t.Errorf("CLI: %q write=%v err=%v, want write=%v", args, write, err, want)
		}
	}
}

func TestRawIsReadOnlyUnlessItWrites(t *testing.T) {
	ado := cliModules["azure"]
	cases := []struct {
		args  string
		write bool
	}{
		{"raw /a/b", false}, {"raw /a/b -X GET", false}, {"raw /a -XHEAD", false},
		{"raw /a -X PUT", true}, {"raw /a -XPOST", true}, {"raw /a --method=delete", true}, {"raw /a --method patch", true},
		{`raw /a -d {}`, true}, {`raw /a --data={}`, true},
	}
	for _, c := range cases {
		write, err := ado.classify(cmdWords(c.args), true)
		if err != nil || write != c.write {
			t.Errorf("%q: write=%v err=%v, want write=%v", c.args, write, err, c.write)
		}
	}
}

// ---- running through a child process (the MCP path) --------------------------------------------------------------

func TestHelperProcess(t *testing.T) {
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

// fakeChildProcess makes runModuleProcess start the test binary as a helper instead of workctl, and records the calls.
func fakeChildProcess(t *testing.T) *[][]string {
	t.Helper()
	var calls [][]string
	old := execCommand
	execCommand = func(ctx context.Context, name string, arg ...string) *exec.Cmd {
		calls = append(calls, arg)
		cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"-test.run=TestHelperProcess", "--"}, arg...)...)
		cmd.Env = append(os.Environ(), "WORKCTL_TEST_HELPER=1")
		return cmd
	}
	t.Cleanup(func() { execCommand = old })
	return &calls
}

func TestCallModuleChecksThePolicyBeforeStartingAnything(t *testing.T) {
	calls := fakeChildProcess(t)
	cfg := defaultConfig()
	cfg.path = "C:/some/workctl.json"

	out, err := callModule(context.Background(), cfg, "azure", []string{"pr", "get", "34438", "-p", "Some Project"})
	if err != nil {
		t.Fatal(err)
	}
	want := "ARGS:--config|C:/some/workctl.json|azure|pr|get|34438|-p|Some Project|--no-color"
	if string(out) != want {
		t.Fatalf("child saw %q, want %q", out, want)
	}

	before := len(*calls)
	if _, err := callModule(context.Background(), cfg, "azure", []string{"pr", "create", "--title", "x"}); err == nil || !strings.Contains(err.Error(), "allow_write") {
		t.Fatalf("a write must be refused while allow_write is off, err = %v", err)
	}
	if _, err := callModule(context.Background(), cfg, "atlassian", []string{"config", "set-token"}); err == nil {
		t.Fatal("credentials commands must be refused")
	}
	if len(*calls) != before {
		t.Fatal("a refused command must not start a process")
	}

	cfg.Policy.AllowWrite = true
	if _, err := callModule(context.Background(), cfg, "atlassian", []string{"comment", "add", "ABC-1", "--body", "hi"}); err != nil {
		t.Fatalf("with allow_write the write must run: %v", err)
	}
	if len(*calls) != before+1 {
		t.Fatal("expected exactly one more process")
	}
	if _, err := callModule(context.Background(), cfg, "atlassian", []string{"config", "set-token"}); err == nil {
		t.Fatal("credentials commands stay refused even with allow_write")
	}
}

func TestCallModuleReportsStderrOfAFailingCommand(t *testing.T) {
	fakeChildProcess(t)
	_, err := callModule(context.Background(), defaultConfig(), "atlassian", []string{"issue", "get", "fail"})
	if err == nil || !strings.Contains(err.Error(), "boom: no token") || !strings.HasPrefix(err.Error(), "atlassian:") {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPToolCallConvertsNumbersAndUsesTheModule(t *testing.T) {
	fakeChildProcess(t)
	out, err := callMCPTool(context.Background(), defaultConfig(), "azure", map[string]any{
		"args": []any{"pr", "get", float64(34438), "--json"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "azure|pr|get|34438|--json|--no-color") {
		t.Fatalf("child saw %q", out)
	}
	if _, err := callMCPTool(context.Background(), defaultConfig(), "azure", map[string]any{"args": []any{"pr", map[string]any{}}}); err == nil {
		t.Fatal("a non-scalar argument must be rejected")
	}
	if _, err := callMCPTool(context.Background(), defaultConfig(), "azure", map[string]any{}); err == nil {
		t.Fatal("empty args must be rejected")
	}
}

func TestModuleToolsDescribeThePolicy(t *testing.T) {
	cfg := defaultConfig()
	for _, tool := range moduleTools(cfg) {
		if !strings.Contains(tool.Description, "refused until policy.allow_write=true") {
			t.Errorf("%s: description = %q", tool.Name, tool.Description)
		}
	}
	cfg.Policy.AllowWrite = true
	for _, tool := range moduleTools(cfg) {
		if !strings.Contains(tool.Description, "allowed (policy.allow_write=true)") {
			t.Errorf("%s: description = %q", tool.Name, tool.Description)
		}
	}
}

func TestArgStrings(t *testing.T) {
	got, err := argStrings(map[string]any{"args": []any{"a", float64(1), 2.5, true}}, "args")
	if err != nil || strings.Join(got, "|") != "a|1|2.5|true" {
		t.Fatalf("got %v, err %v", got, err)
	}
	if _, err := argStrings(map[string]any{"args": "not an array"}, "args"); err == nil {
		t.Fatal("a string is not an array")
	}
	if got, err := argStrings(map[string]any{}, "args"); err != nil || got != nil {
		t.Fatalf("absent args = %v, %v", got, err)
	}
}
