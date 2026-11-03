// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package wizard holds the interactive (survey-driven) flows shared by the
// cobra commands and the REPL. Currently this is the guided issue-create form:
// it prompts for the issue type, every required create-screen field, a few
// always-useful optional fields, and offers to drop the issue into the
// project's active sprint.
package wizard

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/AlecAivazis/survey/v2"
	"workctl/internal/atlassian/client"
	"workctl/internal/atlassian/service"
)

// apiTimeout bounds a single HTTP request. Each Jira call gets its own context
// so the (possibly slow) human typing between prompts never counts against a
// request deadline.
const apiTimeout = 60 * time.Second

func apiCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), apiTimeout)
}

// optionalFields are non-required fields the form always offers (blank skips),
// located on the create screen by system id first, then by display name. Each
// is only prompted when the issue type's create screen actually has it, so the
// same list works across types (a Story with no Affected Environment skips it).
// Custom fields (no stable system id) are matched by their display name.
var optionalFields = []struct{ id, name string }{
	{id: "description"},
	{id: "components"},
	{name: "Affected Environment Checkbox"},
	{id: "assignee"},
	{name: "Tester"},
}

// Create runs the interactive create form: pick an issue type (unless one is
// given), prompt for every required create-screen field plus a fixed set of
// optional ones, optionally drop the issue into the active sprint, then create
// it. parentKey, when set, links the new issue to a parent (sub-tasks).
func Create(svc *service.Service, projectKey, typeNameOrID, parentKey string) (*client.IssueRef, error) {
	typeID, err := resolveType(svc, projectKey, typeNameOrID)
	if err != nil {
		return nil, err
	}

	mctx, mcancel := apiCtx()
	metas, err := svc.Client.CreateMetaFields(mctx, projectKey, typeID)
	mcancel()
	if err != nil {
		return nil, err
	}

	fields := map[string]any{
		"project":   map[string]any{"key": projectKey},
		"issuetype": map[string]any{"id": typeID},
	}
	if parentKey != "" {
		fields["parent"] = map[string]any{"key": parentKey}
	}

	asked := map[string]bool{}
	ask := func(m *client.FieldMeta, optional bool) error {
		v, err := promptFieldValue(svc, projectKey, m, optional)
		if err != nil {
			return err
		}
		if v != nil {
			fields[m.IDKey()] = v
		}
		asked[m.IDKey()] = true
		return nil
	}

	for _, m := range requiredFields(metas) {
		if err := ask(&m, false); err != nil {
			return nil, err
		}
	}
	for _, want := range optionalFields {
		if m := findMeta(metas, want.id, want.name); m != nil && !asked[m.IDKey()] {
			if err := ask(m, true); err != nil {
				return nil, err
			}
		}
	}

	// Offer the project's active sprint, if it has one (scrum boards only).
	sprint := ActiveSprint(svc, projectKey)
	addToSprint := false
	if sprint != nil {
		if err := survey.AskOne(
			&survey.Confirm{Message: fmt.Sprintf("Add to current sprint %q?", sprint.Name), Default: false},
			&addToSprint,
		); err != nil {
			return nil, err
		}
	}

	cctx, ccancel := apiCtx()
	ref, err := svc.Client.CreateIssue(cctx, fields)
	ccancel()
	if err != nil {
		return nil, err
	}

	if addToSprint {
		sctx, scancel := apiCtx()
		err := svc.Client.AddIssuesToSprint(sctx, sprint.ID, ref.Key)
		scancel()
		if err != nil {
			// The issue exists; surface the key so it is not lost.
			return ref, fmt.Errorf("issue %s created but adding to sprint %q failed: %w", ref.Key, sprint.Name, err)
		}
	}
	return ref, nil
}

// findMeta locates a create-screen field by system id (preferred) or display
// name (case-insensitive). Returns nil when the screen has no such field.
func findMeta(metas []client.FieldMeta, id, name string) *client.FieldMeta {
	for i := range metas {
		if id != "" && metas[i].IDKey() == id {
			return &metas[i]
		}
		if name != "" && strings.EqualFold(metas[i].Name, name) {
			return &metas[i]
		}
	}
	return nil
}

