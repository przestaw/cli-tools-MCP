// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"testing/fstest"

	"workctl/internal/testutil"
)

func fakeSource() fstest.MapFS {
	return fstest.MapFS{
		"workctl/SKILL.md":        {Data: []byte("main")},
		"workctl/extra/notes.md":  {Data: []byte("notes")},
		"compact-output/SKILL.md": {Data: []byte("compact")},
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStatusStates(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dir string)
		want  State
	}{
		{"missing", func(string) {}, Missing},
		{"current", func(d string) {
			write(t, filepath.Join(d, "workctl/SKILL.md"), "main")
			write(t, filepath.Join(d, "workctl/extra/notes.md"), "notes")
		}, Current},
		{"changed content", func(d string) {
			write(t, filepath.Join(d, "workctl/SKILL.md"), "old")
			write(t, filepath.Join(d, "workctl/extra/notes.md"), "notes")
		}, Differs},
		{"absent file", func(d string) { write(t, filepath.Join(d, "workctl/SKILL.md"), "main") }, Differs},
		{"extra file is not drift", func(d string) {
			write(t, filepath.Join(d, "workctl/SKILL.md"), "main")
			write(t, filepath.Join(d, "workctl/extra/notes.md"), "notes")
			write(t, filepath.Join(d, "workctl/mine.md"), "mine")
		}, Current},
	}
	for _, tc := range tests {
		dir := t.TempDir()
		tc.setup(dir)
		got, err := Status(fakeSource(), Target{Name: "x", Dir: dir})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || got[0].Skill != "compact-output" || got[1].Skill != "workctl" {
			t.Fatalf("%s: entries %+v", tc.name, got)
		}
		if got[1].State != tc.want || got[0].State != Missing {
			t.Errorf("%s: workctl %s, compact-output %s; want %s, missing", tc.name, got[1].State, got[0].State, tc.want)
		}
	}
}

func TestInstall(t *testing.T) {
	src := fakeSource()
	dir := t.TempDir()
	target := Target{Name: "x", Dir: dir}
	write(t, filepath.Join(dir, "other/SKILL.md"), "keep")
	write(t, filepath.Join(dir, "workctl/mine.md"), "keep too")

	if files, err := Install(src, target, []string{"workctl"}, true); err != nil || len(files) != 2 {
		t.Fatalf("dry run: %v %v", files, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "workctl/SKILL.md")); err == nil {
		t.Fatal("dry run wrote a file")
	}

	if files, err := Install(src, target, []string{"workctl"}, false); err != nil || len(files) != 2 {
		t.Fatalf("install: %v %v", files, err)
	}
	if got := read(t, filepath.Join(dir, "workctl/extra/notes.md")); got != "notes" {
		t.Errorf("notes = %q", got)
	}
	if files, err := Install(src, target, []string{"workctl"}, false); err != nil || len(files) != 0 {
		t.Errorf("second run wrote %v (%v)", files, err)
	}
	for _, keep := range []string{"other/SKILL.md", "workctl/mine.md"} {
		if _, err := os.Stat(filepath.Join(dir, keep)); err != nil {
			t.Errorf("%s was removed", keep)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "compact-output")); err == nil {
		t.Error("compact-output installed without being asked")
	}

	write(t, filepath.Join(dir, "workctl/SKILL.md"), "edited")
	if _, err := Install(src, target, []string{"workctl"}, false); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(dir, "workctl/SKILL.md")); got != "main" {
		t.Errorf("not refreshed: %q", got)
	}

	// No temporary files are left and nothing is world-writable. Windows reports 0666 for every file, so the
	// permission bits say nothing there.
	err := filepath.WalkDir(filepath.Join(dir, "workctl"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if info, _ := d.Info(); runtime.GOOS != "windows" && info.Mode().Perm()&0o002 != 0 {
			t.Errorf("%s is world-writable", p)
		}
		if filepath.Base(p)[0] == '.' {
			t.Errorf("temporary file left: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSelect(t *testing.T) {
	src := fakeSource()
	tests := []struct {
		extra   []string
		want    []string
		wantErr bool
	}{
		{nil, []string{"workctl"}, false},
		{[]string{"compact-output"}, []string{"workctl", "compact-output"}, false},
		{[]string{"workctl", "compact-output", "compact-output"}, []string{"workctl", "compact-output"}, false},
		{[]string{"nope"}, nil, true},
	}
	for _, tc := range tests {
		got, err := Select(src, tc.extra)
		if (err != nil) != tc.wantErr || !slices.Equal(got, tc.want) {
			t.Errorf("Select(%v) = %v, %v", tc.extra, got, err)
		}
	}
}

func TestTargets(t *testing.T) {
	home, codex := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	got, err := Targets([]string{"all"})
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	want := map[string]string{
		"vscode": filepath.Join(home, ".copilot", "skills"),
		"claude": filepath.Join(home, ".claude", "skills"),
		"codex":  filepath.Join(home, ".codex", "skills"),
	}
	for _, tg := range got {
		if tg.Dir != want[tg.Name] {
			t.Errorf("%s: %s, want %s", tg.Name, tg.Dir, want[tg.Name])
		}
	}
	t.Setenv("CODEX_HOME", codex)
	got, _ = Targets([]string{"codex", "codex"})
	if len(got) != 1 || got[0].Dir != filepath.Join(codex, "skills") {
		t.Errorf("CODEX_HOME ignored: %v", got)
	}
	if _, err := Targets([]string{"emacs"}); err == nil {
		t.Error("unknown target accepted")
	}
}

func TestClaudePluginInstalled(t *testing.T) {
	dir := t.TempDir()
	if ClaudePluginInstalled(dir) {
		t.Fatal("reported without a file")
	}
	file := filepath.Join(dir, "plugins/installed_plugins.json")
	write(t, file, `{"version":2,"plugins":{"other@market":[{}]}}`)
	if ClaudePluginInstalled(dir) {
		t.Error("other plugin counted")
	}
	write(t, file, `{"version":2,"plugins":{"workctl@workctl":[{}]}}`)
	if !ClaudePluginInstalled(dir) {
		t.Error("workctl plugin not detected")
	}
}

// The embedded tree is the plugin directory: a new skill is embedded without further work, and it is the same
// bytes as on disk.
func TestEmbeddedMatchesTheRepository(t *testing.T) {
	root := testutil.RepoRoot(t)
	disk := filepath.Join(root, "plugins/workctl/skills")
	entries, err := os.ReadDir(disk)
	if err != nil {
		t.Fatal(err)
	}
	var onDisk []string
	for _, e := range entries {
		if e.IsDir() {
			onDisk = append(onDisk, e.Name())
		}
	}
	embedded, err := Names(Embedded())
	if err != nil || !slices.Equal(onDisk, embedded) {
		t.Fatalf("embedded %v (%v), on disk %v", embedded, err, onDisk)
	}
	for _, n := range embedded {
		diff, err := stale(Embedded(), n, filepath.Join(disk, n))
		if err != nil || len(diff) > 0 {
			t.Errorf("%s differs from the files on disk: %v %v", n, diff, err)
		}
	}
}
