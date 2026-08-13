package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"workctl/internal/atlassian/client"
)

func testService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := client.New(client.Options{URL: srv.URL, Email: "a@b.c", Token: "t", VerifyTLS: true})
	return &Service{Client: c, BaseURL: srv.URL}
}

func meta(schema *client.FieldSchema, allowed string) *client.FieldMeta {
	m := &client.FieldMeta{Name: "F", Schema: schema}
	if allowed != "" {
		m.AllowedValues = json.RawMessage(allowed)
	}
	return m
}

func TestCoerceFieldValueScalars(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {})
	c := context.Background()

	v, err := s.CoerceFieldValue(c, meta(&client.FieldSchema{Type: "string"}, ""), "hello")
	if err != nil || v != "hello" {
		t.Fatalf("string = %v, %v", v, err)
	}
	v, err = s.CoerceFieldValue(c, meta(&client.FieldSchema{Type: "number"}, ""), "3.5")
	if err != nil || v.(float64) != 3.5 {
		t.Fatalf("number = %v, %v", v, err)
	}
	if _, err = s.CoerceFieldValue(c, meta(&client.FieldSchema{Type: "number"}, ""), "NaNope"); err == nil {
		t.Fatal("bad number should error")
	}
}

func TestCoerceFieldValueRichText(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {})
	v, err := s.CoerceFieldValue(context.Background(),
		meta(&client.FieldSchema{Type: "string", System: "description"}, ""), "text")
	if err != nil {
		t.Fatal(err)
	}
	doc, ok := v.(map[string]any)
	if !ok || doc["type"] != "doc" {
		t.Fatalf("description should be ADF, got %v", v)
	}
	v, _ = s.CoerceFieldValue(context.Background(),
		meta(&client.FieldSchema{Type: "string", Custom: "com.atlassian.jira.plugin.system.customfieldtypes:textarea"}, ""), "t")
	if doc, ok := v.(map[string]any); !ok || doc["type"] != "doc" {
		t.Fatalf("textarea should be ADF, got %v", v)
	}
}

func TestCoerceFieldValueOptionMatching(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {})
	m := meta(&client.FieldSchema{Type: "option"}, `[{"value":"High"},{"value":"Low"}]`)
	v, err := s.CoerceFieldValue(context.Background(), m, "high")
	if err != nil {
		t.Fatal(err)
	}
	if v.(map[string]any)["value"] != "High" {
		t.Fatalf("option = %v", v)
	}
	if _, err := s.CoerceFieldValue(context.Background(), m, "Medium"); err == nil ||
		!strings.Contains(err.Error(), "High, Low") {
		t.Fatalf("want option-list error, got %v", err)
	}
}

func TestCoerceFieldValueArrays(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {})
	c := context.Background()
	v, err := s.CoerceFieldValue(c, meta(&client.FieldSchema{Type: "array", Items: "string"}, ""), "a, b")
	if err != nil {
		t.Fatal(err)
	}
	if arr := v.([]string); len(arr) != 2 || arr[0] != "a" || arr[1] != "b" {
		t.Fatalf("labels = %v", v)
	}
	v, err = s.CoerceFieldValue(c, meta(&client.FieldSchema{Type: "array", Items: "version"}, ""), "1.0,2.0")
	if err != nil {
		t.Fatal(err)
	}
	arr := v.([]any)
	if len(arr) != 2 || arr[0].(map[string]any)["name"] != "1.0" {
		t.Fatalf("versions = %v", v)
	}
}

func TestResolveAccountID(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/3/myself":
			w.Write([]byte(`{"accountId":"me-1","displayName":"Me"}`))
		case "/rest/api/3/user/search":
			switch r.URL.Query().Get("query") {
			case "bob":
				w.Write([]byte(`[{"accountId":"u-1","displayName":"Bob","active":true},{"accountId":"u-2","displayName":"Bob Old","active":false}]`))
			case "ambiguous":
				w.Write([]byte(`[{"accountId":"u-1","active":true,"displayName":"A"},{"accountId":"u-2","active":true,"displayName":"B"}]`))
			default:
				w.Write([]byte(`[]`))
			}
		}
	})
	c := context.Background()
	if acc, _ := s.ResolveAccountID(c, "me"); acc != "me-1" {
		t.Fatalf("me = %s", acc)
	}
	if acc, _ := s.ResolveAccountID(c, "557058:abc"); acc != "557058:abc" {
		t.Fatalf("accountId passthrough = %s", acc)
	}
	if acc, err := s.ResolveAccountID(c, "bob"); err != nil || acc != "u-1" {
		t.Fatalf("single active = %s, %v", acc, err)
	}
	if _, err := s.ResolveAccountID(c, "ambiguous"); err == nil {
		t.Fatal("ambiguous should error")
	}
	if _, err := s.ResolveAccountID(c, "ghost"); err == nil {
		t.Fatal("no match should error")
	}
}

func TestResolveTransitionID(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"transitions":[
		  {"id":"11","name":"Start Progress","to":{"name":"In Progress"}},
		  {"id":"21","name":"Close","to":{"name":"Done"}}]}`))
	})
	c := context.Background()
	if id, _, _ := s.ResolveTransitionID(c, "A-1", "close"); id != "21" {
		t.Fatalf("by name = %s", id)
	}
	if id, _, _ := s.ResolveTransitionID(c, "A-1", "In Progress"); id != "11" {
		t.Fatalf("by target status = %s", id)
	}
	if id, _, _ := s.ResolveTransitionID(c, "A-1", "11"); id != "11" {
		t.Fatalf("by id = %s", id)
	}
	if _, _, err := s.ResolveTransitionID(c, "A-1", "Reopen"); err == nil ||
		!strings.Contains(err.Error(), "In Progress, Done") {
		t.Fatalf("want available list, got %v", err)
	}
}

func TestBuildEditFieldsResolvesNames(t *testing.T) {
	s := testService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/rest/api/3/issue/A-1/editmeta" {
			w.Write([]byte(`{"fields":{
			  "summary":{"name":"Summary","key":"summary","schema":{"type":"string"}},
			  "customfield_10010":{"name":"Story Points","key":"customfield_10010","schema":{"type":"number"}}}}`))
		}
	})
	fields, err := s.BuildEditFields(context.Background(), "A-1",
		[]FieldSpec{{Key: "story points", Value: "8"}, {Key: "summary", Value: "S"}})
	if err != nil {
		t.Fatal(err)
	}
	if fields["customfield_10010"].(float64) != 8 || fields["summary"] != "S" {
		t.Fatalf("fields = %v", fields)
	}
	_, err = s.BuildEditFields(context.Background(), "A-1", []FieldSpec{{Key: "Ghost", Value: "x"}})
	if err == nil || !strings.Contains(err.Error(), "available") {
		t.Fatalf("want not-found error, got %v", err)
	}
}