// ActiveSprint returns the active sprint of the scrum board that belongs to the
// project, or nil when there is none. The board must be owned by projectKey
// (location), not merely reference it: a shared board from another project
// would otherwise drop the issue into a foreign sprint. Among parallel active
// sprints on that board, the one named for the project wins (pickProjectSprint).
// Lookup failures are treated as "no sprint" so a board-API hiccup never blocks
// the caller.
func ActiveSprint(svc *service.Service, projectKey string) *client.Sprint {
	bctx, bcancel := apiCtx()
	boards, err := svc.Client.Boards(bctx, projectKey, 0, 50)
	bcancel()
	if err != nil || boards == nil {
		return nil
	}
	for _, b := range boards.Values {
		if b.Type != "scrum" || b.Location.ProjectKey != projectKey {
			continue
		}
		sctx, scancel := apiCtx()
		sp, err := svc.Client.BoardSprints(sctx, b.ID, "active", 0, 50)
		scancel()
		if err != nil || sp == nil || len(sp.Values) == 0 {
			continue
		}
		if s := pickProjectSprint(sp.Values, projectKey); s != nil {
			return s
		}
	}
	return nil
}

// pickProjectSprint chooses the project's sprint among a board's active sprints.
// A board can run several sprints in parallel (a shared company-wide
// sprint can be pulled onto team boards alongside the team's own), so a lone
// active sprint is used as-is, but when several are active the one whose name's
// first token is the project key wins. Returns nil when several are active and
// none clearly belongs to the project, so the form never guesses wrong.
func pickProjectSprint(sprints []client.Sprint, projectKey string) *client.Sprint {
	if len(sprints) == 1 {
		return &sprints[0]
	}
	for i := range sprints {
		if sprintMatchesProject(sprints[i].Name, projectKey) {
			return &sprints[i]
		}
	}
	return nil
}

// sprintMatchesProject reports whether the sprint name's first whitespace token
// equals the project key ("AB Sprint 28" belongs to AB; "Alpha Sprint 16" and
// "ABX Sprint 1" do not).
func sprintMatchesProject(name, projectKey string) bool {
	first, _, _ := strings.Cut(name, " ")
	return strings.EqualFold(first, projectKey)
}

// resolveType maps a type name/id to its id, or prompts for one when none is
// given.
func resolveType(svc *service.Service, projectKey, typeNameOrID string) (string, error) {
	c, cancel := apiCtx()
	types, err := svc.Client.CreateMetaIssueTypes(c, projectKey)
	cancel()
	if err != nil {
		return "", err
	}
	if len(types) == 0 {
		return "", fmt.Errorf("no creatable issue type in %s", projectKey)
	}
	if typeNameOrID != "" {
		for _, t := range types {
			if t.ID == typeNameOrID || strings.EqualFold(t.Name, typeNameOrID) {
				return t.ID, nil
			}
		}
		return "", fmt.Errorf("issue type %q not found in %s", typeNameOrID, projectKey)
	}
	var names []string
	byName := map[string]string{}
	for _, t := range types {
		names = append(names, t.Name)
		byName[t.Name] = t.ID
	}
	var pick string
	if err := survey.AskOne(&survey.Select{Message: "Issue type:", Options: names}, &pick); err != nil {
		return "", err
	}
	return byName[pick], nil
}

// requiredFields selects the create-screen fields the wizard should prompt for:
// required fields that have no server-side default, minus the ones the wizard
// already sets (project/issuetype/parent/reporter). summary is always asked.
func requiredFields(metas []client.FieldMeta) []client.FieldMeta {
	var out []client.FieldMeta
	for _, m := range metas {
		id := m.IDKey()
		if id == "project" || id == "issuetype" || id == "reporter" || id == "parent" {
			continue
		}
		required := m.Required && !m.HasDefaultValue
		if id == "summary" {
			required = true
		}
		if required {
			out = append(out, m)
		}
	}
	return out
}

