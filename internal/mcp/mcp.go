// Package mcp implements the stdio JSON-RPC surface of `workctl mcp`: the protocol
// loop, the tool descriptor with its JSON-schema helpers, and the argument readers.
// It knows nothing about the providers; the caller injects the tool list and dispatch.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// Tool is one MCP tool descriptor, as listed by tools/list.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Server is what the protocol loop needs from the application.
type Server struct {
	Name         string
	Version      string
	Instructions string
	// Tools lists the available tools; called per tools/list request.
	Tools func() []Tool
	// Call runs one tool. Its error becomes an isError text result, not a protocol error.
	Call func(ctx context.Context, name string, args map[string]any) ([]byte, error)
	// MaxOutputBytes truncates tool output; 0 means no limit.
	MaxOutputBytes int
}

func ObjSchema(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func StrProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func IntProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}
func BoolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}
func ArrProp(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": desc}
}

// WithProfileArg adds the optional "profile" argument to the tools whose name starts with prefix.
func WithProfileArg(tools []Tool, prefix, desc string) {
	for _, t := range tools {
		if strings.HasPrefix(t.Name, prefix) {
			t.InputSchema["properties"].(map[string]any)["profile"] = StrProp(desc)
		}
	}
}

// ArgString reads an optional string argument.
func ArgString(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

// ArgInt reads an optional integer argument, accepting the JSON number forms.
func ArgInt(m map[string]any, k string, def int) int {
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

// ArgBool reads an optional boolean argument.
func ArgBool(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }

// ArgStrings reads an array argument as command-line words. Numbers and booleans are accepted (a pull request id
// often arrives as a JSON number); anything else is an error.
func ArgStrings(m map[string]any, k string) ([]string, error) {
	raw, ok := m[k]
	if !ok || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		if ss, isStrings := raw.([]string); isStrings {
			return ss, nil
		}
		return nil, fmt.Errorf("%s must be an array of strings", k)
	}
	out := make([]string, 0, len(list))
	for i, v := range list {
		switch x := v.(type) {
		case string:
			out = append(out, x)
		case float64:
			out = append(out, strconv.FormatFloat(x, 'f', -1, 64))
		case bool:
			out = append(out, strconv.FormatBool(x))
		default:
			return nil, fmt.Errorf("%s[%d] must be a string, number or boolean", k, i)
		}
	}
	return out, nil
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

// Serve runs the JSON-RPC loop on input and output until input ends.
func Serve(ctx context.Context, s Server, input io.Reader, output io.Writer) error {
	handlers := map[string]func(json.RawMessage) (any, error){
		"initialize": s.initializeRPC,
		"ping":       func(json.RawMessage) (any, error) { return map[string]any{}, nil },
		"tools/list": func(json.RawMessage) (any, error) { return map[string]any{"tools": s.Tools()}, nil },
		"tools/call": func(params json.RawMessage) (any, error) { return s.callToolRPC(ctx, params) },
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

func (s Server) initializeRPC(params json.RawMessage) (any, error) {
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
		"serverInfo":      map[string]any{"name": s.Name, "version": s.Version},
		"instructions":    s.Instructions,
	}, nil
}

func (s Server) callToolRPC(ctx context.Context, params json.RawMessage) (any, error) {
	var args struct {
		Name string         `json:"name"`
		Args map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &args); err != nil {
		return nil, err
	}
	data, err := s.Call(ctx, args.Name, args.Args)
	if err != nil {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": err.Error()}}, "isError": true}, nil
	}
	if max := s.MaxOutputBytes; max > 0 && len(data) > max {
		data = append(data[:max], []byte("\n... [workctl MCP output truncated; use CLI for full output]")...)
	}
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": string(data)}}}, nil
}
