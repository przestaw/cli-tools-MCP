// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package issue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

// Available reports whether any profile serves issues.
func Available(cfg *config.Config) bool { return cfg.Has("jira") || cfg.Has("atlassian") }

// Tools are the issue_* MCP tools. issue_update and issue_create are always listed and refused without
// policy.allow_write unless dry_run is set.
func Tools() []mcp.Tool {
	profile := mcp.StrProp("jira or atlassian profile (account); omit to resolve by the issue's project and defaults")
	update := mcp.ObjSchema(map[string]any{
		"key":          mcp.StrProp("Issue key, e.g. ABC-123"),
		"transition":   mcp.StrProp("Transition id, name or target status (see issue_show include transitions)"),
		"resolve":      mcp.BoolProp("Use the first transition that sets a resolution (when transition is omitted)"),
		"resolution":   mcp.StrProp("Resolution name or id, set with the transition; English built-in names also work on localised servers"),
		"fix_versions": mcp.ArrProp("Fix version names or ids (replace the current ones)"),
		"summary":      mcp.StrProp("New title"),
		"description":  mcp.StrProp("New description (plain text or wiki markup)"),
		"labels":       mcp.ArrProp("Labels (replace the current ones; an empty array clears them)"),
		"assignee":     mcp.StrProp("User name (Jira Server) or account id (Jira Cloud)"),
		"comment":      mcp.StrProp("Comment to add (with the transition when there is one)"),
		"link_type":    mcp.StrProp("Link type name or phrase as seen from this issue, e.g. duplicates, blocks, relates to"),
		"link_key":     mcp.StrProp("Issue to link to"),
		"dry_run":      mcp.BoolProp("Only resolve and return the steps; changes nothing"),
		"profile":      profile,
	}, "key")
	create := mcp.ObjSchema(map[string]any{
		"project":     mcp.StrProp("Project key, e.g. ABC"),
		"type":        mcp.StrProp("Issue type name or id, e.g. Task, Bug, Sub-task"),
		"summary":     mcp.StrProp("Title"),
		"description": mcp.StrProp("Description (plain text or wiki markup)"),
		"labels":      mcp.ArrProp("Labels"),
		"assignee":    mcp.StrProp("User name (Jira Server) or account id (Jira Cloud)"),
		"priority":    mcp.StrProp("Priority name"),
		"parent":      mcp.StrProp("Parent issue key, for a sub-task"),
		"fields":      map[string]any{"type": "object", "description": "Other fields sent as-is, e.g. {\"customfield_10010\": \"x\"}; the named arguments win"},
		"dry_run":     mcp.BoolProp("Only resolve the issue type and return the request body; creates nothing"),
		"profile":     profile,
	}, "project", "type", "summary")
	return []mcp.Tool{
		{Name: "issue_show", Description: "Get an issue (Jira Server or Cloud) in a common shape: status, resolution, fix versions, links; optionally comments and transitions. format raw returns the provider JSON.", InputSchema: mcp.ObjSchema(map[string]any{
			"key":     mcp.StrProp("Issue key, e.g. ABC-123"),
			"include": mcp.ArrProp("Optional parts: comments, transitions"),
			"format":  mcp.StrProp("compact (default) or raw"),
			"profile": profile,
		}, "key")},
		{Name: "issue_search", Description: "Search issues with JQL (Jira Server or Cloud); one page in the common shape.", InputSchema: mcp.ObjSchema(map[string]any{
			"query":           mcp.StrProp("JQL"),
			"max_results":     mcp.IntProp("Page size (default 50)"),
			"start_at":        mcp.IntProp("Start index (Jira Server)"),
			"next_page_token": mcp.StrProp("Next page (Jira Cloud)"),
			"profile":         profile,
		}, "query")},
		{Name: "issue_update", Description: "Change an issue in one call: link, transition with resolution/fix versions/comment, summary, description, labels, fix versions, assignee, comment. Names are resolved to ids from the issue's own metadata. Refused without policy.allow_write unless dry_run.", InputSchema: update},
		{Name: "issue_create", Description: "Create an issue (Jira Server or Cloud); the issue type name is resolved to its id from the project's create metadata. Returns key, id and url. Refused without policy.allow_write unless dry_run.", InputSchema: create},
	}
}