// promptFieldValue asks for one field's value according to its schema. User
// fields show a searchable select of assignable users; fields with enumerated
// allowed values show a (multi-)select; everything else is free input coerced
// by schema (description -> ADF, number -> float, ...). When optional, single
// selects offer a skip entry and blank input is allowed.
func promptFieldValue(svc *service.Service, projectKey string, m *client.FieldMeta, optional bool) (any, error) {
	label := m.Name + ":"
	sc := m.Schema

	if isUserField(sc) {
		return promptUser(svc, projectKey, label, sc.Type == "array", optional)
	}

	if opts := allowedOptionLabels(m); len(opts) > 0 {
		if sc != nil && sc.Type == "array" {
			var picks []string
			if err := survey.AskOne(&survey.MultiSelect{Message: label, Options: opts}, &picks); err != nil {
				return nil, err
			}
			if len(picks) == 0 {
				return nil, nil
			}
			// Coerce by the item schema rather than assuming {"value":...}:
			// option arrays want {"value":...} but versions/components want
			// {"name":...}. Hardcoding "value" made Jira silently drop the
			// picks and report the field as still required.
			c, cancel := apiCtx()
			defer cancel()
			return svc.CoerceFieldValue(c, m, strings.Join(picks, ","))
		}
		pick, err := selectOne(label, opts, optional)
		if err != nil || pick == "" {
			return nil, err
		}
		c, cancel := apiCtx()
		defer cancel()
		return svc.CoerceFieldValue(c, m, pick)
	}

	var in string
	var prompt survey.Prompt = &survey.Input{Message: label}
	// Description stays single-line on purpose; only custom textarea fields get
	// the multiline editor.
	if sc != nil && sc.Type == "string" && strings.HasSuffix(sc.Custom, ":textarea") {
		prompt = &survey.Multiline{Message: label}
	}
	if err := survey.AskOne(prompt, &in); err != nil {
		return nil, err
	}
	if in == "" {
		return nil, nil
	}
	c, cancel := apiCtx()
	defer cancel()
	return svc.CoerceFieldValue(c, m, in)
}

const skipOption = "(skip)"

// isUserField reports whether the schema is a single- or multi-user picker.
func isUserField(sc *client.FieldSchema) bool {
	return sc != nil && (sc.Type == "user" || (sc.Type == "array" && sc.Items == "user"))
}

// promptUser shows a searchable select of the project's assignable users and
// returns the chosen payload ({"accountId":...}, or an array for multi-user
// fields). Optional single fields offer a skip entry; no assignable users or a
// skipped/empty pick yields nil.
func promptUser(svc *service.Service, projectKey, label string, multi, optional bool) (any, error) {
	c, cancel := apiCtx()
	users, err := svc.Client.AssignableUsers(c, projectKey, "", 200)
	cancel()
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	labels := make([]string, 0, len(users))
	acct := make(map[string]string, len(users))
	for _, u := range users {
		l := userLabel(u)
		labels = append(labels, l)
		acct[l] = u.AccountID
	}
	if multi {
		var picks []string
		if err := survey.AskOne(&survey.MultiSelect{Message: label, Options: labels}, &picks); err != nil {
			return nil, err
		}
		if len(picks) == 0 {
			return nil, nil
		}
		arr := make([]any, 0, len(picks))
		for _, p := range picks {
			arr = append(arr, map[string]any{"accountId": acct[p]})
		}
		return arr, nil
	}
	pick, err := selectOne(label, labels, optional)
	if err != nil || pick == "" {
		return nil, err
	}
	return map[string]any{"accountId": acct[pick]}, nil
}

// selectOne runs a single-select (survey filters as the user types). When
// optional, a leading skip entry is added and maps back to an empty string.
func selectOne(label string, options []string, optional bool) (string, error) {
	if optional {
		options = append([]string{skipOption}, options...)
	}
	q := &survey.Select{Message: label, Options: options}
	if optional {
		q.Default = skipOption
	}
	var pick string
	if err := survey.AskOne(q, &pick); err != nil {
		return "", err
	}
	if pick == skipOption {
		return "", nil
	}
	return pick, nil
}

// userLabel renders a user as "Name <email>", falling back to name, then id.
func userLabel(u client.User) string {
	switch {
	case u.EmailAddress != "":
		return fmt.Sprintf("%s <%s>", u.DisplayName, u.EmailAddress)
	case u.DisplayName != "":
		return u.DisplayName
	default:
		return u.AccountID
	}
}

// allowedOptionLabels extracts the display labels of a field's allowedValues.
func allowedOptionLabels(m *client.FieldMeta) []string {
	if len(m.AllowedValues) == 0 {
		return nil
	}
	var allowed []map[string]any
	if err := json.Unmarshal(m.AllowedValues, &allowed); err != nil {
		return nil
	}
	var out []string
	for _, av := range allowed {
		for _, k := range []string{"value", "name"} {
			if v, ok := av[k].(string); ok {
				out = append(out, v)
				break
			}
		}
	}
	return out
}
