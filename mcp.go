package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	case strings.HasPrefix(name, "jira_"):
		return cfg.hasJira()
	case strings.HasPrefix(name, "gitlab_"):
		return cfg.hasGitLab()
	case strings.HasPrefix(name, "github_"):
		return cfg.hasGitHub()
	case strings.HasPrefix(name, "task_"):
		return cfg.hasJira() && cfg.hasGitLab()
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
	if cfg.hasGitHub() {
		tools = append(tools, githubTools(cfg)...)
	}
	return append(tools, moduleTools(cfg)...)
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
	var jc, gc *APIClient
	var err error
	if cfg.hasJira() {
		if jc, err = newJiraClient(cfg); err != nil {
			return fmt.Errorf("jira client: %w", err)
		}
	}
	if cfg.hasGitLab() {
		if gc, err = newGitLabClient(cfg); err != nil {
			return fmt.Errorf("gitlab client: %w", err)
		}
	}
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64*1024), 8*1024*1024)
	out := bufio.NewWriter(os.Stdout)
	for in.Scan() {
		line := in.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			_ = sendRPC(out, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "Parse error"}})
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
				_ = sendRPC(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found; use legacy MCP initialize"}})
			}
			continue
		}
		var result any
		var callErr error
		switch req.Method {
		case "initialize":
			var p struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			_ = json.Unmarshal(req.Params, &p)
			pv := p.ProtocolVersion
			switch pv {
			case "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05":
			default:
				pv = "2025-11-25"
			}
			result = map[string]any{
				"protocolVersion": pv,
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
				"serverInfo":      map[string]any{"name": "workctl", "version": version},
				"instructions":    "Azure DevOps (azure), Jira and Confluence Cloud (atlassian), Jira Server, GitLab and GitHub through workctl. Everything is read-only unless policy.allow_write=true in the config file.",
			}
		case "ping":
			result = map[string]any{}
		case "tools/list":
			result = map[string]any{"tools": mcpTools(cfg)}
		case "tools/call":
			var p struct {
				Name string         `json:"name"`
				Args map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &p); err != nil {
				callErr = err
				break
			}
			data, err := callMCPTool(ctx, cfg, jc, gc, p.Name, p.Args)
			if err != nil {
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}
			} else {
				if max := cfg.MCP.MaxOutputBytes; max > 0 && len(data) > max {
					data = append(data[:max], []byte("\n... [workctl MCP output truncated; use CLI for full output]")...)
				}
				result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}}
			}
		default:
			if !notification {
				_ = sendRPC(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found"}})
			}
			continue
		}
		if notification {
			continue
		}
		if callErr != nil {
			_ = sendRPC(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32602, Message: callErr.Error()}})
		} else {
			_ = sendRPC(out, rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result})
		}
	}
	return in.Err()
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

func callMCPTool(ctx context.Context, cfg *Config, jc, gc *APIClient, name string, a map[string]any) ([]byte, error) {
	if !toolAvailable(cfg, name) {
		return nil, fmt.Errorf("tool %q is not available: its backend is not configured", name)
	}
	if strings.HasPrefix(name, "github_") {
		return callGitHubTool(ctx, cfg, name, a)
	}
	project, projectErr := glProject(cfg, argString(a, "project"))
	requireProject := func() error { return projectErr }
	switch name {
	case "azure", "atlassian":
		args, err := argStrings(a, "args")
		if err != nil {
			return nil, err
		}
		return callModule(ctx, cfg, name, args)
	case "jira_get_issue":
		return jiraGetIssue(ctx, jc, argString(a, "key"), nil)
	case "jira_search":
		return jiraSearch(ctx, jc, argString(a, "jql"), argInt(a, "start_at", 0), mustPositive(argInt(a, "max_results", 50), 50))
	case "jira_get_comments":
		return jiraComments(ctx, jc, argString(a, "key"))
	case "jira_get_transitions":
		return jiraTransitions(ctx, jc, argString(a, "key"))
	case "gitlab_get_project":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabGetProject(ctx, gc, project)
	case "gitlab_get_merge_request":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabGetMR(ctx, gc, project, argInt(a, "iid", 0))
	case "gitlab_get_merge_request_changes":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabGetMRChanges(ctx, gc, project, argInt(a, "iid", 0))
	case "gitlab_get_merge_request_notes":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabGetMRNotes(ctx, gc, project, argInt(a, "iid", 0))
	case "gitlab_list_merge_requests":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabListMRs(ctx, gc, project, argString(a, "search"), argString(a, "state"))
	case "gitlab_list_pipelines":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabListPipelines(ctx, gc, project, argString(a, "ref"), argInt(a, "per_page", 30))
	case "gitlab_get_pipeline":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabGetPipeline(ctx, gc, project, argInt(a, "id", 0))
	case "gitlab_get_pipeline_jobs":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabListPipelineJobs(ctx, gc, project, argInt(a, "id", 0))
	case "gitlab_get_job_trace":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabJobTrace(ctx, gc, project, argInt(a, "job_id", 0))
	case "gitlab_list_branches":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabListBranches(ctx, gc, project, argString(a, "search"))
	case "gitlab_get_file":
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabGetFile(ctx, gc, project, argString(a, "path"), argString(a, "ref"))
	case "task_get_context":
		return getTaskContext(ctx, cfg, jc, gc, argString(a, "key"))
	case "jira_add_comment":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		return jiraAddComment(ctx, jc, argString(a, "key"), argString(a, "body"))
	case "gitlab_add_merge_request_note":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabAddMRNote(ctx, gc, project, argInt(a, "iid", 0), argString(a, "body"))
	case "gitlab_create_merge_request":
		if err := requireWrite(cfg); err != nil {
			return nil, err
		}
		if err := requireProject(); err != nil {
			return nil, err
		}
		return gitlabCreateMR(ctx, gc, project, argString(a, "source"), argString(a, "target"), argString(a, "title"), argString(a, "description"), argBool(a, "draft"))
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
