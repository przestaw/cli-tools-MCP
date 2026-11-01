package output

import shared "workctl/internal/output"

// Printer is the shared output printer.
type Printer = shared.Printer

// New returns a printer for stdout; see the shared output package.
var New = shared.New
