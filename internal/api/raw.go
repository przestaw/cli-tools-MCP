package api

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"workctl/internal/cliutil"
	"workctl/internal/config"
)

// RawCommand is the shared `raw [--body JSON|@file] METHOD PATH` command of the thin providers:
// one arbitrary request against the profile's server. Anything but GET and HEAD needs the write policy.
func RawCommand(ctx context.Context, c *Client, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("raw", flag.ContinueOnError)
	bodyArg := fs.String("body", "", "JSON body or @file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: raw [--body JSON|@file] METHOD PATH")
	}
	method := strings.ToUpper(fs.Arg(0))
	if method != "GET" && method != "HEAD" {
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
	}
	body, err := cliutil.ReadBodyArg(*bodyArg)
	if err != nil {
		return err
	}
	b, _, err := c.Do(ctx, method, fs.Arg(1), nil, body, "application/json")
	if err != nil {
		return err
	}
	return cliutil.PrintBody(b)
}
