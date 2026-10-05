package main

import "testing"

func TestParseGitRemote(t *testing.T) {
	for _, input := range []string{
		"https://github.example.com/owner/repository.git",
		"ssh://git@gitlab.example.net/group/project.git",
		"git@dev.azure.example:organization/project/_git/repository.git",
	} {
		remote, err := parseGitRemote(input)
		if err != nil {
			t.Fatalf("parseGitRemote(%q): %v", input, err)
		}
		if remote.Host == "" || remote.Path == "" || remote.Path[len(remote.Path)-4:] == ".git" {
			t.Fatalf("remote = %#v", remote)
		}
	}
}

func TestParseGitRemoteRejectsUnsafePath(t *testing.T) {
	if _, err := parseGitRemote("https://github.example.com/owner/../repository.git"); err == nil {
		t.Fatal("expected unsafe remote path to fail")
	}
}
