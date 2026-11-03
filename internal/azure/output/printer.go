// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package output

import shared "workctl/internal/output"

// Printer is the shared output printer.
type Printer = shared.Printer

// New returns a printer for stdout; see the shared output package.
var New = shared.New
