// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package issue is the provider-neutral issue layer: the same show, search and update operations and one output
// shape for Jira Server/Data Center (profile kind jira) and Jira Cloud (profile kind atlassian). Both speak REST
// API v2 through the workctl api client; they differ only in search pagination and how a user is addressed.
package issue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"workctl/internal/api"
	atlclient "workctl/internal/atlassian/client"
	"workctl/internal/config"
)

// Kinds are the profile kinds that serve issues.
var Kinds = []string{"jira", "atlassian"}

// Issue is the provider-neutral view of one issue.
type Issue struct {
	Key         string       `json:"key"`
	URL         string       `json:"url,omitempty"`
	Title       string       `json:"title"`
	Status      string       `json:"status,omitempty"`
	Resolution  string       `json:"resolution,omitempty"`
	Type        string       `json:"type,omitempty"`
	Priority    string       `json:"priority,omitempty"`
	Assignee    string       `json:"assignee,omitempty"`
	Reporter    string       `json:"reporter,omitempty"`
	FixVersions []string     `json:"fix_versions,omitempty"`
	Labels      []string     `json:"labels,omitempty"`
	Created     string       `json:"created,omitempty"`
	Updated     string       `json:"updated,omitempty"`
	Description string       `json:"description,omitempty"`
	Links       []Link       `json:"links,omitempty"`
	Comments    []Comment    `json:"comments,omitempty"`
	Transitions []Transition `json:"transitions,omitempty"`
}

// Link is one issue link as seen from the issue: "ARGOS-1 duplicates ARGOS-2" is {Relation: "duplicates", Key: "ARGOS-2"}.
type Link struct {
	Relation string `json:"relation"`
	Key      string `json:"key"`
	Title    string `json:"title,omitempty"`
	Status   string `json:"status,omitempty"`
}

// Comment is one issue comment.
type Comment struct {
	Author  string `json:"author,omitempty"`
	Created string `json:"created,omitempty"`
	Body    string `json:"body"`
}

// Transition is one workflow transition available to the account.
type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   string `json:"to,omitempty"`
}

// SearchResult is one page of a search.
type SearchResult struct {
	Total         int     `json:"total,omitempty"`
	StartAt       int     `json:"start_at,omitempty"`
	NextPageToken string  `json:"next_page_token,omitempty"`
	Issues        []Issue `json:"issues"`
}

// Include selects the optional parts of Show.
type Include struct {
	Comments    bool
	Transitions bool
}

// ParseInclude reads "comments", "transitions" (and "links", always present) from a list.
func ParseInclude(items []string) (Include, error) {
	var in Include
	for _, item := range items {
		switch strings.ToLower(strings.TrimSpace(item)) {
		case "comments":
			in.Comments = true
		case "transitions":
			in.Transitions = true
		case "links", "":
		default:
			return in, fmt.Errorf("include: unknown part %q (comments, transitions, links)", item)
		}
	}
	return in, nil
}

// Backend serves issues from one profile.
type Backend struct {
	Provider string // profile kind: jira or atlassian
	Profile  string
	base     string
	c        doer
}

type doer interface {
	Do(ctx context.Context, method, path string, q url.Values, body []byte, contentType string) ([]byte, string, error)
	Close() error
}

// Open connects to the profile that serves key: the named profile, else the jira/atlassian profile the
// configuration resolves for key (--profile, projects match, defaults.<kind>, the only one).
func Open(cfg *config.Config, profileName, key string) (*Backend, error) {
	name, profile, err := ResolveProfile(cfg, profileName, key)
	if err != nil {
		return nil, err
	}
	c, err := api.NewProfileClient(cfg, name, profile)
	if err != nil {
		return nil, err
	}
	return &Backend{Provider: profile.Kind, Profile: name, base: config.NormalizeBaseURL(profile.URL), c: c}, nil
}

