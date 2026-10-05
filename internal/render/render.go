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

// PullRequests writes a pull request result and emits partial-result notices to diagnostics.
func PullRequests(stdout, stderr io.Writer, result vcs.Result, format Format) error {
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
		if _, err := fmt.Fprintf(stdout, "%s #%d %s [%s] %s\n", item.Provider, item.ID, item.Title, item.State, branches); err != nil {
			return err
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
