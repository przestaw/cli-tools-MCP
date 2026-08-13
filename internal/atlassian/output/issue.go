package output

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"workctl/internal/atlassian/client"
)

// Issue prints a Jira issue as a FIELD / VALUE table. With Expand on, the
// Description is rendered as a text block below the table.
func (p *Printer) Issue(is *client.Issue, baseURL string) error {
	f := is.Fields
	var rows [][]string
	add := func(k, v string) {
		if v == "" {
			return
		}
		rows = append(rows, []string{k, v})
	}
	add("Key", is.Key)
	add("Summary", f.Summary)
	add("Type", namedName(f.IssueType))
	add("Status", namedName(f.Status))
	add("Resolution", namedName(f.Resolution))
	add("Priority", namedName(f.Priority))
	add("Assignee", UserName(f.Assignee))
	add("Reporter", UserName(f.Reporter))
	add("Labels", strings.Join(f.Labels, ", "))
	add("Components", namedList(f.Components))
	add("FixVersions", namedList(f.FixVersions))
	if f.Parent != nil {
		add("Parent", issueRefStr(f.Parent))
	}
	if len(f.Subtasks) > 0 {
		add("Subtasks", strconv.Itoa(len(f.Subtasks))+" (see `atlassian issue subtasks`)")
	}
	if len(f.IssueLinks) > 0 {
		add("Links", strconv.Itoa(len(f.IssueLinks))+" (see `atlassian link list`)")
	}
	if len(f.Attachment) > 0 {
		add("Attachments", strconv.Itoa(len(f.Attachment)))
	}
	add("DueDate", f.DueDate)
	add("Created", f.Created)
	add("Updated", f.Updated)

	desc := client.ADFToText(f.Description)
	if p.Expand && desc != "" {
		add("Description", "(see below)")
	} else {
		add("Description", desc)
	}

	// Custom fields with non-null values, labelled via the names expand.
	for id, raw := range sortedCustom(f.Custom) {
		if isNullish(raw) {
			continue
		}
		name := is.Names[id]
		if name == "" {
			name = id
		}
		add(name+"  "+p.dim("("+id+")"), formatRaw(raw))
	}

	if err := p.Table([]string{"Field", "Value"}, rows); err != nil {
		return err
	}
	if p.Expand && desc != "" {
		fmt.Fprintf(p.out(), "\n%s\n%s\n", p.bold("Description:"), desc)
	}
	if baseURL != "" && !p.JSON {
		fmt.Fprintf(p.out(), "\n%s\n", p.dim(baseURL+"/browse/"+is.Key))
	}
	return nil
}

// IssueTable renders the standard issue list table.
func (p *Printer) IssueTable(issues []client.Issue) error {
	rows := make([][]string, len(issues))
	for i, is := range issues {
		rows[i] = []string{
			is.Key,
			is.Fields.Summary,
			namedName(is.Fields.Status),
			namedName(is.Fields.IssueType),
			UserName(is.Fields.Assignee),
			shortDate(is.Fields.Updated),
		}
	}
	return p.Table([]string{"Key", "Summary", "Status", "Type", "Assignee", "Updated"}, rows)
}

// UserName renders a user reference for tables.
func UserName(u *client.User) string {
	if u == nil {
		return ""
	}
	return u.DisplayName
}

func namedName(n *client.Named) string {
	if n == nil {
		return ""
	}
	return n.Name
}

func namedList(ns []client.Named) string {
	parts := make([]string, 0, len(ns))
	for _, n := range ns {
		parts = append(parts, n.Name)
	}
	return strings.Join(parts, ", ")
}

func issueRefStr(r *client.IssueRef) string {
	if r == nil {
		return ""
	}
	if r.Fields != nil && r.Fields.Summary != "" {
		return r.Key + " (" + r.Fields.Summary + ")"
	}
	return r.Key
}

// shortDate trims a Jira ISO timestamp to the date part.
func shortDate(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

// sortedCustom yields custom fields in stable (sorted-id) order.
func sortedCustom(m map[string]json.RawMessage) map[string]json.RawMessage {
	// Map iteration order is random; build an ordered view via sorted keys.
	// (Small maps - simple insertion sort keeps deps minimal.)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	out := make(map[string]json.RawMessage, len(m))
	for _, k := range keys {
		out[k] = m[k]
	}
	return out
}

func isNullish(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s == "" || s == "null" || s == "[]" || s == "{}"
}

// formatRaw renders an arbitrary JSON field value readably.
func formatRaw(raw json.RawMessage) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return strings.TrimSpace(string(raw))
	}
	return formatAny(v)
}

func formatAny(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return fmt.Sprintf("%v", t)
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		parts := make([]string, 0, len(t))
		for _, e := range t {
			parts = append(parts, formatAny(e))
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		// ADF documents render as text.
		if t["type"] == "doc" {
			b, _ := json.Marshal(t)
			return client.ADFToText(b)
		}
		for _, k := range []string{"displayName", "name", "value", "label"} {
			if s, ok := t[k]; ok {
				return formatAny(s)
			}
		}
		if id, ok := t["id"]; ok {
			return formatAny(id)
		}
		b, _ := json.Marshal(t)
		return string(b)
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}
