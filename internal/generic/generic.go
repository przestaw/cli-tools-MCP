// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package generic serves profiles of kind "api": any HTTP service without a dedicated provider
// (CI servers, artifact repositories, ...) and, with backend ssh-exec, services whose API is a set of
// commands run over ssh. It adds no endpoints of its own, only `workctl api raw|download|exec` and the
// api_request, api_download and api_exec MCP tools.
package generic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

// Kind is the profile kind served by this package.
const Kind = "api"

// Command runs `workctl api raw|download|exec ...` against the selected api profile.
func Command(cfg *config.Config, args []string) (resultErr error) {
	usage := fmt.Errorf("api commands: raw [--body JSON|@file] METHOD PATH, download [--output FILE] PATH, exec [--] CMD [ARG...], session-check")
	if len(args) == 0 {
		return usage
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.CommandTimeout("api"))
	defer cancel()
	switch args[0] {
	case "raw", "download", "session-check":
		c, err := api.NewKindClient(cfg, Kind)
		if err != nil {
			return err
		}
		defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
		if args[0] == "session-check" {
			if len(args) != 1 {
				return usage
			}
			if err := c.CheckSession(ctx); err != nil {
				return err
			}
			_, err := fmt.Fprintln(os.Stdout, "Session check passed: login succeeded; logout rejected the original session cookie.")
			return err
		}
		if args[0] == "download" {
			return downloadCommand(ctx, c, args[1:])
		}
		return api.RawCommand(ctx, c, cfg, args[1:])
	case "exec":
		argv := args[1:]
		if len(argv) > 0 && argv[0] == "--" {
			argv = argv[1:]
		}
		name, profile, err := sshExecProfile(cfg)
		if err != nil {
			return err
		}
		out, err := runSSH(ctx, cfg, name, profile, argv)
		if err != nil {
			return err
		}
		_, err = os.Stdout.Write(out)
		return err
	}
	return usage
}

// Tools are the MCP tools of api profiles. Both are listed in read-only mode too: GET, HEAD and read_only commands
// need no write policy.
func Tools() []mcp.Tool {
	tools := []mcp.Tool{{
		Name:        "api_request",
		Description: "Send one HTTP request to a configured api profile (generic service). Path is relative to the profile URL; GET/HEAD always allowed, other methods need policy.allow_write=true.",
		InputSchema: mcp.ObjSchema(map[string]any{
			"method": mcp.StrProp("HTTP method, default GET"),
			"path":   mcp.StrProp("Path on the profile's server, for example /api/items"),
			"query": map[string]any{
				"type":        "object",
				"description": "Query parameters, encoded by workctl. A value may be a string, number, boolean or an array (one parameter per item, e.g. a multi-select).",
			},
			"body": mcp.StrProp("Request body (JSON text)"),
		}, "path"),
	}, {
		Name:        "api_download",
		Description: "Download one file from a configured api profile without changing a byte (binary files, archives, logs). Streams it to a new file in the workctl downloads directory and returns its path, size, SHA-256, content type and the server's file name.",
		InputSchema: mcp.ObjSchema(map[string]any{
			"path":     mcp.StrProp("Path on the profile's server"),
			"query":    map[string]any{"type": "object", "description": "Query parameters, as in api_request"},
			"filename": mcp.StrProp("File name to keep (default: the server's or the path's); directories are dropped"),
		}, "path"),
	}, {
		Name:        "api_exec",
		Description: "Run one remote command of an ssh-exec api profile. Only commands allowed by the profile run; those not marked read_only need policy.allow_write=true.",
		InputSchema: mcp.ObjSchema(map[string]any{"args": mcp.ArrProp("Command and arguments, one word per item")}, "args"),
	}}
	mcp.WithProfileArg(tools, "api_", "api profile; omit to use defaults.api")
	return tools
}

// CallTool runs one api_* MCP tool against the profile named by the call (default: defaults.api).
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) (out []byte, resultErr error) {
	cfg, err := cfg.Scoped(mcp.ArgString(a, "profile"), Kind)
	if err != nil {
		return nil, err
	}
	switch name {
	case "api_request":
		return request(ctx, cfg, a)
	case "api_download":
		c, err := api.NewKindClient(cfg, Kind)
		if err != nil {
			return nil, err
		}
		defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
		query, err := queryArg(a["query"])
		if err != nil {
			return nil, err
		}
		return download(ctx, c, mcp.ArgString(a, "path"), query, "", mcp.ArgString(a, "filename"))
	case "api_exec":
		argv, err := mcp.ArgStrings(a, "args")
		if err != nil {
			return nil, err
		}
		profileName, profile, err := sshExecProfile(cfg)
		if err != nil {
			return nil, err
		}
		return runSSH(ctx, cfg, profileName, profile, argv)
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

// request is the api_request tool: one HTTP request, writes only with the write policy.
func request(ctx context.Context, cfg *config.Config, a map[string]any) (out []byte, resultErr error) {
	method := strings.ToUpper(mcp.ArgString(a, "method"))
	if method == "" {
		method = http.MethodGet
	}
	if !api.IsReadMethod(method) {
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	c, err := api.NewKindClient(cfg, Kind)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, c.Close()) }()
	query, err := queryArg(a["query"])
	if err != nil {
		return nil, err
	}
	var body []byte
	if s := mcp.ArgString(a, "body"); s != "" {
		body = []byte(s)
	}
	return api.BodyOnly(c.Do(ctx, method, mcp.ArgString(a, "path"), query, body, "application/json"))
}

// queryArg turns the query argument of api_request into URL parameters; an array adds one parameter per item.
func queryArg(arg any) (url.Values, error) {
	q := url.Values{}
	if arg == nil {
		return q, nil
	}
	m, ok := arg.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("query must be an object")
	}
	for key, value := range m {
		items, isList := value.([]any)
		if !isList {
			items = []any{value}
		}
		for _, item := range items {
			switch item.(type) {
			case string, float64, bool:
				q.Add(key, fmt.Sprint(item))
			default:
				return nil, fmt.Errorf("query %q: values must be strings, numbers or booleans", key)
			}
		}
	}
	return q, nil
}
