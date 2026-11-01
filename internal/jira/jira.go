// Package jira is the thin Jira Server provider: REST calls, the `workctl jira ...`
// commands and the jira_* MCP tools.
package jira

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"workctl/internal/api"
	"workctl/internal/cliutil"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

// GetIssue fetches one issue; the task context uses it to show the issue next to its branches.
func GetIssue(ctx context.Context, c *api.Client, key string, fields []string) ([]byte, error) {
	q := url.Values{}
	if len(fields) > 0 {
		q.Set("fields", strings.Join(fields, ","))
	}
	return api.BodyOnly(c.Do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key), q, nil, ""))
}

func search(ctx context.Context, c *api.Client, jql string, startAt, maxResults int) ([]byte, error) {
	q := url.Values{}
	q.Set("jql", jql)
	q.Set("startAt", strconv.Itoa(startAt))
	q.Set("maxResults", strconv.Itoa(maxResults))
	return api.BodyOnly(c.Do(ctx, http.MethodGet, "/rest/api/2/search", q, nil, ""))
}

func comments(ctx context.Context, c *api.Client, key string) ([]byte, error) {
	return api.BodyOnly(c.Do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", nil, nil, ""))
}

func transitions(ctx context.Context, c *api.Client, key string) ([]byte, error) {
	return api.BodyOnly(c.Do(ctx, http.MethodGet, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions?expand=transitions.fields", nil, nil, ""))
}

// Myself fetches the authenticated user; doctor and the setup wizard use it as the connectivity check.
func Myself(ctx context.Context, c *api.Client) ([]byte, error) {
	return api.BodyOnly(c.Do(ctx, http.MethodGet, "/rest/api/2/myself", nil, nil, ""))
}

func serverInfo(ctx context.Context, c *api.Client) ([]byte, error) {
	return api.BodyOnly(c.Do(ctx, http.MethodGet, "/rest/api/2/serverInfo", nil, nil, ""))
}

func addComment(ctx context.Context, c *api.Client, key, text string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]string{"body": text})
	return api.BodyOnly(c.Do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/comment", nil, payload, "application/json"))
}

func doTransition(ctx context.Context, c *api.Client, key, id string) ([]byte, error) {
	payload, _ := json.Marshal(map[string]any{"transition": map[string]string{"id": id}})
	b, _, err := c.Do(ctx, http.MethodPost, "/rest/api/2/issue/"+url.PathEscape(key)+"/transitions", nil, payload, "application/json")
	if err != nil {
		return nil, err
	}
	if len(b) == 0 {
		return []byte("{\"ok\":true}\n"), nil
	}
	return b, nil
}

// Command runs `workctl jira ...`.
func Command(cfg *config.Config, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("jira commands: issue, search, comments, transitions, transition, comment, myself, server-info, raw")
	}
	c, err := api.NewKindClient(cfg, "jira")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	switch args[0] {
	case "issue":
		fs := flag.NewFlagSet("jira issue", flag.ContinueOnError)
		fields := fs.String("fields", "", "comma-separated fields")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: jira issue [--fields a,b] KEY")
		}
		b, err := GetIssue(ctx, c, fs.Arg(0), cliutil.SplitCommaList(*fields))
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "search":
		fs := flag.NewFlagSet("jira search", flag.ContinueOnError)
		jql := fs.String("jql", "", "JQL")
		start := fs.Int("start", 0, "startAt")
		max := fs.Int("max", 50, "maxResults")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *jql == "" {
			return fmt.Errorf("--jql is required")
		}
		b, err := search(ctx, c, *jql, *start, *max)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "comments":
		if len(args) != 2 {
			return fmt.Errorf("usage: jira comments KEY")
		}
		b, err := comments(ctx, c, args[1])
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "transitions":
		if len(args) != 2 {
			return fmt.Errorf("usage: jira transitions KEY")
		}
		b, err := transitions(ctx, c, args[1])
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "transition":
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
		fs := flag.NewFlagSet("jira transition", flag.ContinueOnError)
		id := fs.String("id", "", "transition id")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *id == "" {
			return fmt.Errorf("usage: jira transition --id ID KEY")
		}
		b, err := doTransition(ctx, c, fs.Arg(0), *id)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "comment":
		if len(args) < 2 || args[1] != "add" {
			return fmt.Errorf("usage: jira comment add --body TEXT KEY")
		}
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
		fs := flag.NewFlagSet("jira comment add", flag.ContinueOnError)
		body := fs.String("body", "", "comment body")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if fs.NArg() != 1 || *body == "" {
			return fmt.Errorf("usage: jira comment add --body TEXT KEY")
		}
		b, err := addComment(ctx, c, fs.Arg(0), *body)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "myself":
		b, err := Myself(ctx, c)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "server-info":
		b, err := serverInfo(ctx, c)
		if err != nil {
			return err
		}
		return cliutil.PrintBody(b)
	case "raw":
		return api.RawCommand(ctx, c, cfg, args[1:])
	default:
		return fmt.Errorf("unknown jira command %q", args[0])
	}
}

// Tools are the MCP tools of the Jira module; write tools exist only with policy.allow_write.
func Tools(cfg *config.Config) []mcp.Tool {
	tools := []mcp.Tool{
		{Name: "jira_get_issue", Description: "Get a Jira issue by key.", InputSchema: mcp.ObjSchema(map[string]any{"key": mcp.StrProp("Issue key, e.g. DEMO-123")}, "key")},
		{Name: "jira_search", Description: "Run a JQL query.", InputSchema: mcp.ObjSchema(map[string]any{"jql": mcp.StrProp("JQL query"), "start_at": mcp.IntProp("Start index"), "max_results": mcp.IntProp("Maximum results")}, "jql")},
		{Name: "jira_get_comments", Description: "Get comments for a Jira issue.", InputSchema: mcp.ObjSchema(map[string]any{"key": mcp.StrProp("Issue key")}, "key")},
		{Name: "jira_get_transitions", Description: "Get available Jira issue transitions.", InputSchema: mcp.ObjSchema(map[string]any{"key": mcp.StrProp("Issue key")}, "key")},
	}
	if cfg.Policy.AllowWrite {
		tools = append(tools,
			mcp.Tool{Name: "jira_add_comment", Description: "Add a Jira comment. Enabled only when policy.allow_write=true.", InputSchema: mcp.ObjSchema(map[string]any{"key": mcp.StrProp("Issue key"), "body": mcp.StrProp("Comment body")}, "key", "body")},
		)
	}
	return tools
}

// CallTool runs one jira_* MCP tool. The client is made per call, so a missing login fails only these tools.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	c, err := api.NewKindClient(cfg, "jira")
	if err != nil {
		return nil, err
	}
	key := mcp.ArgString(a, "key")
	switch name {
	case "jira_get_issue":
		return GetIssue(ctx, c, key, nil)
	case "jira_search":
		return search(ctx, c, mcp.ArgString(a, "jql"), mcp.ArgInt(a, "start_at", 0), cliutil.MustPositive(mcp.ArgInt(a, "max_results", 50), 50))
	case "jira_get_comments":
		return comments(ctx, c, key)
	case "jira_get_transitions":
		return transitions(ctx, c, key)
	case "jira_add_comment":
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
		return addComment(ctx, c, key, mcp.ArgString(a, "body"))
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}
