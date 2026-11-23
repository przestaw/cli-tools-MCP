// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package config

import (
	"fmt"
	"strconv"
	"strings"
)

// SettingKeys lists the names Set accepts; "KIND" stands for a profile kind.
const SettingKeys = "policy.allow_write, policy.confirm_writes, policy.confirm_session_writes, " +
	"timeout_seconds, timeouts.KIND, mcp.max_output_bytes, modules.timeout_seconds, defaults.KIND"

// Set changes one setting of the policy, the timeouts or the per-kind defaults, as `workctl config set KEY VALUE`
// does. Policy keys also work without the "policy." prefix. It does not validate the result: call Validate.
// Profiles are edited with `workctl profile`.
func (c *Config) Set(key, value string) error {
	switch strings.TrimPrefix(key, "policy.") {
	case "allow_write":
		return setBool(&c.Policy.AllowWrite, key, value)
	case "confirm_writes":
		return setBool(&c.Policy.ConfirmWrites, key, value)
	case "confirm_session_writes":
		return setBool(&c.Policy.ConfirmSessionWrites, key, value)
	}
	switch key {
	case "timeout_seconds":
		return setCount(&c.TimeoutSeconds, key, value)
	case "mcp.max_output_bytes":
		return setCount(&c.MCP.MaxOutputBytes, key, value)
	case "modules.timeout_seconds":
		return setCount(&c.Modules.TimeoutSeconds, key, value)
	}
	if kind, ok := strings.CutPrefix(key, "timeouts."); ok && kind != "" {
		seconds := c.Timeouts[kind]
		if err := setCount(&seconds, key, value); err != nil {
			return err
		}
		if c.Timeouts == nil {
			c.Timeouts = map[string]int{}
		}
		c.Timeouts[kind] = seconds
		return nil
	}
	if kind, ok := strings.CutPrefix(key, "defaults."); ok && kind != "" {
		if value == "" {
			delete(c.Defaults, kind)
		} else {
			c.Defaults[kind] = value
		}
		return nil
	}
	return fmt.Errorf("unknown setting %q (use %s)", key, SettingKeys)
}

func setBool(dst *bool, key, value string) error {
	b, err := strconv.ParseBool(value)
	if err != nil {
		return fmt.Errorf("%s: %q is not true or false", key, value)
	}
	*dst = b
	return nil
}

func setCount(dst *int, key, value string) error {
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		return fmt.Errorf("%s: %q is not a non-negative number", key, value)
	}
	*dst = n
	return nil
}
