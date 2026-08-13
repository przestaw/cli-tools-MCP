// Package output renders results as human-readable tables or JSON.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

// Printer controls how results are rendered.
type Printer struct {
	JSON   bool     // emit JSON instead of a table
	Fields []string // optional column subset (table mode)
	Color  bool     // enable ANSI colors
	Expand bool     // do not truncate long values
	W      io.Writer
}

// New returns a Printer writing to stdout.
func New(jsonOut bool, fields []string) *Printer {
	return &Printer{JSON: jsonOut, Fields: fields, W: os.Stdout}
}

// out returns the destination writer (defaults to stdout).
func (p *Printer) out() io.Writer {
	if p.W == nil {
		return os.Stdout
	}
	return p.W
}

// JSONValue marshals v as indented JSON.
func (p *Printer) JSONValue(v any) error {
	enc := json.NewEncoder(p.out())
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// Raw writes pre-formatted JSON bytes (pretty-printed).
func (p *Printer) Raw(data []byte) error {
	var buf any
	if err := json.Unmarshal(data, &buf); err != nil {
		// not JSON, dump as-is
		_, err := p.out().Write(data)
		return err
	}
	return p.JSONValue(buf)
}

// Table renders rows with the given headers. Each row must have len(headers) cells.
// If p.Fields is set, only those columns (matched case-insensitively by header)
// are shown, in the requested order.
func (p *Printer) Table(headers []string, rows [][]string) error {
	cols := selectColumns(headers, p.Fields)
	tw := tabwriter.NewWriter(p.out(), 0, 2, 2, ' ', 0)
	hdr := make([]string, len(cols))
	for i, c := range cols {
		hdr[i] = strings.ToUpper(headers[c])
	}
	fmt.Fprintln(tw, strings.Join(hdr, "\t"))
	for _, row := range rows {
		cells := make([]string, len(cols))
		for i, c := range cols {
			if c < len(row) {
				cells[i] = p.cell(row[c])
			}
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	return tw.Flush()
}

// selectColumns maps requested field names to header indices, preserving the
// requested order. An empty request returns all columns in order.
func selectColumns(headers, want []string) []int {
	if len(want) == 0 {
		idx := make([]int, len(headers))
		for i := range headers {
			idx[i] = i
		}
		return idx
	}
	var out []int
	for _, w := range want {
		for i, h := range headers {
			if strings.EqualFold(strings.TrimSpace(w), h) {
				out = append(out, i)
				break
			}
		}
	}
	if len(out) == 0 { // no matches -> fall back to all
		return selectColumns(headers, nil)
	}
	return out
}

// cell collapses newlines/tabs so a value stays on one table row, and caps the
// length to keep tables readable - unless Expand is set, which shows full text.
func (p *Printer) cell(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\t", " ")
	if !p.Expand && len([]rune(s)) > 80 {
		r := []rune(s)
		return string(r[:77]) + "..."
	}
	return s
}

// Msg prints a plain status line to stdout.
func (p *Printer) Msg(format string, a ...any) {
	fmt.Fprintf(p.out(), format+"\n", a...)
}
