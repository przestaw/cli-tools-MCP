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

// callJiraTool runs one jira_* MCP tool. The client is made per call, so a missing login fails only these tools.
func callJiraTool(ctx context.Context, cfg *Config, name string, a map[string]any) ([]byte, error) {
	c, err := newKindClient(cfg, "jira")
	if err != nil {
		return nil, err
	}
	key := argString(a, "key")
	switch name {
	case "jira_get_issue":
		return jiraGetIssue(ctx, c, key, nil)
	case "jira_search":
		return jiraSearch(ctx, c, argString(a, "jql"), argInt(a, "start_at", 0), mustPositive(argInt(a, "max_results", 50), 50))
	case "jira_get_comments":
		return jiraComments(ctx, c, key)
	case "jira_get_transitions":
		return jiraTransitions(ctx, c, key)
	case "jira_add_comment":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		return jiraAddComment(ctx, c, key, argString(a, "body"))
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

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
