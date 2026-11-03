// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package api

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"strings"

	"workctl/internal/cliutil"
	"workctl/internal/config"
)

// RawCommand is the shared `raw [--body JSON|@file] [--query KEY=VALUE]... METHOD PATH` command of the thin providers:
// one arbitrary request against the profile's server. Anything but GET and HEAD needs the write policy.
func RawCommand(ctx context.Context, c *Client, cfg *config.Config, args []string) error {
	fs := flag.NewFlagSet("raw", flag.ContinueOnError)
	bodyArg := fs.String("body", "", "JSON body or @file")
	query := QueryFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("usage: raw [--body JSON|@file] [--query KEY=VALUE]... METHOD PATH")
	}
	method := strings.ToUpper(fs.Arg(0))
	if !IsReadMethod(method) {
		if err := cfg.RequireWrite(); err != nil {
			return err
		}
	}
	body, err := cliutil.ReadBodyArg(*bodyArg)
	if err != nil {
		return err
	}
	b, _, err := c.Do(ctx, method, fs.Arg(1), query, body, "application/json")
	if err != nil {
		return err
	}
	return cliutil.PrintBody(b)
}

// QueryFlag adds the repeatable --query KEY=VALUE flag to fs and returns the values it collects; workctl encodes them.
func QueryFlag(fs *flag.FlagSet) url.Values {
	query := url.Values{}
	fs.Func("query", "query parameter KEY=VALUE, encoded by workctl; repeat for several values of one key", func(s string) error {
		key, value, ok := strings.Cut(s, "=")
		if !ok || key == "" {
			return fmt.Errorf("want KEY=VALUE, got %q", s)
		}
		query.Add(key, value)
		return nil
	})
	return query
}
