package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func jiraGetIssue(ctx context.Context, c *APIClient, key string, fields []string) ([]byte, error) {
	q := url.Values{}
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	return bodyOnly(c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key), q, nil, ""))
}

func jiraSearch(ctx context.Context, c *APIClient, jql string, startAt, maxResults int) ([]byte, error) {
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", strconv.Itoa(startAt))
	q.Set("maxResults", strconv.Itoa(maxResults))
	return bodyOnly(c.do(ctx, http.MethodGet, "/rest/api/2/search", q, nil, ""))
}

func jiraComments(ctx context.Context, c *APIClient, key string) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", nil, nil, ""))
}

func jiraTransitions(ctx context.Context, c *APIClient, key string) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions?expand=transitions.fields", nil, nil, ""))
}

func jiraMyself(ctx context.Context, c *APIClient) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, "/rest/api/2/myself", nil, nil, ""))
}

func jiraServerInfo(ctx context.Context, c *APIClient) ([]byte, error) {
	return bodyOnly(c.do(ctx, http.MethodGet, "/rest/api/2/serverInfo", nil, nil, ""))
}

func jiraAddComment(ctx context.Context, c *APIClient, key, text string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{"body": text})
	return bodyOnly(c.do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", nil, payload, "application/json"))
}

func jiraDoTransition(ctx context.Context, c *APIClient, key, id string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"transition": map[string]string{"id": id}})
	b, _, err := c.do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, payload, "application/json")
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return []byte("{\"ok\":true}\n"), nil
	}
	return b, nil
}

func bodyOnly(b []byte, _ string, err error) ([]byte, error) { return b, err }

func parseCommaList(s string) []string {
	var out []string
	for _, x := range strings.Split(s, ",") {
		x = strings.TrimSpace(x)
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func mustPositive(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

func requireWrite(cfg *Config) error {
	if !cfg.Policy.AllowWrite {
		return fmt.Errorf("write operation blocked by policy; set policy.allow_write=true in %s", cfg.path)
	}
	return nil
}
