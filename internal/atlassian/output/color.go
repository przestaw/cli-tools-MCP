package output

// ANSI color codes used for emphasis and status markers.
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// paint wraps s in an ANSI code when color is enabled, else returns s unchanged.
func (p *Printer) paint(code, s string) string {
	if !p.Color {
		return s
	}
	return code + s + ansiReset
}

func (p *Printer) green(s string) string  { return p.paint(ansiGreen, s) }
func (p *Printer) red(s string) string    { return p.paint(ansiRed, s) }
func (p *Printer) yellow(s string) string { return p.paint(ansiYellow, s) }
func (p *Printer) cyan(s string) string   { return p.paint(ansiCyan, s) }
func (p *Printer) bold(s string) string   { return p.paint(ansiBold, s) }
func (p *Printer) dim(s string) string    { return p.paint(ansiDim, s) }
