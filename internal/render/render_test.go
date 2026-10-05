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
	if err := PullRequests(&stdout, &stderr, result, FormatText); err != nil {
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
	if err := PullRequests(&stdout, &stderr, vcs.Result{Items: []vcs.PullRequest{}}, FormatJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "\"items\": []") || stderr.Len() != 0 {
		t.Fatalf("output = %q / %q", stdout.String(), stderr.String())
	}
}
