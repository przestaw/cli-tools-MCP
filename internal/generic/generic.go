// Package generic serves profiles of kind "api": any HTTP service without a dedicated provider
// (Jenkins, Artifactory, SonarQube, ...). It adds no endpoints of its own, only `workctl api raw`
// and the api_request MCP tool on top of the shared client in internal/api.
package generic

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

// Kind is the profile kind served by this package.
const Kind = "api"

// Command runs `workctl api raw [--body JSON|@file] METHOD PATH` against the selected api profile.
func Command(cfg *config.Config, args []string) error {
	if len(args) == 0 || args[0] != "raw" {
		return fmt.Errorf("api commands: raw [--body JSON|@file] METHOD PATH")
	}
	c, err := api.NewKindClient(cfg, Kind)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	return api.RawCommand(ctx, c, cfg, args[1:])
}

// Tools are the MCP tools of api profiles. The tool is listed in read-only mode too: GET and HEAD need no write policy.
func Tools() []mcp.Tool {
	tools := []mcp.Tool{{
		Name:        "api_request",
		Description: "Send one HTTP request to a configured api profile (generic service). Path is relative to the profile URL; GET/HEAD always allowed, other methods need policy.allow_write=true.",
		InputSchema: mcp.ObjSchema(map[string]any{
			"method": mcp.StrProp("HTTP method, default GET"),
			"path":   mcp.StrProp("Path on the profile's server, for example /job/build/api/json"),
			"body":   mcp.StrProp("Request body (JSON text)"),
		}, "path"),
	}}
	mcp.WithProfileArg(tools, "api_", "api profile; omit to use defaults.api")
	return tools
}

// CallTool runs the api_request MCP tool against the profile named by the call (default: defaults.api).
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	if name != "api_request" {
		return nil, fmt.Errorf("unknown tool %q", name)
	}
	cfg, err := cfg.Scoped(mcp.ArgString(a, "profile"), Kind)
	if err != nil {
		return nil, err
	}
	method := strings.ToUpper(mcp.ArgString(a, "method"))
	if method == "" {
		method = http.MethodGet
	}
	if method != http.MethodGet && method != http.MethodHead {
		if err := cfg.RequireWrite(); err != nil {
			return nil, err
		}
	}
	c, err := api.NewKindClient(cfg, Kind)
	if err != nil {
		return nil, err
	}
	var body []byte
	if s := mcp.ArgString(a, "body"); s != "" {
		body = []byte(s)
	}
	return api.BodyOnly(c.Do(ctx, method, mcp.ArgString(a, "path"), nil, body, "application/json"))
}