// ResolveProfile picks the profile for key among the jira and atlassian profiles.
func ResolveProfile(cfg *config.Config, profileName, key string) (string, config.Profile, error) {
	if profileName != "" {
		p, ok := cfg.Profiles[profileName]
		if !ok || !isIssueKind(p.Kind) {
			return "", config.Profile{}, fmt.Errorf("profile %q is not a configured jira or atlassian profile", profileName)
		}
		return profileName, p, nil
	}
	var kinds []string
	for _, k := range Kinds {
		if cfg.Has(k) {
			kinds = append(kinds, k)
		}
	}
	switch len(kinds) {
	case 0:
		return "", config.Profile{}, fmt.Errorf("no jira or atlassian profile is configured")
	case 1:
		return cfg.ResolveFor(kinds[0], key)
	}
	if p, ok := cfg.Profiles[cfg.ProfileName()]; ok && isIssueKind(p.Kind) {
		return cfg.ProfileName(), p, nil
	}
	for _, k := range kinds {
		if name, p, err := cfg.ResolveFor(k, key); err == nil && p.MatchResource(key) > 0 {
			return name, p, nil
		}
	}
	return "", config.Profile{}, fmt.Errorf("both jira and atlassian profiles are configured and none lists the project of %q: pass profile", key)
}

func isIssueKind(kind string) bool { return kind == "jira" || kind == "atlassian" }

// Close releases the client (form-login sessions log out).
func (b *Backend) Close() error { return b.c.Close() }

func (b *Backend) call(ctx context.Context, method, path string, q url.Values, body any) ([]byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return nil, err
		}
	}
	out, _, err := b.c.Do(ctx, method, path, q, payload, "application/json")
	return out, err
}

func (b *Backend) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	data, err := b.call(ctx, http.MethodGet, path, q, nil)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func issuePath(key string, rest ...string) string {
	return "/rest/api/2/issue/" + url.PathEscape(key) + strings.Join(rest, "")
}

const showFields = "summary,status,resolution,issuetype,priority,assignee,reporter,fixVersions,labels,created,updated,description,issuelinks"

// Show returns one issue in the common shape.
func (b *Backend) Show(ctx context.Context, key string, in Include) (*Issue, error) {
	data, err := b.ShowRaw(ctx, key, in)
	if err != nil {
		return nil, err
	}
	var r rawIssue
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	is := b.normalize(r)
	if in.Transitions {
		ts, err := b.transitions(ctx, key, false)
		if err != nil {
			return nil, err
		}
		for _, t := range ts {
			is.Transitions = append(is.Transitions, Transition{ID: t.ID, Name: t.Name, To: t.To.Name})
		}
	}
	return &is, nil
}

// ShowRaw returns the provider JSON of one issue with the fields Show uses.
func (b *Backend) ShowRaw(ctx context.Context, key string, in Include) ([]byte, error) {
	fields := showFields
	if in.Comments {
		fields += ",comment"
	}
	return b.call(ctx, http.MethodGet, issuePath(key), url.Values{"fields": {fields}}, nil)
}

// Search runs a JQL query. Jira Server pages with startAt; Jira Cloud with nextPageToken.
func (b *Backend) Search(ctx context.Context, jql string, startAt, maxResults int, pageToken string) (*SearchResult, error) {
	if maxResults <= 0 {
		maxResults = 50
	}
	body := map[string]any{"jql": jql, "maxResults": maxResults, "fields": strings.Split(showFields, ",")}
	path := "/rest/api/2/search"
	if b.Provider == "atlassian" {
		if startAt > 0 {
			return nil, fmt.Errorf("jira cloud pages with next_page_token, not start_at")
		}
		path = "/rest/api/2/search/jql"
		if pageToken != "" {
			body["nextPageToken"] = pageToken
		}
	} else {
		body["startAt"] = startAt
	}
	data, err := b.call(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return nil, err
	}
	var r struct {
		Total         int        `json:"total"`
		StartAt       int        `json:"startAt"`
		NextPageToken string     `json:"nextPageToken"`
		Issues        []rawIssue `json:"issues"`
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	out := &SearchResult{Total: r.Total, StartAt: r.StartAt, NextPageToken: r.NextPageToken, Issues: []Issue{}}
	for _, ri := range r.Issues {
		out.Issues = append(out.Issues, b.normalize(ri))
	}
	return out, nil
}

type named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type person struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

func (p *person) String() string {
	if p == nil {
		return ""
	}
	if p.Name != "" {
		return p.Name
	}
	return p.DisplayName
}

type linkedIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary string `json:"summary"`
		Status  *named `json:"status"`
	} `json:"fields"`
}

