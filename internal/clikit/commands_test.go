package clikit

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestResolveAndLeaves(t *testing.T) {
	root := Root("test", "test", "")
	group := &cobra.Command{Use: "repo", Aliases: []string{"r"}}
	group.AddCommand(&cobra.Command{Use: "show", Aliases: []string{"s"}, Run: func(*cobra.Command, []string) {}})
	root.AddCommand(group)
	root.PersistentFlags().Bool("json", false, "")
	path, isGroup, rest, err := Resolve(root, []string{"--json", "r", "s", "123"})
	if err != nil || isGroup || !reflect.DeepEqual(path, []string{"repo", "show"}) || !reflect.DeepEqual(rest, []string{"--json", "123"}) {
		t.Fatalf("path=%v group=%v rest=%v err=%v", path, isGroup, rest, err)
	}
	_, isGroup, _, err = Resolve(root, []string{"repo"})
	if err != nil || !isGroup {
		t.Fatalf("group=%v err=%v", isGroup, err)
	}
	found := false
	for _, leaf := range Leaves(root) {
		if reflect.DeepEqual(leaf, []string{"repo", "show"}) {
			found = true
		}
	}
	if !found {
		t.Fatal("runnable command missing")
	}
}

func TestVersionReadsCurrentValue(t *testing.T) {
	version := "old"
	root := Root("test", "", "")
	root.AddCommand(Version("test", &version))
	var buf bytes.Buffer
	root.SetOut(&buf)
	version = "new"
	if err := Run(root, []string{"version"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "test new\n" {
		t.Fatal(buf.String())
	}
}

func TestRaw(t *testing.T) {
	for _, tc := range []struct {
		path, want string
		absolute   bool
	}{
		{"endpoint", "/endpoint", false},
		{"/endpoint", "/endpoint", false},
		{"https://example.test/path", "https://example.test/path", true},
	} {
		called, printed := false, false
		cmd := Raw("raw <path>", "", tc.absolute, func(ctx context.Context, method, path string, body any) (json.RawMessage, error) {
			called = true
			if _, ok := ctx.Deadline(); !ok {
				t.Error("no timeout")
			}
			if path != tc.want || method != "POST" || !reflect.DeepEqual(body, map[string]any{"a": float64(1)}) {
				t.Errorf("%s %s %v", method, path, body)
			}
			return json.RawMessage(`{"ok":true}`), nil
		}, func(data []byte) error { printed = string(data) == `{"ok":true}`; return nil })
		if err := Run(cmd, []string{tc.path, "-X", "post", "-d", `{"a":1}`}); err != nil {
			t.Fatal(err)
		}
		if !called || !printed {
			t.Fatal("raw not executed/printed")
		}
	}
}

func TestRawRejectsInvalidInput(t *testing.T) {
	for _, args := range [][]string{{}, {"path", "--data", "{"}} {
		called := false
		cmd := Raw("raw <path>", "", false, func(context.Context, string, string, any) (json.RawMessage, error) { called = true; return nil, nil }, nil)
		cmd.SilenceErrors, cmd.SilenceUsage = true, true
		err := Run(cmd, args)
		if err == nil || called {
			t.Fatalf("args=%v err=%v called=%v", args, err, called)
		}
		if len(args) > 0 && !strings.Contains(err.Error(), "invalid --data JSON") {
			t.Fatal(err)
		}
	}
}
