// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

// Package plugins embeds the agent skills of the plugin package, so the binary installs the very skills it was built with.
package plugins

import "embed"

// Skills holds plugins/workctl/skills: one directory per skill, rooted at "workctl/skills".
//
//go:embed workctl/skills
var Skills embed.FS
