// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestBatchSessionApprovalIncludesWriteSummary(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	cfg.Policy.ConfirmWrites, cfg.Policy.ConfirmSessionWrites = false, true
	text, asked := callOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`, twoBuilds)
	if len(*seen) != 2 || strings.Count(asked, "Approve this session?") != 1 ||
		!strings.Contains(asked, "later writes to any configured profile in this MCP connection") ||
		!strings.Contains(asked, "/job/app/build") || !strings.Contains(asked, "/job/e2e/build") || strings.Count(text, `"ok"`) != 2 {
		t.Fatalf("text=%q asked=%q requests=%v", text, asked, *seen)
	}
}

func TestBatchDescriptionsExplainCallsWithoutChangingRequests(t *testing.T) {
	const call = `{"name":"batch","arguments":{"calls":[
		{"tool":"request","description":"Start application build","args":{"profile":"jenkins","method":"POST","path":"/job/app/build","body":"{\"parameter\":\"test\"}"}},
		{"tool":"request","description":"Start integration tests","args":{"profile":"jenkins","method":"POST","path":"/job/e2e/build"}}]}}`
	for _, mode := range []string{"per-write", "session"} {
		for _, approved := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/approved=%t", mode, approved), func(t *testing.T) {
				cfg, seen := jenkinsLike(t)
				cfg.Policy.ConfirmWrites = mode == "per-write"
				cfg.Policy.ConfirmSessionWrites = mode == "session"
				answer := fmt.Sprintf(`{"action":"accept","content":{"approve":%t}}`, approved)
				text, asked := callOverMCP(t, cfg, answer, call)
				for _, description := range []string{"Start application build", "Start integration tests"} {
					if strings.Count(asked, description) != 1 {
						t.Fatalf("description missing or repeated: %q", asked)
					}
				}
				if !strings.Contains(asked, "1. Start application build\n   request ") ||
					!strings.Contains(asked, "2. Start integration tests\n   request ") ||
					!strings.Contains(asked, `"method":"POST"`) || !strings.Contains(asked, `"/job/app/build"`) ||
					!strings.Contains(asked, `"/job/e2e/build"`) || !strings.Contains(asked, `\"parameter\":\"test\"`) {
					t.Fatalf("descriptions replaced request details: %q", asked)
				}
				if !approved {
					if len(*seen) != 0 || !strings.Contains(text, "declined") {
						t.Fatalf("declined: text=%q requests=%v", text, *seen)
					}
					return
				}
				if len(*seen) != 2 || (*seen)[0].Method != "POST" || (*seen)[0].Path != "/job/app/build" ||
					(*seen)[0].Body != `{"parameter":"test"}` || (*seen)[1].Path != "/job/e2e/build" || (*seen)[1].Body != "" {
					t.Fatalf("description altered provider requests: %v", *seen)
				}
			})
		}
	}
}

func TestBatchDescriptionIsOneBoundedLine(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	call := strings.Replace(twoBuilds, `"tool":"request"`,
		`"description":"  First\n\tstep\r\n`+strings.Repeat("ż", 121)+`  ","tool":"request"`, 1)
	_, asked := callOverMCP(t, cfg, `{"action":"decline"}`, call)
	if !strings.Contains(asked, "1. First step "+strings.Repeat("ż", 109)+"... (132 characters in total)\n   request ") ||
		len(*seen) != 0 {
		t.Fatalf("asked=%q requests=%v", asked, *seen)
	}
}

