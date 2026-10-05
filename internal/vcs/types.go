// Package vcs contains the provider-neutral parts of pull request output.
package vcs

import "time"

// PullRequest is the common subset shared by Azure DevOps pull requests, GitLab merge requests and GitHub pull requests.
type PullRequest struct {
	Provider     string         `json:"provider"`
	Repository   string         `json:"repository"`
	ID           int            `json:"id"`
	Title        string         `json:"title"`
	State        string         `json:"state"`
	Draft        bool           `json:"draft"`
	Author       string         `json:"author,omitempty"`
	SourceBranch string         `json:"source_branch,omitempty"`
	TargetBranch string         `json:"target_branch,omitempty"`
	URL          string         `json:"url,omitempty"`
	CreatedAt    *time.Time     `json:"created_at,omitempty"`
	UpdatedAt    *time.Time     `json:"updated_at,omitempty"`
	Details      map[string]any `json:"details,omitempty"`
}

// Result holds a normalized response plus provider-specific details that have no shared equivalent.
type Result struct {
	Items    []PullRequest `json:"items"`
	Warnings []string      `json:"warnings,omitempty"`
	Errors   []string      `json:"errors,omitempty"`
}
