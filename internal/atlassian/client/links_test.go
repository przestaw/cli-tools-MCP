package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// Jira Cloud displays the outward phrase on the issue sent as "inwardIssue"
// (payload field names are the reverse of the displayed direction). For
// "from -[outward]-> to" the from issue must therefore be the inwardIssue.
// Regression test for the inverted mapping that produced reversed links.
func TestCreateLinkDirectionMapping(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/rest/api/3/issueLink" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body struct {
			Type         struct{ Name string } `json:"type"`
			InwardIssue  struct{ Key string }  `json:"inwardIssue"`
			OutwardIssue struct{ Key string }  `json:"outwardIssue"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Type.Name != "Implementation" {
			t.Errorf("type = %q, want Implementation", body.Type.Name)
		}
		if body.InwardIssue.Key != "AB-431" {
			t.Errorf("inwardIssue = %q, want AB-431 (the from side carries the outward phrase)", body.InwardIssue.Key)
		}
		if body.OutwardIssue.Key != "WEBAPP-76" {
			t.Errorf("outwardIssue = %q, want WEBAPP-76 (the to side carries the inward phrase)", body.OutwardIssue.Key)
		}
		w.WriteHeader(http.StatusCreated)
	})
	// "AB-431 implements WEBAPP-76": WEBAPP-76 must end up showing
	// inward "is implemented by" AB-431.
	if err := c.CreateLink(context.Background(), "Implementation", "AB-431", "WEBAPP-76"); err != nil {
		t.Fatalf("CreateLink: %v", err)
	}
}
