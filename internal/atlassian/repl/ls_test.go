// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package repl

import (
	"strings"
	"testing"

	"workctl/internal/atlassian/client"
)

func TestParseLsArgs(t *testing.T) {
	cases := []struct {
		in   []string
		want lsArgs
		err  bool
	}{
		{nil, lsArgs{}, false},
		{[]string{"more"}, lsArgs{mode: "more"}, false},
		{[]string{"all", "by", "updated", "desc"}, lsArgs{mode: "all", field: "updated", dir: "desc"}, false},
		{[]string{"by", "Title"}, lsArgs{field: "title"}, false},
		{[]string{"desc", "by", "key"}, lsArgs{field: "key", dir: "desc"}, false},
		{[]string{"by"}, lsArgs{}, true},
		{[]string{"bogus"}, lsArgs{}, true},
		{[]string{"more", "by", "key"}, lsArgs{}, true}, // cannot re-sort a continuation
	}
	for _, c := range cases {
		got, err := parseLsArgs(c.in)
		if c.err {
			if err == nil {
				t.Errorf("parseLsArgs(%v): want error", c.in)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("parseLsArgs(%v) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
}

func TestJQLOrderClause(t *testing.T) {
	if got, _ := jqlOrderClause("", ""); got != " ORDER BY updated DESC" {
		t.Fatalf("default = %q", got)
	}
	if got, _ := jqlOrderClause("key", ""); got != " ORDER BY key ASC" {
		t.Fatalf("key default dir = %q", got)
	}
	if got, _ := jqlOrderClause("rank", "asc"); got != " ORDER BY Rank ASC" {
		t.Fatalf("rank = %q", got)
	}
	if _, err := jqlOrderClause("color", ""); err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("want field list error, got %v", err)
	}
}

func TestSpaceSortParam(t *testing.T) {
	if got, _ := spaceSortParam("", ""); got != "title" {
		t.Fatalf("default = %q", got)
	}
	if got, _ := spaceSortParam("modified", ""); got != "-modified-date" {
		t.Fatalf("modified default dir = %q", got)
	}
	if got, _ := spaceSortParam("title", "desc"); got != "-title" {
		t.Fatalf("title desc = %q", got)
	}
	if _, err := spaceSortParam("size", ""); err == nil {
		t.Fatal("want error for unknown field")
	}
}

func TestSortChildren(t *testing.T) {
	kids := []client.ContentChild{{Title: "b"}, {Title: "a"}, {Title: "c"}}
	if err := sortChildren(kids, "title", ""); err != nil || kids[0].Title != "a" {
		t.Fatalf("asc = %+v, %v", kids, err)
	}
	if err := sortChildren(kids, "title", "desc"); err != nil || kids[0].Title != "c" {
		t.Fatalf("desc = %+v, %v", kids, err)
	}
	if err := sortChildren(kids, "modified", ""); err == nil {
		t.Fatal("want error for unsupported field")
	}
}

func TestNormalizeIssueKey(t *testing.T) {
	inProject := &Session{}
	inProject.ctx = navCtx{level: levelProject, projectKey: "WEBAPP"}
	root := &Session{} // no project context

	cases := []struct {
		s    *Session
		tok  string
		want string
	}{
		{inProject, "842", "WEBAPP-842"},    // bare id expands
		{inProject, "webapp-9", "WEBAPP-9"}, // full key just upper-cased
		{inProject, "OTHER-3", "OTHER-3"},   // cross-project full key kept
		{root, "842", "842"},                // no project: left as-is
	}
	for _, c := range cases {
		if got := c.s.normalizeIssueKey(c.tok); got != c.want {
			t.Errorf("normalizeIssueKey(%q) = %q; want %q", c.tok, got, c.want)
		}
	}
}

func TestIsIssueKeyToken(t *testing.T) {
	inProject := &Session{}
	inProject.ctx = navCtx{level: levelProject, projectKey: "WEBAPP"}
	root := &Session{}

	if !inProject.isIssueKeyToken("842") {
		t.Error("bare id should be a key token inside a project")
	}
	if !inProject.isIssueKeyToken("WEBAPP-1") {
		t.Error("full key should be a key token")
	}
	if inProject.isIssueKeyToken("Done") {
		t.Error("a word (status/field) must not be taken as a key")
	}
	if root.isIssueKeyToken("842") {
		t.Error("bare id must not be a key token without a project context")
	}
	if !root.isIssueKeyToken("WEBAPP-1") {
		t.Error("full key is a key token even at root")
	}
}

func TestJiraFindJQL(t *testing.T) {
	inProject := &Session{}
	inProject.ctx = navCtx{level: levelProject, projectKey: "AB"}
	root := &Session{}

	cases := []struct {
		name string
		s    *Session
		text string
		want string
	}{
		// the reported bug: a full key must be matched by key, not text ~.
		{"full key", inProject, "AB-1557", "key = AB-1557 ORDER BY updated DESC"},
		{"bare id in project", inProject, "1557", "key = AB-1557 ORDER BY updated DESC"},
		{"cross-project key", inProject, "OTHER-3", "key = OTHER-3 ORDER BY updated DESC"},
		// non-key terms stay a project-scoped full-text search.
		{"free text", inProject, "slow login page", `project = AB AND text ~ "slow login page" ORDER BY updated DESC`},
		// a bare number without project context is not a key -> full text.
		{"bare id at root", root, "1557", "text ~ 1557 ORDER BY updated DESC"},
	}
	for _, c := range cases {
		if got := c.s.jiraFindJQL(c.text); got != c.want {
			t.Errorf("%s: jiraFindJQL(%q) = %q; want %q", c.name, c.text, got, c.want)
		}
	}
}

func TestParseTraceArgs(t *testing.T) {
	cases := []struct {
		args            []string
		ctxKey, project string
		key             string
		depth           int
		dir             string
		err             bool
	}{
		{nil, "AB-1", "AB", "AB-1", 2, "both", false},                     // bare trace in issue ctx
		{[]string{"down"}, "AB-1", "AB", "AB-1", 2, "down", false},        // direction only
		{[]string{"3", "up"}, "AB-1", "AB", "AB-1", 3, "up", false},       // depth + direction
		{[]string{"up", "3"}, "AB-1", "AB", "AB-1", 3, "up", false},       // any order
		{[]string{"AB-9", "1", "both"}, "", "", "AB-9", 1, "both", false}, // explicit key
		{[]string{"ab-9"}, "AB-1", "AB", "AB-9", 2, "both", false},        // key overrides ctx
		{[]string{"743"}, "", "AB", "AB-743", 2, "both", false},           // big number = issue in project
		{[]string{"outward"}, "AB-1", "AB", "AB-1", 2, "outward", false},  // alias still accepted
		{nil, "", "", "", 0, "", true},                                    // no ctx, no key
		{[]string{"sideways"}, "AB-1", "AB", "", 0, "", true},             // junk arg
	}
	for _, c := range cases {
		key, depth, dir, err := parseTraceArgs(c.args, c.ctxKey, c.project)
		if c.err {
			if err == nil {
				t.Errorf("parseTraceArgs(%v): want error", c.args)
			}
			continue
		}
		if err != nil || key != c.key || depth != c.depth || dir != c.dir {
			t.Errorf("parseTraceArgs(%v, %q, %q) = %q,%d,%q,%v; want %q,%d,%q",
				c.args, c.ctxKey, c.project, key, depth, dir, err, c.key, c.depth, c.dir)
		}
	}
}

func TestSortByField(t *testing.T) {
	ps := []client.Project{{Key: "B", Name: "x"}, {Key: "A", Name: "y"}}
	keys := map[string]func(client.Project) string{
		"key": func(p client.Project) string { return p.Key },
	}
	if err := sortByField(ps, "key", "", keys); err != nil || ps[0].Key != "A" {
		t.Fatalf("asc = %+v, %v", ps, err)
	}
	if err := sortByField(ps, "key", "desc", keys); err != nil || ps[0].Key != "B" {
		t.Fatalf("desc = %+v, %v", ps, err)
	}
	if err := sortByField(ps, "lead", "", keys); err == nil {
		t.Fatal("want error for unknown field")
	}
}
