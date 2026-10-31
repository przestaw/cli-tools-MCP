package output

import (
	"io"
	shared "workctl/internal/output"
)

type Printer shared.Printer

func New(jsonOut bool, fields []string) *Printer { return (*Printer)(shared.New(jsonOut, fields)) }
func (p *Printer) out() io.Writer                { return (*shared.Printer)(p).Out() }
func (p *Printer) cell(value string) string      { return (*shared.Printer)(p).Cell(value) }
func (p *Printer) JSONValue(value any) error     { return (*shared.Printer)(p).JSONValue(value) }
func (p *Printer) Raw(data []byte) error         { return (*shared.Printer)(p).Raw(data) }
func (p *Printer) Table(headers []string, rows [][]string) error {
	return (*shared.Printer)(p).Table(headers, rows)
}
func (p *Printer) Msg(format string, args ...any) { (*shared.Printer)(p).Msg(format, args...) }
