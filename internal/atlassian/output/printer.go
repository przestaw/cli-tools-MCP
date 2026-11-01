package output

import (
	"io"
	shared "workctl/internal/output"
)

// Printer wraps the shared output printer. The other files of this package add the Jira and Confluence renderers.
type Printer shared.Printer

// New returns a printer for stdout.
func New(jsonOut bool, fields []string) *Printer { return (*Printer)(shared.New(jsonOut, fields)) }
func (p *Printer) out() io.Writer                { return (*shared.Printer)(p).Out() }
func (p *Printer) cell(value string) string      { return (*shared.Printer)(p).Cell(value) }

// JSONValue writes value as indented JSON.
func (p *Printer) JSONValue(value any) error { return (*shared.Printer)(p).JSONValue(value) }

// Raw writes an API response: indented when it is JSON, unchanged otherwise.
func (p *Printer) Raw(data []byte) error { return (*shared.Printer)(p).Raw(data) }

// Table writes rows under upper-cased headers.
func (p *Printer) Table(headers []string, rows [][]string) error {
	return (*shared.Printer)(p).Table(headers, rows)
}

// Msg writes one formatted line.
func (p *Printer) Msg(format string, args ...any) { (*shared.Printer)(p).Msg(format, args...) }