// CallTool runs one issue_* tool.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) (out []byte, resultErr error) {
	key := mcp.ArgString(a, "key")
	if (name == "issue_update" || name == "issue_create") && !mcp.ArgBool(a, "dry_run") {
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	b, err := Open(cfg, mcp.ArgString(a, "profile"), cliutil.FirstNonEmpty(key, mcp.ArgString(a, "project")))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, b.Close()) }()
	switch name {
	case "issue_show":
		items, err := mcp.ArgStrings(a, "include")
		if err != nil {
			return nil, err
		}
		in, err := ParseInclude(items)
		if err != nil {
			return nil, err
		}
		if strings.EqualFold(mcp.ArgString(a, "format"), "raw") {
			return b.ShowRaw(ctx, key, in)
		}
		return marshal(b.Show(ctx, key, in))
	case "issue_search":
		return marshal(b.Search(ctx, mcp.ArgString(a, "query"), mcp.ArgInt(a, "start_at", 0), mcp.ArgInt(a, "max_results", 50), mcp.ArgString(a, "next_page_token")))
	case "issue_update":
		u, err := updateArgs(a)
		if err != nil {
			return nil, err
		}
		return runUpdate(ctx, b, key, u, mcp.ArgBool(a, "dry_run"))
	case "issue_create":
		n, err := createArgs(a)
		if err != nil {
			return nil, err
		}
		s, err := b.PlanCreate(ctx, n)
		if err != nil {
			return nil, err
		}
		if mcp.ArgBool(a, "dry_run") {
			return marshal(&Created{DryRun: true, Step: s.Describe, Body: s.body}, nil)
		}
		return marshal(b.Create(ctx, s))
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

func createArgs(a map[string]any) (NewIssue, error) {
	labels, err := mcp.ArgStrings(a, "labels")
	if err != nil {
		return NewIssue{}, err
	}
	extra, ok := a["fields"].(map[string]any)
	if !ok && a["fields"] != nil {
		return NewIssue{}, fmt.Errorf("fields must be an object")
	}
	return NewIssue{Project: mcp.ArgString(a, "project"), Type: mcp.ArgString(a, "type"), Fields: Fields{
		Summary: mcp.ArgString(a, "summary"), Description: mcp.ArgString(a, "description"), Labels: labels,
		Assignee: mcp.ArgString(a, "assignee"), Priority: mcp.ArgString(a, "priority"), Parent: mcp.ArgString(a, "parent"), Extra: extra,
	}}, nil
}

func updateArgs(a map[string]any) (Update, error) {
	fix, err := mcp.ArgStrings(a, "fix_versions")
	if err != nil {
		return Update{}, err
	}
	labels, err := mcp.ArgStrings(a, "labels")
	if err != nil {
		return Update{}, err
	}
	u := Update{
		Transition: mcp.ArgString(a, "transition"), Resolve: mcp.ArgBool(a, "resolve"), Resolution: mcp.ArgString(a, "resolution"),
		FixVersions: fix, Summary: mcp.ArgString(a, "summary"), Description: mcp.ArgString(a, "description"), Labels: labels,
		Assignee: mcp.ArgString(a, "assignee"), Comment: mcp.ArgString(a, "comment"),
	}
	if lt, lk := mcp.ArgString(a, "link_type"), mcp.ArgString(a, "link_key"); lt != "" || lk != "" {
		u.Link = &LinkSpec{Type: lt, Key: lk}
	}
	return u, nil
}

// UpdateResult is what issue_update returns: the steps (planned or done) and, after a change, the issue.
type UpdateResult struct {
	DryRun bool     `json:"dry_run,omitempty"`
	Steps  []string `json:"steps"`
	Issue  *Issue   `json:"issue,omitempty"`
}

func runUpdate(ctx context.Context, b *Backend, key string, u Update, dryRun bool) ([]byte, error) {
	steps, err := b.Plan(ctx, key, u)
	if err != nil {
		return nil, err
	}
	if dryRun {
		return marshal(&UpdateResult{DryRun: true, Steps: describe(steps)}, nil)
	}
	done, err := b.Apply(ctx, steps)
	if err != nil {
		return nil, err
	}
	is, err := b.Show(ctx, key, Include{})
	if err != nil {
		return nil, fmt.Errorf("done (%s), but reading the issue back failed: %w", strings.Join(done, "; "), err)
	}
	return marshal(&UpdateResult{Steps: done, Issue: is}, nil)
}

func describe(steps []Step) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Describe)
	}
	return out
}

func marshal[T any](v T, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
