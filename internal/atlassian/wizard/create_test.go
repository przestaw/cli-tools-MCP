package wizard

import (
	"encoding/json"
	"testing"

	"workctl/internal/atlassian/client"
)

func TestRequiredFields(t *testing.T) {
	str := &client.FieldSchema{Type: "string"}
	metas := []client.FieldMeta{
		{FieldID: "project", Name: "Project", Required: true},
		{FieldID: "issuetype", Name: "Issue Type", Required: true},
		{FieldID: "reporter", Name: "Reporter", Required: true},
		{FieldID: "parent", Name: "Parent", Required: true},
		{FieldID: "summary", Name: "Summary", Required: false, Schema: str},                               // forced in
		{FieldID: "priority", Name: "Priority", Required: true, Schema: str},                              // required
		{FieldID: "withdefault", Name: "Has Default", Required: true, HasDefaultValue: true, Schema: str}, // server default -> skip
		{FieldID: "optional", Name: "Optional", Required: false, Schema: str},                             // optional -> skip
	}
	var ids []string
	for _, m := range requiredFields(metas) {
		ids = append(ids, m.IDKey())
	}
	want := []string{"summary", "priority"}
	if len(ids) != len(want) {
		t.Fatalf("ids = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("ids = %v, want %v", ids, want)
		}
	}
}

func TestFindMeta(t *testing.T) {
	metas := []client.FieldMeta{
		{FieldID: "description", Name: "Description"},
		{FieldID: "assignee", Name: "Assignee"},
		{FieldID: "customfield_123", Name: "Tester"},
	}
	if m := findMeta(metas, "assignee", ""); m == nil || m.IDKey() != "assignee" {
		t.Fatalf("by id = %v", m)
	}
	// custom fields are matched by display name, case-insensitively
	if m := findMeta(metas, "", "tester"); m == nil || m.IDKey() != "customfield_123" {
		t.Fatalf("by name = %v", m)
	}
	if m := findMeta(metas, "nope", "ghost"); m != nil {
		t.Fatalf("missing should be nil, got %v", m)
	}
}

func TestPickProjectSprint(t *testing.T) {
	// a lone active sprint is used as-is, whatever its name
	if s := pickProjectSprint([]client.Sprint{{ID: 1, Name: "Alpha Sprint 16"}}, "AB"); s == nil || s.ID != 1 {
		t.Fatalf("single = %v", s)
	}
	// parallel sprints on one board: the project-named one wins (the real AB bug)
	multi := []client.Sprint{
		{ID: 8899, Name: "Alpha Sprint 16"},
		{ID: 8905, Name: "AB Sprint 28"},
	}
	if s := pickProjectSprint(multi, "AB"); s == nil || s.ID != 8905 {
		t.Fatalf("multi = %v", s)
	}
	// parallel sprints, none for the project: do not guess
	none := []client.Sprint{
		{ID: 1, Name: "Alpha Sprint 16"},
		{ID: 2, Name: "LEGACY Sprint 4"},
	}
	if s := pickProjectSprint(none, "AB"); s != nil {
		t.Fatalf("no-match should be nil, got %v", s)
	}
}

func TestSprintMatchesProject(t *testing.T) {
	if !sprintMatchesProject("AB Sprint 28", "AB") {
		t.Error("AB Sprint 28 should match AB")
	}
	if !sprintMatchesProject("ab sprint 5", "AB") {
		t.Error("match must be case-insensitive")
	}
	if sprintMatchesProject("Alpha Sprint 16", "AB") {
		t.Error("Alpha Sprint 16 must not match AB")
	}
	if sprintMatchesProject("ABX Sprint 1", "AB") {
		t.Error("ABX must not match AB (first token, not prefix)")
	}
}

func TestIsUserField(t *testing.T) {
	if !isUserField(&client.FieldSchema{Type: "user"}) {
		t.Error("single user picker")
	}
	if !isUserField(&client.FieldSchema{Type: "array", Items: "user"}) {
		t.Error("multi user picker")
	}
	if isUserField(&client.FieldSchema{Type: "array", Items: "option"}) {
		t.Error("option array is not a user field")
	}
	if isUserField(nil) {
		t.Error("nil schema is not a user field")
	}
}

func TestUserLabel(t *testing.T) {
	if got := userLabel(client.User{DisplayName: "Bob", EmailAddress: "bob@example.com"}); got != "Bob <bob@example.com>" {
		t.Fatalf("with email = %q", got)
	}
	if got := userLabel(client.User{DisplayName: "Bob"}); got != "Bob" {
		t.Fatalf("name only = %q", got)
	}
	if got := userLabel(client.User{AccountID: "acc-1"}); got != "acc-1" {
		t.Fatalf("id fallback = %q", got)
	}
}

func TestAllowedOptionLabels(t *testing.T) {
	if got := allowedOptionLabels(&client.FieldMeta{
		AllowedValues: json.RawMessage(`[{"value":"High"},{"value":"Low"}]`),
	}); len(got) != 2 || got[0] != "High" || got[1] != "Low" {
		t.Fatalf("value labels = %v", got)
	}
	// falls back to "name" when there is no "value" attribute (components, ...)
	if got := allowedOptionLabels(&client.FieldMeta{
		AllowedValues: json.RawMessage(`[{"name":"Backend"},{"name":"Frontend"}]`),
	}); len(got) != 2 || got[0] != "Backend" {
		t.Fatalf("name labels = %v", got)
	}
	if got := allowedOptionLabels(&client.FieldMeta{}); got != nil {
		t.Fatalf("no allowedValues should give nil, got %v", got)
	}
}
