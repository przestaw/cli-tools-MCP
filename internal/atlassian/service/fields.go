package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"workctl/internal/atlassian/client"
)

// BuildEditFields converts --field key=value specs into a Jira "fields" update
// payload using the issue's editmeta (schema + allowed values). Keys match the
// field id ("customfield_10010", "summary") or display name (case-insensitive).
func (s *Service) BuildEditFields(ctx context.Context, issueKey string, specs []FieldSpec) (map[string]any, error) {
	meta, err := s.Client.EditMeta(ctx, issueKey)
	if err != nil {
		return nil, err
	}
	metas := make([]client.FieldMeta, 0, len(meta))
	for id, m := range meta {
		if m.Key == "" {
			m.Key = id
		}
		metas = append(metas, m)
	}
	return s.buildFields(ctx, metas, specs)
}

// BuildCreateFields converts --field specs into a "fields" payload using the
// project + issue type createmeta.
func (s *Service) BuildCreateFields(ctx context.Context, projectKey, issueTypeID string, specs []FieldSpec) (map[string]any, error) {
	metas, err := s.Client.CreateMetaFields(ctx, projectKey, issueTypeID)
	if err != nil {
		return nil, err
	}
	return s.buildFields(ctx, metas, specs)
}

// buildFields resolves each spec against the field metas and coerces values.
func (s *Service) buildFields(ctx context.Context, metas []client.FieldMeta, specs []FieldSpec) (map[string]any, error) {
	out := map[string]any{}
	var names []string
	for _, m := range metas {
		names = append(names, m.Name)
	}
	for _, sp := range specs {
		var m *client.FieldMeta
		for i := range metas {
			if strings.EqualFold(metas[i].Name, sp.Key) || metas[i].IDKey() == sp.Key {
				m = &metas[i]
				break
			}
		}
		if m == nil {
			return nil, fmt.Errorf("field %q not found (available: %s)", sp.Key, strings.Join(names, ", "))
		}
		v, err := s.CoerceFieldValue(ctx, m, sp.Value)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", m.Name, err)
		}
		out[m.IDKey()] = v
	}
	return out, nil
}

// CoerceFieldValue converts a string into the JSON value the field's schema
// expects. Unknown shapes fall back to JSON parsing, then to the raw string.
func (s *Service) CoerceFieldValue(ctx context.Context, m *client.FieldMeta, value string) (any, error) {
	sc := m.Schema
	if sc == nil {
		return value, nil
	}
	switch sc.Type {
	case "string":
		// v3 rich-text fields (description, environment, custom textareas) take ADF.
		if sc.System == "description" || sc.System == "environment" ||
			strings.HasSuffix(sc.Custom, ":textarea") {
			return client.TextToADF(value), nil
		}
		return value, nil
	case "number":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("not a number: %q", value)
		}
		return f, nil
	case "date", "datetime":
		return value, nil
	case "option":
		return s.matchAllowed(m, value, "value")
	case "priority", "resolution":
		return map[string]any{"name": value}, nil
	case "issuetype":
		return map[string]any{"name": value}, nil
	case "project":
		return map[string]any{"key": value}, nil
	case "issuelink":
		return map[string]any{"key": value}, nil
	case "user":
		acc, err := s.ResolveAccountID(ctx, value)
		if err != nil {
			return nil, err
		}
		return map[string]any{"accountId": acc}, nil
	case "team":
		return value, nil
	case "array":
		parts := splitList(value)
		switch sc.Items {
		case "string":
			return parts, nil
		case "option":
			var arr []any
			for _, p := range parts {
				v, err := s.matchAllowed(m, p, "value")
				if err != nil {
					return nil, err
				}
				arr = append(arr, v)
			}
			return arr, nil
		case "component", "version":
			var arr []any
			for _, p := range parts {
				arr = append(arr, map[string]any{"name": p})
			}
			return arr, nil
		case "user":
			var arr []any
			for _, p := range parts {
				acc, err := s.ResolveAccountID(ctx, p)
				if err != nil {
					return nil, err
				}
				arr = append(arr, map[string]any{"accountId": acc})
			}
			return arr, nil
		}
	}
	// Last resort: accept raw JSON, else the bare string.
	var v any
	if err := json.Unmarshal([]byte(value), &v); err == nil {
		return v, nil
	}
	return value, nil
}

// matchAllowed matches value against the meta's allowedValues by the given
// attribute (e.g. "value" for options), case-insensitively. Returns
// {attr: <canonical>} and errors with the option list on no match.
func (s *Service) matchAllowed(m *client.FieldMeta, value, attr string) (any, error) {
	if len(m.AllowedValues) == 0 {
		return map[string]any{attr: value}, nil
	}
	var allowed []map[string]any
	if err := json.Unmarshal(m.AllowedValues, &allowed); err != nil {
		return map[string]any{attr: value}, nil
	}
	var opts []string
	for _, av := range allowed {
		for _, k := range []string{attr, "name"} {
			if v, ok := av[k].(string); ok {
				if strings.EqualFold(v, value) {
					return map[string]any{k: v}, nil
				}
				opts = append(opts, v)
				break
			}
		}
	}
	return nil, fmt.Errorf("value %q not in allowed options: %s", value, strings.Join(opts, ", "))
}

// ResolveAccountID maps "me", an accountId, or a search term to an accountId.
func (s *Service) ResolveAccountID(ctx context.Context, who string) (string, error) {
	if who == "me" {
		u, err := s.Client.Myself(ctx)
		if err != nil {
			return "", err
		}
		return u.AccountID, nil
	}
	// Heuristic: accountIds contain ':' (e.g. 557058:...) or are long opaque ids.
	if strings.Contains(who, ":") {
		return who, nil
	}
	users, err := s.Client.FindUsers(ctx, who)
	if err != nil {
		return "", err
	}
	var active []client.User
	for _, u := range users {
		if u.Active {
			active = append(active, u)
		}
	}
	if len(active) == 1 {
		return active[0].AccountID, nil
	}
	if len(active) == 0 {
		return "", fmt.Errorf("no active user matches %q", who)
	}
	var names []string
	for _, u := range active {
		names = append(names, fmt.Sprintf("%s (%s)", u.DisplayName, u.AccountID))
	}
	return "", fmt.Errorf("ambiguous user %q: %s", who, strings.Join(names, ", "))
}

// splitList splits a comma-separated flag value, trimming whitespace.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ResolveTransitionID matches a transition by name (case-insensitive) or id.
func (s *Service) ResolveTransitionID(ctx context.Context, issueKey, nameOrID string) (string, string, error) {
	trs, err := s.Client.Transitions(ctx, issueKey)
	if err != nil {
		return "", "", err
	}
	var names []string
	for _, t := range trs {
		label := t.Name
		if t.To != nil {
			label = t.To.Name
		}
		names = append(names, label)
		if strings.EqualFold(t.Name, nameOrID) || t.ID == nameOrID ||
			(t.To != nil && strings.EqualFold(t.To.Name, nameOrID)) {
			return t.ID, t.Name, nil
		}
	}
	return "", "", fmt.Errorf("no transition %q on %s (available: %s)", nameOrID, issueKey, strings.Join(names, ", "))
}
