package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
)

type Printer struct {
	JSON   bool
	Fields []string
	Color  bool
	Expand bool
	W      io.Writer
}

func New(jsonOut bool, fields []string) *Printer {
	return &Printer{JSON: jsonOut, Fields: fields, W: os.Stdout}
}

func (p *Printer) Out() io.Writer {
	if p.W == nil {
		return os.Stdout
	}
	return p.W
}

func (p *Printer) JSONValue(value any) error {
	enc := json.NewEncoder(p.Out())
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

func (p *Printer) Raw(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		_, err := p.Out().Write(data)
		return err
	}
	return p.JSONValue(value)
}

func (p *Printer) Table(headers []string, rows [][]string) error {
	cols := selectColumns(headers, p.Fields)
	tw := tabwriter.NewWriter(p.Out(), 0, 2, 2, ' ', 0)
	hdr := make([]string, len(cols))
	for index, col := range cols {
		hdr[index] = strings.ToUpper(headers[col])
	}
	if _, err := fmt.Fprintln(tw, strings.Join(hdr, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		cells := make([]string, len(cols))
		for index, col := range cols {
			if col < len(row) {
				cells[index] = p.Cell(row[col])
			}
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func (p *Printer) KeyValue(pairs [][2]string) error {
	rows := make([][]string, len(pairs))
	for index, pair := range pairs {
		rows[index] = []string{pair[0], pair[1]}
	}
	return p.Table([]string{"Field", "Value"}, rows)
}

func selectColumns(headers, want []string) []int {
	if len(want) == 0 {
		cols := make([]int, len(headers))
		for index := range headers {
			cols[index] = index
		}
		return cols
	}
	var cols []int
	for _, field := range want {
		for index, header := range headers {
			if strings.EqualFold(strings.TrimSpace(field), header) {
				cols = append(cols, index)
				break
			}
		}
	}
	if len(cols) == 0 {
		return selectColumns(headers, nil)
	}
	return cols
}

func (p *Printer) Cell(value string) string {
	value = strings.NewReplacer("\r", "", "\n", " ", "\t", " ").Replace(value)
	runes := []rune(value)
	if !p.Expand && len(runes) > 80 {
		return string(runes[:77]) + "..."
	}
	return value
}

func (p *Printer) Msg(format string, args ...any) { fmt.Fprintf(p.Out(), format+"\n", args...) }