type rawIssue struct {
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Status      *named          `json:"status"`
		Resolution  *named          `json:"resolution"`
		IssueType   *named          `json:"issuetype"`
		Priority    *named          `json:"priority"`
		Assignee    *person         `json:"assignee"`
		Reporter    *person         `json:"reporter"`
		FixVersions []named         `json:"fixVersions"`
		Labels      []string        `json:"labels"`
		Created     string          `json:"created"`
		Updated     string          `json:"updated"`
		Description json.RawMessage `json:"description"`
		IssueLinks  []struct {
			Type struct {
				Inward  string `json:"inward"`
				Outward string `json:"outward"`
			} `json:"type"`
			InwardIssue  *linkedIssue `json:"inwardIssue"`
			OutwardIssue *linkedIssue `json:"outwardIssue"`
		} `json:"issuelinks"`
		Comment *struct {
			Comments []struct {
				Author  *person         `json:"author"`
				Created string          `json:"created"`
				Body    json.RawMessage `json:"body"`
			} `json:"comments"`
		} `json:"comment"`
	} `json:"fields"`
}

func nameOf(n *named) string {
	if n == nil {
		return ""
	}
	return n.Name
}

// text reads a v2 string or, defensively, an ADF document.
func text(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return atlclient.ADFToText(raw)
}

func (b *Backend) normalize(r rawIssue) Issue {
	f := r.Fields
	is := Issue{
		Key: r.Key, URL: b.base + "/browse/" + r.Key, Title: f.Summary,
		Status: nameOf(f.Status), Resolution: nameOf(f.Resolution), Type: nameOf(f.IssueType), Priority: nameOf(f.Priority),
		Assignee: f.Assignee.String(), Reporter: f.Reporter.String(), Labels: f.Labels,
		Created: f.Created, Updated: f.Updated, Description: text(f.Description),
	}
	for _, v := range f.FixVersions {
		is.FixVersions = append(is.FixVersions, v.Name)
	}
	for _, l := range f.IssueLinks {
		rel, other := l.Type.Outward, l.OutwardIssue
		if other == nil {
			rel, other = l.Type.Inward, l.InwardIssue
		}
		if other != nil {
			is.Links = append(is.Links, Link{Relation: rel, Key: other.Key, Title: other.Fields.Summary, Status: nameOf(other.Fields.Status)})
		}
	}
	if f.Comment != nil {
		for _, c := range f.Comment.Comments {
			is.Comments = append(is.Comments, Comment{Author: c.Author.String(), Created: c.Created, Body: text(c.Body)})
		}
	}
	return is
}

// Update is one change of an issue: optional link, transition (with resolution, fix versions and comment when
// the transition screen has them), fix version edit, assignee and comment, applied in that order.
type Update struct {
	Transition  string    `json:"transition,omitempty"` // id, name or target status
	Resolve     bool      `json:"resolve,omitempty"`    // pick the first transition that sets a resolution
	Resolution  string    `json:"resolution,omitempty"` // name or id
	FixVersions []string  `json:"fix_versions,omitempty"`
	Assignee    string    `json:"assignee,omitempty"` // user name (Server) or account id (Cloud)
	Comment     string    `json:"comment,omitempty"`
	Link        *LinkSpec `json:"link,omitempty"`
}

// LinkSpec links the issue to Key; Type is a link type name or its outward/inward phrase ("duplicates").
type LinkSpec struct {
	Type string `json:"type"`
	Key  string `json:"key"`
}

// Empty reports whether the update changes nothing.
func (u Update) Empty() bool {
	return u.Transition == "" && !u.Resolve && u.Resolution == "" && len(u.FixVersions) == 0 && u.Assignee == "" &&
		u.Comment == "" && u.Link == nil
}

// Step is one request of a plan with a human description.
type Step struct {
	Describe string
	method   string
	path     string
	body     any
}

