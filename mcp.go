package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"workctl/internal/render"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func objSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

// toolAvailable reports whether the backend of an MCP tool is configured (Jira and GitLab are each optional).
func toolAvailable(cfg *Config, name string) bool {
	switch {
	case name == "pr_list" || name == "pr_show":
		return cfg.has("github") || cfg.has("gitlab") || cfg.has("azure")
	case strings.HasPrefix(name, "jira_"):
		return cfg.has("jira")
	case strings.HasPrefix(name, "gitlab_"):
		return cfg.has("gitlab")
	case strings.HasPrefix(name, "github_"):
		return cfg.has("github")
	case strings.HasPrefix(name, "task_"):
		return cfg.has("jira") && cfg.has("gitlab")
	}
	return true
}

func mcpTools(cfg *Config) []mcpTool {
	all := []mcpTool{
		{"jira_get_issue", "Get a Jira issue by key.", objSchema(map[string]any{"key": strProp("Issue key, e.g. DEMO-123")}, "key")},
		{"jira_search", "Run a JQL query.", objSchema(map[string]any{"jql": strProp("JQL query"), "start_at": intProp("Start index"), "max_results": intProp("Maximum results")}, "jql")},
		{"jira_get_comments", "Get comments for a Jira issue.", objSchema(map[string]any{"key": strProp("Issue key")}, "key")},
		{"jira_get_transitions", "Get available Jira issue transitions.", objSchema(map[string]any{"key": strProp("Issue key")}, "key")},
		{"gitlab_get_project", "Get GitLab project metadata.", objSchema(map[string]any{"project": strProp("Project path group/name; omit to use default")})},
		{"gitlab_get_merge_request", "Get a GitLab merge request.", objSchema(map[string]any{"project": strProp("Project path"), "iid": intProp("Merge request IID")}, "iid")},
		{"gitlab_get_merge_request_changes", "Get merge request diff/changes (compatible with older GitLab v4 APIs).", objSchema(map[string]any{"project": strProp("Project path"), "iid": intProp("Merge request IID")}, "iid")},
		{"gitlab_get_merge_request_notes", "Get merge request notes/comments.", objSchema(map[string]any{"project": strProp("Project path"), "iid": intProp("Merge request IID")}, "iid")},
		{"gitlab_list_merge_requests", "List/search merge requests in a project.", objSchema(map[string]any{"project": strProp("Project path"), "search": strProp("Search text"), "state": strProp("opened, closed, merged, all")})},
		{"gitlab_list_pipelines", "List pipelines, optionally for a ref.", objSchema(map[string]any{"project": strProp("Project path"), "ref": strProp("Branch/tag ref"), "per_page": intProp("Items per page")})},
		{"gitlab_get_pipeline", "Get one pipeline.", objSchema(map[string]any{"project": strProp("Project path"), "id": intProp("Pipeline ID")}, "id")},
		{"gitlab_get_pipeline_jobs", "Get jobs belonging to a pipeline.", objSchema(map[string]any{"project": strProp("Project path"), "id": intProp("Pipeline ID")}, "id")},
		{"gitlab_get_job_trace", "Get a GitLab CI job log/trace.", objSchema(map[string]any{"project": strProp("Project path"), "job_id": intProp("Job ID")}, "job_id")},
		{"gitlab_list_branches", "List/search branches in a project.", objSchema(map[string]any{"project": strProp("Project path"), "search": strProp("Branch search text")})},
		{"gitlab_get_file", "Get raw repository file contents at a ref.", objSchema(map[string]any{"project": strProp("Project path"), "path": strProp("Repository-relative file path"), "ref": strProp("Branch/tag/commit; default HEAD")}, "path")},
		{"task_get_context", "Get Jira issue plus matching branches and merge requests from configured GitLab projects.", objSchema(map[string]any{"key": strProp("Jira issue key")}, "key")},
	}
	if cfg.Policy.AllowWrite {
		all = append(all,
			mcpTool{"jira_add_comment", "Add a Jira comment. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"key": strProp("Issue key"), "body": strProp("Comment body")}, "key", "body")},
			mcpTool{"gitlab_add_merge_request_note", "Add a GitLab merge request note. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"project": strProp("Project path"), "iid": intProp("MR IID"), "body": strProp("Note body")}, "iid", "body")},
			mcpTool{"gitlab_create_merge_request", "Create a merge request. Enabled only when policy.allow_write=true.", objSchema(map[string]any{"project": strProp("Project path"), "source": strProp("Source branch"), "target": strProp("Target branch"), "title": strProp("Title"), "description": strProp("Description"), "draft": boolProp("Prefix title with Draft")}, "source", "target", "title")},
		)
	}
	tools := []mcpTool{} // never nil: the JSON must say [] and not null
	for _, t := range all {
		if toolAvailable(cfg, t.Name) {
			tools = append(tools, t)
		}
	}
	withProfileArg(tools, "gitlab_", "GitLab profile (account); omit to use defaults.gitlab")
	if cfg.has("github") {
		tools = append(tools, githubTools(cfg)...)
	}
	if toolAvailable(cfg, "pr_list") {
		tools = append(tools,
			mcpTool{"pr_list", "List pull requests or merge requests from GitHub, GitLab, or Azure DevOps using unified profiles.", objSchema(map[string]any{
				"profile": strProp("Profile name (optional if default profile exists)"),
				"repo":    strProp("Repository name or path (optional if default_repo is set)"),
				"project": strProp("Project name (required for Azure DevOps unless set in profile)"),
				"state":   strProp("Filter state: open, closed, merged, all"),
				"search":  strProp("Search text (title and description)"),
				"format":  strProp("Output format: json or text (default json)"),
			})},
			mcpTool{"pr_show", "Get details and description of a pull request or merge request.", objSchema(map[string]any{
				"id":      intProp("Pull request or merge request number"),
				"profile": strProp("Profile name (optional if default profile exists)"),
				"repo":    strProp("Repository name or path (optional if default_repo is set)"),
				"project": strProp("Project name (required for Azure DevOps unless set in profile)"),
				"format":  strProp("Output format: json or text (default json)"),
			}, "id")},
		)
	}
	return append(tools, moduleTools(cfg)...)
}

// withProfileArg adds the optional "profile" argument to the tools whose name starts with prefix.
func withProfileArg(tools []mcpTool, prefix, desc string) {
	for _, t := range tools {
		if strings.HasPrefix(t.Name, prefix) {
			t.InputSchema["properties"].(map[string]any)["profile"] = strProp(desc)
		}
	}
}

// scopedConfig narrows cfg to the profile named by the call's "profile" argument; without one it returns cfg itself.
func scopedConfig(cfg *Config, a map[string]any, kind string) (*Config, error) {
	p := argString(a, "profile")
	if p == "" {
		return cfg, nil
	}
	if cfg.Profiles[p].Kind != kind {
		return nil, fmt.Errorf("profile %q is not a configured %s profile", p, kind)
	}
	scoped := *cfg
	scoped.profile = p
	return &scoped, nil
}

func sendRPC(w *bufio.Writer, resp rpcResponse) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return w.Flush()
}

func runMCP(ctx context.Context, cfg *Config) error {
	return serveMCP(ctx, cfg, os.Stdin, os.Stdout)
}

func serveMCP(ctx context.Context, cfg *Config, input io.Reader, output io.Writer) error {
	handlers := map[string]func(json.RawMessage) (any, error){
		"initialize": initializeRPC,
		"ping":       func(json.RawMessage) (any, error) { return map[string]any{}, nil },
		"tools/list": func(json.RawMessage) (any, error) { return map[string]any{"tools": mcpTools(cfg)}, nil },
		"tools/call": func(params json.RawMessage) (any, error) { return callToolRPC(ctx, cfg, params) },
	}
	in := bufio.NewScanner(input)
	in.Buffer(make([]byte, 64*1024), 8*1024*1024)
	out := bufio.NewWriter(output)
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			if err := sendRPC(out, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}}); err != nil {
				return err
			}
			continue
		}
		// Notifications have no id and must not receive a response.
		notification := len(req.ID) == 0 || string(req.ID) == "null"
		if req.Method == "notifications/initialized" || req.Method == "notifications/cancelled" {
			continue
		}
		if req.Method == "server/discover" {
			// Intentionally legacy-only MCP. Modern clients are expected to fall back to initialize.
			if !notification {
				if err := sendRPC(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found; use legacy MCP initialize"}}); err != nil {
					return err
				}
			}
			continue
		}
		handler := handlers[req.Method]
		if handler == nil {
			if !notification {
				if err := sendRPC(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found"}}); err != nil {
					return err
				}
			}
			continue
		}
		result, callErr := handler(req.Params)
		if notification {
			continue
		}
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result}
		if callErr != nil {
			resp.Result = nil
			resp.Error = &rpcError{Code: -32602, Message: callErr.Error()}
		}
		if err := sendRPC(out, resp); err != nil {
			return err
		}
	}
	return in.Err()
}

