// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package google

import (
	"testing"

	"workctl/internal/api"
)

func TestMD5Hash(t *testing.T) {
	for name, tc := range map[string]struct {
		drive  *api.DriveFile
		export string
		want   bool
	}{
		"drive file with md5":       {&api.DriveFile{MD5: "abc"}, "", true},
		"drive file without md5":    {&api.DriveFile{}, "", false},
		"native document, exported": {&api.DriveFile{}, "text/plain", false},
		"export ignores md5":        {&api.DriveFile{MD5: "abc"}, "text/plain", false},
		"not a drive download":      {nil, "", false},
	} {
		if got := MD5Hash(tc.drive, tc.export) != nil; got != tc.want {
			t.Errorf("%s: MD5Hash != nil is %v, want %v", name, got, tc.want)
		}
	}
}