func TestBatchRefusesBeforeAskingWhenACallIsInvalid(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	for _, call := range []string{
		`{"name":"batch","arguments":{"calls":[{"tool":"request","args":{"profile":"jenkins","path":"/job/app/api/json"}}]}}`,
		`{"name":"batch","arguments":{"calls":[{"tool":"batch","args":{}}]}}`,
		`{"name":"batch","arguments":{"calls":[]}}`,
		`{"name":"batch","arguments":{"calls":[{"tool":"request","args":{"profile":"jenkins","method":"POST","path":"/job/app/build"}},{"tool":"request","args":{"profile":"missing","method":"POST","path":"/job/e2e/build"}}]}}`,
		`{"name":"batch","arguments":{"calls":[{"tool":"request","args":{"profile":"jenkins","method":"POST","path":"/job/app/build"}},{"tool":"request","args":{"profile":"jenkins","method":"POST"}}]}}`,
		`{"name":"batch","arguments":{"calls":[{"tool":"request","args":{"profile":"jenkins","method":"POST","path":"/job/app/build"}},{"tool":"request","args":{"profile":42,"method":"POST","path":"/job/e2e/build"}}]}}`,
		strings.Replace(twoBuilds, `"tool":"request"`, `"description":42,"tool":"request"`, 1),
		strings.Replace(twoBuilds, `"tool":"request"`, `"description":"Start a build","tool":"unknown"`, 1),
	} {
		if text, asked := callOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`, call); asked != "" || len(*seen) != 0 || !strings.Contains(text, "calls") {
			t.Fatalf("%s: text=%q asked=%q requests=%v", call, text, asked, *seen)
		}
	}
}

func TestBatchSkipsTheRestAfterAFailure(t *testing.T) {
	cfg, _ := jenkinsLike(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	profile := cfg.Profiles["jenkins"]
	profile.URL = server.URL
	cfg.Profiles["jenkins"] = profile
	text, _ := callOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`, twoBuilds)
	if requests != 1 || !strings.Contains(text, `"failed"`) || !strings.Contains(text, `"skipped"`) {
		t.Fatalf("text=%q requests=%d", text, requests)
	}
}

func TestBatchConfirmationIsOptional(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	cfg.Policy.ConfirmWrites = false
	text, asked := callOverMCP(t, cfg, `{"action":"decline"}`, twoBuilds)
	if asked != "" || len(*seen) != 2 || strings.Count(text, `"ok"`) != 2 {
		t.Fatalf("text=%q asked=%q requests=%v", text, asked, *seen)
	}
}

func TestConfirmationCanBeRequestedForOneOperation(t *testing.T) {
	cfg, seen := jenkinsLike(t)
	cfg.Policy.ConfirmWrites = false
	for _, call := range []string{
		`{"name":"request","arguments":{"profile":"jenkins","method":"POST","path":"/job/app/build","confirm":true}}`,
		strings.Replace(twoBuilds, `"calls":`, `"confirm":true,"calls":`, 1),
		strings.Replace(twoBuilds, `"method":"POST"`, `"confirm":true,"method":"POST"`, 1),
	} {
		text, asked := callOverMCP(t, cfg, `{"action":"decline"}`, call)
		if asked == "" || len(*seen) != 0 || !strings.Contains(text, "declined") {
			t.Fatalf("text=%q asked=%q requests=%v", text, asked, *seen)
		}
	}
	cfg.Policy.ConfirmWrites = true
	call := strings.Replace(twoBuilds, `"calls":`, `"confirm":false,"calls":`, 1)
	text, asked := callOverMCP(t, cfg, `{"action":"decline"}`, call)
	if asked == "" || len(*seen) != 0 || !strings.Contains(text, "declined") {
		t.Fatalf("policy was overridden: text=%q asked=%q requests=%v", text, asked, *seen)
	}
}

func TestBatchContinuesIndependentStops(t *testing.T) {
	cfg, _ := jenkinsLike(t)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	profile := cfg.Profiles["jenkins"]
	profile.URL = server.URL
	cfg.Profiles["jenkins"] = profile
	call := strings.Replace(twoBuilds, `"calls":`, `"stop_on_error":false,"calls":`, 1)
	text, asked := callOverMCP(t, cfg, `{"action":"accept","content":{"approve":true}}`, call)
	if requests != 2 || asked == "" || !strings.Contains(text, `"failed"`) || !strings.Contains(text, `"ok"`) {
		t.Fatalf("text=%q asked=%q requests=%d", text, asked, requests)
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