func initializeRPC(params json.RawMessage) (any, error) {
	var args struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	_ = json.Unmarshal(params, &args)
	pv := args.ProtocolVersion
	switch pv {
	case "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05":
	default:
		pv = "2025-11-25"
	}
	return map[string]any{
		"protocolVersion": pv,
		"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		"serverInfo":      map[string]any{"name": "workctl", "version": version},
		"instructions":    "Azure DevOps (azure), Jira and Confluence Cloud (atlassian), Jira Server, GitLab and GitHub through workctl. Everything is read-only unless policy.allow_write=true in the config file.",
	}, nil
}

func callToolRPC(ctx context.Context, cfg *Config, params json.RawMessage) (any, error) {
	var args struct {
		Name string         `json:"name"`
		Args map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	data, err := callMCPTool(ctx, cfg, args.Name, args.Args)
	if err != nil {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}, nil
	}
	if max := cfg.MCP.MaxOutputBytes; max > 0 && len(data) > max {
		data = append(data[:max], []byte("\n... [workctl MCP output truncated; use CLI for full output]")...)
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}}, nil
}

func argString(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
func argInt(m map[string]any, k string, def int) int {
	switch v := m[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		n, _ := strconv.Atoi(v.String())
		return n
	}
	return def
}
func argBool(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }

// callMCPTool runs one MCP tool. Provider clients are made per call, so a missing login fails only that provider's tools.
func callMCPTool(ctx context.Context, cfg *Config, name string, a map[string]any) ([]byte, error) {
	if !toolAvailable(cfg, name) {
		return nil, fmt.Errorf("tool %q is not available: its backend is not configured", name)
	}
	switch {
	case strings.HasPrefix(name, "github_"):
		return callGitHubTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "gitlab_"):
		return callGitLabTool(ctx, cfg, name, a)
	case strings.HasPrefix(name, "jira_"):
		return callJiraTool(ctx, cfg, name, a)
	}
	switch name {
	case "pr_list", "pr_show":
		id := 0
		if name == "pr_show" {
			if id = argInt(a, "id", 0); id <= 0 {
				return nil, fmt.Errorf("id must be a positive integer")
			}
		}
		pName, profile, err := prProfile(cfg, argString(a, "profile"), "")
		if err != nil {
			return nil, err
		}
		res, err := fetchPullRequests(ctx, cfg, pName, profile, argString(a, "repo"), argString(a, "project"), argString(a, "search"), argString(a, "state"), id)
		if err != nil {
			return nil, err
		}
		format, err := render.ParseFormat(firstNonEmpty(argString(a, "format"), "json"))
		if err != nil {
			return nil, err
		}
		var stdout, stderr bytes.Buffer
		if err := render.PullRequests(&stdout, &stderr, res, format, id > 0); err != nil {
			return nil, err
		}
		return stdout.Bytes(), nil
	case "azure", "atlassian":
		args, err := argStrings(a, "args")
		if err != nil {
			return nil, err
		}
		return callModule(ctx, cfg, name, args)
	case "task_get_context":
		return getTaskContext(ctx, cfg, argString(a, "key"))
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
