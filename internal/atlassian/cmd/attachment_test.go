// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveDownloadNeverFollowsOrReplacesTheServerName(t *testing.T) {
	t.Chdir(t.TempDir())
	outside := filepath.Join("..", "x")

	for name, want := range map[string]string{"report.pdf": "report.pdf", outside: "x", "/etc/cron.d/y": "y"} {
		got, err := saveDownload("", name, []byte("new"))
		if err != nil || got != want {
			t.Errorf("saveDownload(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(outside); err == nil {
		t.Error("the file was written outside the current directory")
	}

	// A name the server chose never replaces an existing file; -o does.
	if err := os.WriteFile("keep.txt", []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := saveDownload("", "keep.txt", []byte("new")); err == nil {
		t.Error("an existing file was replaced")
	}
	if b, _ := os.ReadFile("keep.txt"); string(b) != "old" {
		t.Errorf("keep.txt = %q", b)
	}
	if _, err := saveDownload("keep.txt", "ignored", []byte("new")); err != nil {
		t.Errorf("-o: %v", err)
	}

	for _, name := range []string{"", ".", "..", "a/.."} {
		if got, err := saveDownload("", name, []byte("x")); err == nil {
			t.Errorf("saveDownload(%q) = %q, want an error", name, got)
		}
	}
}
