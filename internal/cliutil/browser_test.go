// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cliutil

import (
	"context"
	"os/exec"
	"reflect"
	"testing"

	"workctl/internal/proc"
)

func TestOpenURLRefusesNonHTTP(t *testing.T) {
	for _, target := range []string{"file:///etc/passwd", "javascript:alert(1)", "ms-msdt:/x", "/relative", "https://"} {
		if err := OpenURL(target); err == nil {
			t.Errorf("OpenURL(%q) succeeded, want refusal", target)
		}
	}
}

func TestOpenURLStartsBrowserThroughProc(t *testing.T) {
	var gotName string
	var gotArgs []string
	orig := proc.Command
	t.Cleanup(func() { proc.Command = orig })
	proc.Command = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		gotName, gotArgs = name, args
		return exec.CommandContext(ctx, "true")
	}
	if err := OpenURL("https://example.com/login?x=1"); err != nil {
		t.Fatal(err)
	}
	if gotName == "" || len(gotArgs) == 0 || gotArgs[len(gotArgs)-1] != "https://example.com/login?x=1" {
		t.Fatalf("browser started as %q %v", gotName, gotArgs)
	}
}

func TestOpenArgsPerOS(t *testing.T) {
	if got := openArgs("windows", "https://a"); !reflect.DeepEqual(got, []string{"url.dll,FileProtocolHandler", "https://a"}) || openCommand("windows") != "rundll32" {
		t.Errorf("windows: %q %v", openCommand("windows"), got)
	}
	if openCommand("darwin") != "open" || openCommand("linux") != "xdg-open" {
		t.Error("wrong opener")
	}
}
