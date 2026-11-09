// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"strings"
	"testing"
)

const twoBuilds = `{"name":"batch","arguments":{"calls":[
	{"tool":"request","args":{"profile":"jenkins","method":"POST","path":"/job/app/build"}},
	{"tool":"request","args":{"profile":"jenkins","method":"POST","path":"/job/e2e/build"}}]}}`

func TestBatchAsksOnceForAllWrites(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	text, asked := callOverMCP(t, cfg, `{"action":"decline"}`, twoBuilds)
	if !strings.Contains(text, "declined") || len(*seen) != 0 || !strings.Contains(asked, "2 writes") ||
		!strings.Contains(asked, "/job/app/build") || !strings.Contains(asked, "/job/e2e/build") {
		t.Fatalf("declined: text=%q asked=%q requests=%v", text, asked, *seen)
	}
	text, _ = callOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`, twoBuilds)
	if len(*seen) != 2 || (*seen)[0].Path != "/job/app/build" || (*seen)[1].Path != "/job/e2e/build" || strings.Count(text, `"ok"`) != 2 {
		t.Fatalf("approved: text=%q requests=%v", text, *seen)
	}
}

func TestBatchRefusesBeforeAskingWhenACallIsInvalid(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	for _, call := range []string{
		`{"name":"batch","arguments":{"calls":[{"tool":"request","args":{"profile":"jenkins","path":"/job/app/api/json"}}]}}`,
		`{"name":"batch","arguments":{"calls":[{"tool":"batch","args":{}}]}}`,
		`{"name":"batch","arguments":{"calls":[]}}`,
	} {
		if text, asked := callOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`, call); asked != "" || len(*seen) != 0 || !strings.Contains(text, "calls") {
			t.Fatalf("%s: text=%q asked=%q requests=%v", call, text, asked, *seen)
		}
	}
}

func TestBatchSkipsTheRestAfterAFailure(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	call := `{"name":"batch","arguments":{"calls":[
		{"tool":"request","args":{"profile":"missing","method":"POST","path":"/job/app/build"}},
		{"tool":"request","args":{"profile":"jenkins","method":"POST","path":"/job/e2e/build"}}]}}`
	text, _ := callOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`, call)
	if len(*seen) != 0 || !strings.Contains(text, `"failed"`) || !strings.Contains(text, `"skipped"`) {
		t.Fatalf("text=%q requests=%v", text, *seen)
	}
}

func TestCancelledCallDoesNotWrite(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	text, asked := buildOverMCP(t, cfg, "cancel")
	if asked == "" || len(*seen) != 0 || !strings.Contains(text, "canceled") {
		t.Fatalf("text=%q asked=%q requests=%v", text, asked, *seen)
	}
}

func TestQuestionShortensByCharacters(t *testing.T) {
	got := describeCall("comment", map[string]any{"body": strings.Repeat("ż", 301)})
	if !strings.Contains(got, strings.Repeat("ż", 300)+"... (301 characters in total)") {
		t.Fatalf("question: %q", got)
	}
}
