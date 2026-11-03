// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

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
}

// UnsupportedCapabilityError means that a provider intentionally does not implement an operation.
type UnsupportedCapabilityError struct {
	Provider   string
	Capability string
}

func (e UnsupportedCapabilityError) Error() string {
	return e.Provider + " does not support " + e.Capability
}
