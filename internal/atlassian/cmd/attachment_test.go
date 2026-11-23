// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package cmd

import "testing"

func TestOutputPathNeverFollowsTheServerName(t *testing.T) {
	for flag, cases := range map[string]map[string]string{
		"":      {"report.pdf": "report.pdf", "../../x": "x", "/etc/cron.d/x": "x"},
		"my.go": {"../../x": "my.go"},
	} {
		for name, want := range cases {
			if got := outputPath(flag, name); got != want {
				t.Errorf("outputPath(%q, %q) = %q, want %q", flag, name, got, want)
			}
		}
	}
}
