// Package service wires configuration to the API client and provides
// higher-level helpers (field resolution, payload building, JQL assembly)
// shared by the cobra commands and the interactive REPL.
package service

import (
	"fmt"
	"strings"

	"workctl/internal/atlassian/client"
	"workctl/internal/atlassian/config"
)

// Service holds a configured API client plus the resolved profile name.
type Service struct {
	Client      *client.Client
	ProfileName string
	BaseURL     string
}

// Overrides are command-line overrides applied on top of the active profile.
type Overrides struct {
	Profile  string
	Insecure bool // force-skip TLS verify
}

// New builds a Service from config + overrides, resolving the API token.
func New(cfg *config.Config, ov Overrides) (*Service, error) {
	prof, name, err := cfg.Active(ov.Profile)
	if err != nil {
		return nil, err
	}
	if prof.URL == "" {
		return nil, fmt.Errorf("profile %q has no url (run `atlassian config init`)", name)
	}
	tok, err := config.ResolveToken(name, prof.Email)
	if err != nil {
		return nil, err
	}
	verifyTLS := prof.VerifyTLS
	if ov.Insecure {
		verifyTLS = false
	}
	c := client.New(client.Options{
		URL:       prof.URL,
		Email:     prof.Email,
		Token:     tok,
		Timeout:   prof.TimeoutDuration(),
		VerifyTLS: verifyTLS,
	})
	return &Service{Client: c, ProfileName: name, BaseURL: c.BaseURL()}, nil
}

// FieldSpec is one parsed --field key=value pair.
type FieldSpec struct {
	Key   string
	Value string
}

// ParseFieldArgs parses ["Story Points=5","labels=a,b"] into FieldSpecs.
func ParseFieldArgs(args []string) ([]FieldSpec, error) {
	var out []FieldSpec
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok {
			return nil, fmt.Errorf("invalid --field %q (want key=value)", a)
		}
		out = append(out, FieldSpec{Key: strings.TrimSpace(k), Value: v})
	}
	return out, nil
}

// JQLBuilder assembles a JQL string from simple constraints.
type JQLBuilder struct {
	parts []string
}

// Project adds a project filter.
func (b *JQLBuilder) Project(key string) *JQLBuilder {
	if key != "" {
		b.parts = append(b.parts, fmt.Sprintf("project = %s", quoteJQL(key)))
	}
	return b
}

// Status adds a status equality filter.
func (b *JQLBuilder) Status(s string) *JQLBuilder {
	if s != "" {
		b.parts = append(b.parts, fmt.Sprintf("status = %s", quoteJQL(s)))
	}
	return b
}

// Type adds an issue type filter.
func (b *JQLBuilder) Type(s string) *JQLBuilder {
	if s != "" {
		b.parts = append(b.parts, fmt.Sprintf("issuetype = %s", quoteJQL(s)))
	}
	return b
}

// Assignee adds an assignee filter ("me" maps to currentUser()).
func (b *JQLBuilder) Assignee(s string) *JQLBuilder {
	if s == "me" {
		b.parts = append(b.parts, "assignee = currentUser()")
	} else if s != "" {
		b.parts = append(b.parts, fmt.Sprintf("assignee = %s", quoteJQL(s)))
	}
	return b
}

// Key adds an exact issue-key constraint. Unlike Text, this matches the issue
// key itself (e.g. AB-1557); JQL `text ~` only searches summary/description.
func (b *JQLBuilder) Key(s string) *JQLBuilder {
	if s != "" {
		b.parts = append(b.parts, fmt.Sprintf("key = %s", quoteJQL(s)))
	}
	return b
}

// Text adds a full-text constraint (summary/description match).
func (b *JQLBuilder) Text(s string) *JQLBuilder {
	if s != "" {
		b.parts = append(b.parts, fmt.Sprintf("text ~ %s", quoteJQL(s)))
	}
	return b
}

// Label adds a label filter.
func (b *JQLBuilder) Label(s string) *JQLBuilder {
	if s != "" {
		b.parts = append(b.parts, fmt.Sprintf("labels = %s", quoteJQL(s)))
	}
	return b
}

// String returns the assembled JQL (parts AND-joined, newest first). Empty if
// no constraints.
func (b *JQLBuilder) String() string {
	if len(b.parts) == 0 {
		return ""
	}
	return strings.Join(b.parts, " AND ") + " ORDER BY updated DESC"
}

// quoteJQL quotes a JQL value unless it is a bare safe token.
func quoteJQL(s string) string {
	safe := s != ""
	for _, r := range s {
		isSafe := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !isSafe {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
