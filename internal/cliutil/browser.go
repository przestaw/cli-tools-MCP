// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cliutil

import (
	"context"
	"fmt"
	"net/url"
	"runtime"

	"workctl/internal/proc"
)

// OpenURL opens an http or https URL in the default browser and returns without waiting for the browser.
// Other schemes are refused so a server-supplied value can never start a local handler.
func OpenURL(target string) error {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("refusing to open %q: only absolute http and https URLs are allowed", target)
	}
	return proc.Command(context.Background(), openCommand(runtime.GOOS), openArgs(runtime.GOOS, target)...).Start()
}

func openCommand(goos string) string {
	switch goos {
	case "windows":
		return "rundll32"
	case "darwin":
		return "open"
	default:
		return "xdg-open"
	}
}

func openArgs(goos, target string) []string {
	if goos == "windows" {
		return []string{"url.dll,FileProtocolHandler", target}
	}
	return []string{target}
}
