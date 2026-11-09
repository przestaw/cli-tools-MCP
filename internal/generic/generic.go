// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package generic serves profiles of kind "api": any HTTP service without a dedicated provider
// (CI servers, artifact repositories, ...) and, with backend ssh-exec, services whose API is a set of
// commands run over ssh. It adds no endpoints of its own, only `workctl api raw|download|exec` and the
// api_request, api_download and api_exec MCP tools.
package generic

import (
	"context"
	"encoding/json"
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

// Tools are request and download (every HTTP profile) and api_exec (ssh-exec api profiles). They are listed in
// read-only mode too: GET, HEAD and read_only commands need no write policy.
func Tools() []mcp.Tool {
	profile := mcp.StrProp("Profile of any HTTP kind (github, gitlab, jira, atlassian, api); omit for defaults.api")
	query := map[string]any{
		"type":        "object",
		"description": "Query parameters, encoded by workctl. A value may be a string, number, boolean or an array (one parameter per item, e.g. a multi-select).",
	}
	path := mcp.StrProp("Path on the profile's server: a relative path gets the API prefix (repos/o/r, projects/1, issue/ABC-1), one starting with / is taken from the site root. Full URLs are refused.")
	return []mcp.Tool{{
		Name:        "request",
		Description: "Send one HTTP request through a configured profile (any provider API or a generic service) for what the other tools do not cover. GET/HEAD always allowed, other methods need policy.allow_write=true.",
		InputSchema: mcp.ObjSchema(map[string]any{"method": mcp.StrProp("HTTP method, default GET"), "path": path, "query": query, "body": mcp.StrProp("Request body (JSON text)"),
			"content_type":      mcp.StrProp("Body media type; default application/json; use application/x-www-form-urlencoded for form parameters"),
			"response_metadata": mcp.BoolProp("Return body and Location (e.g. Jenkins queue item URL); native HTTP profiles only"),
			"accept":            mcp.StrProp("Accept header, e.g. application/vnd.github.full+json for body_html with signed attachment links"), "profile": profile}, "path"),
	}, {
		Name:        "download",
		Description: "Download one file through a configured profile without changing a byte (attachments, logs, artifacts, archives). Streams it to a new file in the workctl downloads directory and returns its path, size, SHA-256, content type, the server's file name and a warning when an HTML page came instead. Redirects to known storage hosts (GitHub Actions logs, release assets) are followed without the credential.",
		InputSchema: mcp.ObjSchema(map[string]any{"path": mcp.StrProp("Path as for request, or a signed https URL on a storage host (e.g. a private-user-images.githubusercontent.com link from body_html), fetched without the credential"),
			"query": query, "filename": mcp.StrProp("File name to keep (default: the server's or the path's); directories are dropped"), "profile": profile}, "path"),
	}, {
		Name:        "api_exec",
		Description: "Run one remote command of an ssh-exec api profile. Only commands allowed by the profile run; those not marked read_only need policy.allow_write=true.",
		InputSchema: mcp.ObjSchema(map[string]any{"args": mcp.ArrProp("Command and arguments, one word per item"), "profile": mcp.StrProp("api profile; omit to use defaults.api")}, "args"),
	}}
}

// Available reports whether request/download have a profile to use.
func Available(cfg *config.Config) bool {
	for _, p := range cfg.Profiles {
		if httpKinds[p.Kind] && !p.UsesSSHExec() {
			return true
		}
	}
	return false
}

var httpKinds = map[string]bool{"github": true, "gitlab": true, "jira": true, "atlassian": true, Kind: true}

// httpClient connects to the profile of a request or download: the named profile of any HTTP kind, else defaults.api.
func httpClient(cfg *config.Config, name string) (*api.Client, error) {
	if name == "" {
		if p, ok := cfg.Profiles[cfg.ProfileName()]; ok && httpKinds[p.Kind] {
			name = cfg.ProfileName()
		} else {
			var err error
			if name, _, err = cfg.ProfileOf(Kind); err != nil {
				return nil, fmt.Errorf("pass profile (any of github, gitlab, jira, atlassian, api): %w", err)
			}
		}
	}
	p, ok := cfg.Profiles[name]
	switch {
	case !ok:
		return nil, fmt.Errorf("profile %q is not configured", name)
	case !httpKinds[p.Kind]:
		return nil, fmt.Errorf("profile %q is of kind %s: use the cli tool for it", name, p.Kind)
	case p.UsesSSHExec():
		return nil, fmt.Errorf("profile %q runs remote commands over ssh: use api exec (the api_exec tool)", name)
	}
	return api.NewProfileClient(cfg, name, p)
}

// CallTool runs request, download and api_exec.
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) (out []byte, resultErr error) {
	switch name {
	case "request":
		return request(ctx, cfg, a)
	case "download":
		c, err := httpClient(cfg, mcp.ArgString(a, "profile"))
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
		cfg, err := cfg.Scoped(mcp.ArgString(a, "profile"), Kind)
		if err != nil {
			return nil, err
		}
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

// ValidateRequest checks the profile, query and target before a batch sends its first write.
func ValidateRequest(cfg *config.Config, a map[string]any) error {
	c, err := httpClient(cfg, mcp.ArgString(a, "profile"))
	if err != nil {
		return err
	}
	query, err := queryArg(a["query"])
	if err != nil {
		return err
	}
	method := strings.ToUpper(mcp.ArgString(a, "method"))
	if method == "" {
		method = http.MethodGet
	}
	return c.ValidateRequest(method, mcp.ArgString(a, "path"), query)
}

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
	c, err := httpClient(cfg, mcp.ArgString(a, "profile"))
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
	if accept := mcp.ArgString(a, "accept"); accept != "" {
		c = c.WithAccept(accept)
	}
	ctype := mcp.ArgString(a, "content_type")
	if mcp.ArgBool(a, "response_metadata") {
		data, headers, err := c.DoWithHeaders(ctx, method, mcp.ArgString(a, "path"), query, body, ctype)
		if err != nil {
			return nil, err
		}
		var result any = string(data)
		if json.Valid(data) {
			result = json.RawMessage(data)
		}
		return json.Marshal(map[string]any{"body": result, "location": headers.Get("Location")})
	}
	return api.BodyOnly(c.Do(ctx, method, mcp.ArgString(a, "path"), query, body, ctype))
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
