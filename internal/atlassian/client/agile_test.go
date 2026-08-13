package client

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAddIssuesToSprintBody(t *testing.T) {
	var body map[string]any
	var path, method string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		path, method = r.URL.Path, r.Method
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusNoContent)
	})
	if err := c.AddIssuesToSprint(context.Background(), 42, "AB-1", "AB-2"); err != nil {
		t.Fatal(err)
	}
	if method != http.MethodPost || path != "/rest/agile/1.0/sprint/42/issue" {
		t.Fatalf("%s %s", method, path)
	}
	issues, ok := body["issues"].([]any)
	if !ok || len(issues) != 2 || issues[0] != "AB-1" || issues[1] != "AB-2" {
		t.Fatalf("issues body = %v", body["issues"])
	}
}
