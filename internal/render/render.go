// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package render writes stable command output for workctl's unified APIs.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"workctl/internal/vcs"
)

// Format is the supported output representation.
type Format string

// The output formats accepted by ParseFormat.
const (
	FormatText Format = "text"
	FormatJSON Format = "json"
)

// ParseFormat validates a command-line format value.
func ParseFormat(value string) (Format, error) {
	switch Format(strings.ToLower(strings.TrimSpace(value))) {
	case "", FormatText:
		return FormatText, nil
	case FormatJSON:
		return FormatJSON, nil
	default:
		return "", fmt.Errorf("unsupported format %q (use text or json)", value)
	}
}

// PullRequests writes a pull request result; detail adds URL and description (pr show). Partial-result notices go to stderr.
func PullRequests(stdout, stderr io.Writer, result vcs.Result, format Format, detail bool) error {
	if format == FormatJSON {
		body, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(stdout, string(body))
		return err
	}
	for _, item := range result.Items {
		branches := item.SourceBranch + " -> " + item.TargetBranch
		if item.SourceBranch == "" && item.TargetBranch == "" {
			branches = ""
		}
		author := ""
		if item.Author != "" {
			author = " by @" + item.Author
		}
		draft := ""
		if item.Draft {
			draft = " [draft]"
		}
		branchPart := ""
		if branches != "" {
			branchPart = " " + branches
		}
		if _, err := fmt.Fprintf(stdout, "%s #%d %s [%s]%s%s%s\n", item.Provider, item.ID, item.Title, item.State, draft, branchPart, author); err != nil {
			return err
		}
		if detail {
			if item.URL != "" {
				if _, err := fmt.Fprintf(stdout, "URL: %s\n", item.URL); err != nil {
					return err
				}
			}
			if desc := strings.TrimSpace(item.Description); desc != "" {
				if _, err := fmt.Fprintf(stdout, "\n%s\n", desc); err != nil {
					return err
				}
			}
		}
	}
	for _, warning := range result.Warnings {
		if _, err := fmt.Fprintf(stderr, "warning: %s\n", warning); err != nil {
			return err
		}
	}
	for _, problem := range result.Errors {
		if _, err := fmt.Fprintf(stderr, "partial result: %s\n", problem); err != nil {
			return err
		}
	}
	return nil
}
