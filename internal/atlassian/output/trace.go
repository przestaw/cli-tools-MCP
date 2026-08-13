package output

import (
	"fmt"

	"workctl/internal/atlassian/service"
)

// Trace prints a traceability tree as an indented ASCII outline.
func (p *Printer) Trace(root *service.TraceNode) {
	w := p.out()
	fmt.Fprintf(w, "%s %s\n", root.Key, root.Summary)
	for i, e := range root.Edges {
		p.traceEdge(e, "", i == len(root.Edges)-1)
	}
}

func (p *Printer) traceEdge(e service.TraceEdge, indent string, last bool) {
	w := p.out()
	branch := "├─"
	childIndent := indent + "│ "
	if last {
		branch = "└─"
		childIndent = indent + "  "
	}
	fmt.Fprintf(w, "%s%s %s [%s] %s %s%s\n",
		indent, branch, arrow(e.Direction), e.Label, e.Node.Key, e.Node.Summary, suffix(e.Node))
	for i, c := range e.Node.Edges {
		p.traceEdge(c, childIndent, i == len(e.Node.Edges)-1)
	}
}

// arrow renders a direction marker (ASCII for terminal portability).
func arrow(dir string) string {
	switch dir {
	case "outward":
		return "v"
	case "inward":
		return "^"
	default:
		return "-"
	}
}

func suffix(n *service.TraceNode) string {
	switch {
	case n.Cyclic:
		return "  (cycle)"
	case n.Capped:
		return "  (...)"
	default:
		return ""
	}
}

// TraceLinks prints a flat one-level link table for an issue.
func (p *Printer) TraceLinks(links []service.FlatLink) error {
	rows := make([][]string, len(links))
	for i, l := range links {
		rows[i] = []string{l.Direction, l.Label, l.Key, l.Summary, l.Status}
	}
	return p.Table([]string{"Direction", "Type", "Key", "Summary", "Status"}, rows)
}
