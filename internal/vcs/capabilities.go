package vcs

import "context"

// PullRequestReader is implemented by providers that can list and read pull requests.
type PullRequestReader interface {
	ListPullRequests(context.Context, ListOptions) (Result, error)
	GetPullRequest(context.Context, ListOptions, int) (PullRequest, error)
}

// ListOptions are provider-neutral filters for a pull request listing.
type ListOptions struct {
	Repository string
	Project    string
	State      string
	Search     string
	Limit      int
}

// PullRequestWriter is implemented only by providers that can change pull requests.
type PullRequestWriter interface {
	CreatePullRequest(context.Context, CreateOptions) (PullRequest, error)
}

// CreateOptions holds the common data needed to open a pull request.
type CreateOptions struct {
	Repository   string
	SourceBranch string
	TargetBranch string
	Title        string
	Description  string
	Draft        bool
}

// UnsupportedCapabilityError means that a provider intentionally does not implement an operation.
type UnsupportedCapabilityError struct {
	Provider   string
	Capability string
}

func (e UnsupportedCapabilityError) Error() string {
	return e.Provider + " does not support " + e.Capability
}
