// Copyright (c) 2026 Przemysław Stawczyk (https://github.com/przestaw). All rights reserved.
// This file is part of workctl, licensed under the BSD 4-Clause License; see LICENSE and NOTICE.

package output

// ANSI color codes used for emphasis and status markers.
const (
	ansiReset = "\x1b[0m"
	ansiBold  = "\x1b[1m"
	ansiDim   = "\x1b[2m"
)

// paint wraps s in an ANSI code when color is enabled, else returns s unchanged.
func (p *Printer) paint(code, s string) string {
	if !p.Color {
		return s
	}
	return code + s + ansiReset
}

func (p *Printer) bold(s string) string { return p.paint(ansiBold, s) }
func (p *Printer) dim(s string) string  { return p.paint(ansiDim, s) }
