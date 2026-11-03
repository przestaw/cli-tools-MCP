// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package service

import (
	"strings"
	"testing"
)

func TestParseVote(t *testing.T) {
	cases := map[string]int{
		"approve": 10, "approved": 10, "approve-suggestions": 5,
		"reset": 0, "wait": -5, "reject": -10, "10": 10, "-10": -10,
	}
	for in, want := range cases {
		got, err := ParseVote(in)
		if err != nil {
			t.Fatalf("ParseVote(%q) error: %v", in, err)
		}
		if got != want {
			t.Errorf("ParseVote(%q) = %d, want %d", in, got, want)
		}
	}
	if _, err := ParseVote("bogus"); err == nil {
		t.Error("ParseVote(bogus) should error")
	}
}

func TestNormalizeBranch(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"main":            "refs/heads/main",
		"refs/heads/main": "refs/heads/main",
		"refs/tags/v1":    "refs/tags/v1",
		"feature/x":       "refs/heads/feature/x",
	}
	for in, want := range cases {
		if got := NormalizeBranch(in); got != want {
			t.Errorf("NormalizeBranch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShortRef(t *testing.T) {
	if got := ShortRef("refs/heads/main"); got != "main" {
		t.Errorf("ShortRef = %q, want main", got)
	}
	if got := ShortRef("refs/tags/v1"); got != "tags/v1" {
		t.Errorf("ShortRef = %q, want tags/v1", got)
	}
}

func TestVoteName(t *testing.T) {
	if VoteName(10) != "approved" || VoteName(-10) != "rejected" || VoteName(0) != "no vote" {
		t.Errorf("VoteName mismatch")
	}
}

func TestMergeCommitMessage(t *testing.T) {
	if got := MergeCommitMessage(7, "Title", ""); got != "Merged PR 7: Title" {
		t.Errorf("without description = %q", got)
	}
	if got := MergeCommitMessage(7, "Title", "Body"); got != "Merged PR 7: Title\n\nBody" {
		t.Errorf("with description = %q", got)
	}
	exact := strings.Repeat("ä", MaxMergeDescription)
	if got := MergeCommitMessage(7, "Title", exact); got != "Merged PR 7: Title\n\n"+exact {
		t.Errorf("description at the limit must not be cut")
	}
	if got := MergeCommitMessage(7, "Title", exact+"more"); got != "Merged PR 7: Title\n\n"+exact+"..." {
		t.Errorf("long description = ...%q", got[len(got)-10:])
	}
}
