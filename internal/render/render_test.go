package render

import (
	"bytes"
	"strings"
	"testing"

	"workctl/internal/vcs"
)

func TestPullRequestsWritesTextAndDiagnosticsSeparately(t *testing.T) {
	var stdout, stderr bytes.Buffer
	result := vcs.Result{
		Items:    []vcs.PullRequest{{Provider: "github", ID: 4, Title: "Fix", State: "open", SourceBranch: "fix", TargetBranch: "main"}},
		Warnings: []string{"limited"},
	}
	if err := PullRequests(&stdout, &stderr, result, FormatText, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "github #4 Fix [open] fix -> main") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stdout.String() != "github #4 Fix [open] fix -> main\n" || !strings.Contains(stderr.String(), "warning: limited") {
		t.Fatalf("unexpected output: %q / %q", stdout.String(), stderr.String())
	}
}

func TestPullRequestsWritesStableJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := PullRequests(&stdout, &stderr, vcs.Result{Items: []vcs.PullRequest{}}, FormatJSON, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "\"items\": []") || stderr.Len() != 0 {
		t.Fatalf("output = %q / %q", stdout.String(), stderr.String())
	}
}

func TestPullRequestsSingleItemDetailedText(t *testing.T) {
	var stdout, stderr bytes.Buffer
	result := vcs.Result{
		Items: []vcs.PullRequest{{
			Provider:     "github",
			ID:           42,
			Title:        "Implement unified PR",
			Description:  "This PR implements the unified VCS layer.\nLine 2",
			State:        "open",
			Draft:        true,
			Author:       "dev",
			SourceBranch: "feat/pr",
			TargetBranch: "main",
			URL:          "https://github.example/pull/42",
		}},
	}
	if err := PullRequests(&stdout, &stderr, result, FormatText, true); err != nil {
		t.Fatal(err)
	}
	wantHead := "github #42 Implement unified PR [open] [draft] feat/pr -> main by @dev\nURL: https://github.example/pull/42\n\nThis PR implements the unified VCS layer.\nLine 2\n"
	if stdout.String() != wantHead {
		t.Fatalf("got:\n%q\nwant:\n%q", stdout.String(), wantHead)
	}
	stdout.Reset()
	if err := PullRequests(&stdout, &stderr, result, FormatText, false); err != nil {
		t.Fatal(err)
	}
	if strings.Count(stdout.String(), "\n") != 1 {
		t.Fatalf("a one-item list must stay one line: %q", stdout.String())
	}
}
