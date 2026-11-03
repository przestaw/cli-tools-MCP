// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package client

import (
	"encoding/json"
	"testing"
)

func TestIssueFieldsCollectsCustomFields(t *testing.T) {
	body := `{
	  "summary":"S",
	  "status":{"name":"Open"},
	  "customfield_10010":"epic-x",
	  "customfield_10020":[{"name":"Sprint 1"}],
	  "customfield_10030":null
	}`
	var f IssueFields
	if err := json.Unmarshal([]byte(body), &f); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if f.Summary != "S" || f.Status.Name != "Open" {
		t.Fatalf("known fields lost: %+v", f)
	}
	if len(f.Custom) != 3 {
		t.Fatalf("custom count = %d", len(f.Custom))
	}
	if string(f.Custom["customfield_10010"]) != `"epic-x"` {
		t.Fatalf("custom value = %s", f.Custom["customfield_10010"])
	}
}

func TestAttachmentIDAcceptsStringAndNumber(t *testing.T) {
	// fields.attachment[] form
	var a Attachment
	if err := json.Unmarshal([]byte(`{"id":"63790","filename":"a.png","size":8027}`), &a); err != nil {
		t.Fatalf("string id: %v", err)
	}
	if a.ID != "63790" || a.Filename != "a.png" || a.Size != 8027 {
		t.Fatalf("string id decoded wrong: %+v", a)
	}
	// GET /rest/api/3/attachment/{id} form
	var b Attachment
	if err := json.Unmarshal([]byte(`{"id":63790,"filename":"b.png","size":1}`), &b); err != nil {
		t.Fatalf("number id: %v", err)
	}
	if b.ID != "63790" || b.Filename != "b.png" {
		t.Fatalf("number id decoded wrong: %+v", b)
	}
	// absent / null must not leak the literal "null"
	var c Attachment
	if err := json.Unmarshal([]byte(`{"id":null,"filename":"c.png"}`), &c); err != nil {
		t.Fatalf("null id: %v", err)
	}
	if c.ID != "" {
		t.Fatalf("null id = %q, want empty", c.ID)
	}
}

func TestFieldMetaIDKey(t *testing.T) {
	if (FieldMeta{FieldID: "customfield_1"}).IDKey() != "customfield_1" {
		t.Fatal("createmeta id")
	}
	if (FieldMeta{Key: "summary"}).IDKey() != "summary" {
		t.Fatal("editmeta key")
	}
}