// Plan resolves an update into requests, reading the transitions, edit metadata and link types it needs,
// without changing anything.
func (b *Backend) Plan(ctx context.Context, key string, u Update) ([]Step, error) {
	if u.Empty() {
		return nil, fmt.Errorf("nothing to update")
	}
	var steps []Step
	if u.Link != nil {
		s, err := b.linkStep(ctx, key, *u.Link)
		if err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	commentDone := false
	if u.Transition != "" || u.Resolve {
		t, err := b.pickTransition(ctx, key, u)
		if err != nil {
			return nil, err
		}
		fields := map[string]any{}
		var with []string
		if u.Resolution != "" {
			v, err := t.allowed("resolution", u.Resolution)
			if err != nil {
				return nil, err
			}
			fields["resolution"] = map[string]string{"id": v.ID}
			with = append(with, "resolution "+v.Name)
		}
		if _, onScreen := t.Fields["fixVersions"]; onScreen && len(u.FixVersions) > 0 {
			ids, names, err := t.allowedList("fixVersions", u.FixVersions)
			if err != nil {
				return nil, err
			}
			fields["fixVersions"] = ids
			with = append(with, "fix versions "+strings.Join(names, ", "))
			u.FixVersions = nil
		}
		body := map[string]any{"transition": map[string]string{"id": t.ID}}
		if len(fields) > 0 {
			body["fields"] = fields
		}
		if u.Comment != "" {
			body["update"] = map[string]any{"comment": []any{map[string]any{"add": map[string]string{"body": u.Comment}}}}
			with = append(with, "comment")
			commentDone = true
		}
		d := fmt.Sprintf("transition %q (%s) to %s", t.Name, t.ID, t.To.Name)
		if len(with) > 0 {
			d += " with " + strings.Join(with, ", ")
		}
		steps = append(steps, Step{Describe: d, method: http.MethodPost, path: issuePath(key, "/transitions"), body: body})
	} else if u.Resolution != "" {
		return nil, fmt.Errorf("a resolution is set by a transition: add transition or resolve")
	}
	if len(u.FixVersions) > 0 {
		s, err := b.editFixVersions(ctx, key, u.FixVersions)
		if err != nil {
			return nil, err
		}
		steps = append(steps, s)
	}
	if u.Assignee != "" {
		body := map[string]string{"name": u.Assignee}
		if b.Provider == "atlassian" {
			body = map[string]string{"accountId": u.Assignee}
		}
		steps = append(steps, Step{Describe: "assign to " + u.Assignee, method: http.MethodPut, path: issuePath(key, "/assignee"), body: body})
	}
	if u.Comment != "" && !commentDone {
		steps = append(steps, Step{Describe: "comment", method: http.MethodPost, path: issuePath(key, "/comment"), body: map[string]string{"body": u.Comment}})
	}
	return steps, nil
}

// Apply runs the steps of a plan in order and stops at the first failure, reporting what already happened.
func (b *Backend) Apply(ctx context.Context, steps []Step) ([]string, error) {
	var done []string
	for _, s := range steps {
		if _, err := b.call(ctx, s.method, s.path, nil, s.body); err != nil {
			if len(done) > 0 {
				return done, fmt.Errorf("%s: %w (already done: %s)", s.Describe, err, strings.Join(done, "; "))
			}
			return done, fmt.Errorf("%s: %w", s.Describe, err)
		}
		done = append(done, s.Describe)
	}
	return done, nil
}

type fieldMeta struct {
	AllowedValues []named `json:"allowedValues"`
}

type transitionMeta struct {
	ID     string               `json:"id"`
	Name   string               `json:"name"`
	To     named                `json:"to"`
	Fields map[string]fieldMeta `json:"fields"`
}

func (b *Backend) transitions(ctx context.Context, key string, withFields bool) ([]transitionMeta, error) {
	q := url.Values{}
	if withFields {
		q.Set("expand", "transitions.fields")
	}
	var r struct {
		Transitions []transitionMeta `json:"transitions"`
	}
	if err := b.getJSON(ctx, issuePath(key, "/transitions"), q, &r); err != nil {
		return nil, err
	}
	return r.Transitions, nil
}

func (b *Backend) pickTransition(ctx context.Context, key string, u Update) (transitionMeta, error) {
	ts, err := b.transitions(ctx, key, true)
	if err != nil {
		return transitionMeta{}, err
	}
	for _, t := range ts {
		if u.Transition != "" && (t.ID == u.Transition || strings.EqualFold(t.Name, u.Transition) || strings.EqualFold(t.To.Name, u.Transition)) {
			return t, nil
		}
		if _, sets := t.Fields["resolution"]; u.Transition == "" && sets {
			return t, nil
		}
	}
	var avail []string
	for _, t := range ts {
		avail = append(avail, fmt.Sprintf("%s %q -> %s", t.ID, t.Name, t.To.Name))
	}
	want := strconv.Quote(u.Transition)
	if u.Transition == "" {
		want = "that sets a resolution"
	}
	return transitionMeta{}, fmt.Errorf("%s: no transition %s; available: %s", key, want, strings.Join(avail, ", "))
}

// standardResolutions are the ids of Jira's built-in resolutions. A localised server names them differently
// ("Duplikat"), so an English name falls back to its id.
var standardResolutions = map[string]string{
	"fixed": "1", "won't fix": "2", "wont fix": "2", "duplicate": "3", "incomplete": "4", "cannot reproduce": "5", "done": "10000",
}

func (t transitionMeta) allowed(field, want string) (named, error) {
	f, ok := t.Fields[field]
	if !ok {
		return named{}, fmt.Errorf("transition %q has no %s field", t.Name, field)
	}
	if v, ok := matchValue(f.AllowedValues, want); ok {
		return v, nil
	}
	if id, ok := standardResolutions[strings.ToLower(want)]; ok && field == "resolution" {
		if v, ok := matchValue(f.AllowedValues, id); ok {
			return v, nil
		}
	}
	return named{}, fmt.Errorf("%s %q is not allowed by %q; allowed: %s", field, want, t.Name, describeValues(f.AllowedValues, want))
}

func (t transitionMeta) allowedList(field string, wants []string) ([]map[string]string, []string, error) {
	var ids []map[string]string
	var names []string
	for _, w := range wants {
		v, err := t.allowed(field, w)
		if err != nil {
			return nil, nil, err
		}
		ids = append(ids, map[string]string{"id": v.ID})
		names = append(names, v.Name)
	}
	return ids, names, nil
}

func matchValue(values []named, want string) (named, bool) {
	for _, v := range values {
		if v.ID == want || strings.EqualFold(v.Name, want) {
			return v, true
		}
	}
	return named{}, false
}

// describeValues lists allowed values; long lists (fix versions) are narrowed to the ones containing want.
func describeValues(values []named, want string) string {
	var out []string
	for _, v := range values {
		if len(values) <= 20 || strings.Contains(strings.ToLower(v.Name), strings.ToLower(want)) {
			out = append(out, v.Name)
		}
	}
	if len(out) > 20 {
		out = append(out[:20], "...")
	}
	if len(out) == 0 {
		return fmt.Sprintf("%d values, none containing %q", len(values), want)
	}
	return strings.Join(out, ", ")
}

func (b *Backend) editFixVersions(ctx context.Context, key string, wants []string) (Step, error) {
	var meta struct {
		Fields map[string]fieldMeta `json:"fields"`
	}
	if err := b.getJSON(ctx, issuePath(key, "/editmeta"), nil, &meta); err != nil {
		return Step{}, err
	}
	f, ok := meta.Fields["fixVersions"]
	if !ok {
		return Step{}, fmt.Errorf("%s: fix versions are not editable by this account", key)
	}
	t := transitionMeta{Name: "edit", Fields: map[string]fieldMeta{"fixVersions": f}}
	ids, names, err := t.allowedList("fixVersions", wants)
	if err != nil {
		return Step{}, err
	}
	return Step{Describe: "set fix versions " + strings.Join(names, ", "), method: http.MethodPut, path: issuePath(key),
		body: map[string]any{"fields": map[string]any{"fixVersions": ids}}}, nil
}

func (b *Backend) linkStep(ctx context.Context, key string, spec LinkSpec) (Step, error) {
	if spec.Key == "" || spec.Type == "" {
		return Step{}, fmt.Errorf("link needs type and key")
	}
	var r struct {
		IssueLinkTypes []struct {
			Name    string `json:"name"`
			Inward  string `json:"inward"`
			Outward string `json:"outward"`
		} `json:"issueLinkTypes"`
	}
	if err := b.getJSON(ctx, "/rest/api/2/issueLinkType", nil, &r); err != nil {
		return Step{}, err
	}
	var known []string
	for _, t := range r.IssueLinkTypes {
		known = append(known, t.Outward)
		inward := strings.EqualFold(spec.Type, t.Inward) && !strings.EqualFold(spec.Type, t.Outward)
		if !inward && !strings.EqualFold(spec.Type, t.Name) && !strings.EqualFold(spec.Type, t.Outward) {
			continue
		}
		// Jira reads {inwardIssue: A, outwardIssue: B} as "A <outward> B".
		from, to, rel := key, spec.Key, t.Outward
		if inward {
			from, to, rel = spec.Key, key, t.Inward
		}
		return Step{
			Describe: fmt.Sprintf("link %s %s %s", key, rel, spec.Key),
			method:   http.MethodPost, path: "/rest/api/2/issueLink",
			body: map[string]any{"type": map[string]string{"name": t.Name}, "inwardIssue": map[string]string{"key": from}, "outwardIssue": map[string]string{"key": to}},
		}, nil
	}
	return Step{}, fmt.Errorf("unknown link type %q; known: %s", spec.Type, strings.Join(known, ", "))
}
