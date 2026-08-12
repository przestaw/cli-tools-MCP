// Package service wires configuration to the API client and provides
// higher-level helpers (project resolution, identity lookup, ref/vote parsing)
// shared by the cobra commands and the interactive REPL.
package service

import (
	"context"
	"fmt"
	"strings"

	"workctl/internal/azure/client"
	"workctl/internal/azure/config"
)

// Service holds a configured API client plus resolved profile defaults.
type Service struct {
	Client         *client.Client
	ProfileName    string
	OrgURL         string
	DefaultProject string
}

// Overrides are command-line overrides applied on top of the active profile.
type Overrides struct {
	Profile  string
	Project  string // overrides the profile default project
	Insecure bool   // force-skip TLS verify
}

// New builds a Service from config + overrides, resolving the PAT.
func New(cfg *config.Config, ov Overrides) (*Service, error) {
	prof, name, err := cfg.Active(ov.Profile)
	if err != nil {
		return nil, err
	}
	if prof.OrgURL == "" {
		return nil, fmt.Errorf("profile %q has no org_url (run `azure config init`)", name)
	}
	tok, err := config.ResolveToken(name, prof.OrgURL)
	if err != nil {
		return nil, err
	}
	verifyTLS := prof.VerifyTLS
	if ov.Insecure {
		verifyTLS = false
	}
	c := client.New(client.Options{
		OrgURL:    prof.OrgURL,
		Token:     tok,
		Timeout:   prof.TimeoutDuration(),
		VerifyTLS: verifyTLS,
	})
	project := prof.Project
	if ov.Project != "" {
		project = ov.Project
	}
	return &Service{
		Client:         c,
		ProfileName:    name,
		OrgURL:         c.OrgURL(),
		DefaultProject: project,
	}, nil
}

// Project returns the project to use, preferring an explicit override over the
// resolved default. Errors if neither is set.
func (s *Service) Project(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if s.DefaultProject != "" {
		return s.DefaultProject, nil
	}
	return "", fmt.Errorf("no project set (pass --project or set a default with `azure config set project <name>`)")
}

// CurrentUserID returns the authenticated user's identity id (for PR votes).
func (s *Service) CurrentUserID(ctx context.Context) (string, error) {
	cd, err := s.Client.ConnectionData(ctx)
	if err != nil {
		return "", err
	}
	if cd.AuthenticatedUser.ID == "" {
		return "", fmt.Errorf("could not resolve the authenticated user")
	}
	return cd.AuthenticatedUser.ID, nil
}

// NormalizeBranch ensures a branch name is a full ref (refs/heads/<name>).
// A name already starting with refs/ is returned unchanged.
func NormalizeBranch(name string) string {
	if name == "" {
		return ""
	}
	if strings.HasPrefix(name, "refs/") {
		return name
	}
	return "refs/heads/" + name
}

// ShortRef trims the refs/heads/ prefix for display.
func ShortRef(ref string) string {
	ref = strings.TrimPrefix(ref, "refs/heads/")
	return strings.TrimPrefix(ref, "refs/")
}

// votes maps vote names to Azure DevOps vote codes.
var votes = map[string]int{
	"approve":             10,
	"approved":            10,
	"approve-suggestions": 5,
	"suggestions":         5,
	"reset":               0,
	"none":                0,
	"wait":                -5,
	"waiting":             -5,
	"reject":              -10,
	"rejected":            -10,
}

// ParseVote maps a vote name (or a raw integer string) to a vote code.
func ParseVote(s string) (int, error) {
	if v, ok := votes[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v, nil
	}
	switch strings.TrimSpace(s) {
	case "10":
		return 10, nil
	case "5":
		return 5, nil
	case "0":
		return 0, nil
	case "-5":
		return -5, nil
	case "-10":
		return -10, nil
	}
	return 0, fmt.Errorf("unknown vote %q (use approve|approve-suggestions|reset|wait|reject)", s)
}

// VoteName renders a vote code as a short word.
func VoteName(v int) string {
	switch v {
	case 10:
		return "approved"
	case 5:
		return "approved-with-suggestions"
	case 0:
		return "no vote"
	case -5:
		return "waiting"
	case -10:
		return "rejected"
	default:
		return fmt.Sprintf("%d", v)
	}
}

// MaxMergeDescription is where the Azure DevOps web dialog cuts the description
// it appends to the merge commit message, adding "..." after the cut
// (measured on browser-completed PRs: bodies of 3000 chars + "...").
const MaxMergeDescription = 3000

// MergeCommitMessage builds the merge commit message the Azure DevOps web
// dialog prefills when a PR is completed: "Merged PR <id>: <title>", a blank
// line and the description. The server never derives it itself - an API
// completion without completionOptions.mergeCommitMessage gets the bare
// "Merge pull request <id> from <src> into <dst>" and loses the description.
func MergeCommitMessage(id int, title, description string) string {
	msg := fmt.Sprintf("Merged PR %d: %s", id, title)
	if description == "" {
		return msg
	}
	if r := []rune(description); len(r) > MaxMergeDescription {
		description = string(r[:MaxMergeDescription]) + "..."
	}
	return msg + "\n\n" + description
}
