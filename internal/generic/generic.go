// Package generic serves profiles of kind "api": any HTTP service without a dedicated provider
// (Jenkins, Artifactory, SonarQube, ...) and, with backend ssh-exec, services whose API is a set of
// commands run over ssh. It adds no endpoints of its own, only `workctl api raw|exec` and the
// api_request and api_exec MCP tools.
package generic

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"workctl/internal/api"
	"workctl/internal/config"
	"workctl/internal/mcp"
)

// Kind is the profile kind served by this package.
const Kind = "api"

// Command runs `workctl api raw [--body JSON|@file] METHOD PATH` or `workctl api exec [--] CMD [ARG...]`
// against the selected api profile.
func Command(cfg *config.Config, args []string) error {
	usage := fmt.Errorf("api commands: raw [--body JSON|@file] METHOD PATH, exec [--] CMD [ARG...]")
	if len(args) == 0 {
		return usage
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	switch args[0] {
	case "raw":
		c, err := api.NewKindClient(cfg, Kind)
		if err != nil {
			return err
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
			"path":   mcp.StrProp("Path on the profile's server, for example /job/build/api/json"),
			"body":   mcp.StrProp("Request body (JSON text)"),
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
func CallTool(ctx context.Context, cfg *config.Config, name string, a map[string]any) ([]byte, error) {
	cfg, err := cfg.Scoped(mcp.ArgString(a, "profile"), Kind)
	if err != nil {
		return nil, err
	}
	switch name {
	case "api_request":
		return request(ctx, cfg, a)
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
func request(ctx context.Context, cfg *config.Config, a map[string]any) ([]byte, error) {
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
