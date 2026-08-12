// Package diff produces a unified textual diff between two versions of a file.
// It uses a longest-common-subsequence edit script, which is adequate for
// showing pull-request changes without pulling in an external dependency.
package diff

import (
	"fmt"
	"strings"
)

// maxLCS caps the per-side line count for the O(n*m) LCS table. Beyond this a
// file is treated as a wholesale replacement to bound memory and time.
const maxLCS = 4000

type kind int

const (
	eq kind = iota
	del
	ins
)

type line struct {
	k          kind
	text       string
	aNum, bNum int
}

// Unified renders a unified diff of a (old) vs b (new) with ctx context lines.
// It returns the hunk body only (no "diff --git"/"---"/"+++" file header, which
// the caller adds). An empty return means the contents are identical.
func Unified(a, b []string, ctx int) string {
	if ctx < 0 {
		ctx = 0
	}
	ops := editScript(a, b)
	lines := number(ops)
	n := len(lines)

	// Mark lines that fall within ctx of any change; contiguous marked runs
	// become hunks.
	keep := make([]bool, n)
	changed := false
	for i, ln := range lines {
		if ln.k == eq {
			continue
		}
		changed = true
		lo, hi := i-ctx, i+ctx
		if lo < 0 {
			lo = 0
		}
		if hi >= n {
			hi = n - 1
		}
		for x := lo; x <= hi; x++ {
			keep[x] = true
		}
	}
	if !changed {
		return ""
	}

	var out strings.Builder
	for i := 0; i < n; {
		if !keep[i] {
			i++
			continue
		}
		j := i
		for j < n && keep[j] {
			j++
		}
		writeHunk(&out, lines[i:j])
		i = j
	}
	return out.String()
}

// writeHunk emits one @@ hunk for a contiguous run of kept lines.
func writeHunk(out *strings.Builder, run []line) {
	var aStart, aLen, bStart, bLen int
	for _, ln := range run {
		if ln.k != ins { // eq or del carries an old-file line number
			if aStart == 0 {
				aStart = ln.aNum
			}
			aLen++
		}
		if ln.k != del { // eq or ins carries a new-file line number
			if bStart == 0 {
				bStart = ln.bNum
			}
			bLen++
		}
	}
	fmt.Fprintf(out, "@@ -%s +%s @@\n", span(aStart, aLen), span(bStart, bLen))
	for _, ln := range run {
		switch ln.k {
		case eq:
			out.WriteString(" " + ln.text + "\n")
		case del:
			out.WriteString("-" + ln.text + "\n")
		case ins:
			out.WriteString("+" + ln.text + "\n")
		}
	}
}

// span renders the "start,len" part of a hunk header (len 0 -> "start,0").
func span(start, length int) string {
	if length == 0 {
		return "0,0"
	}
	if length == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, length)
}

// number assigns old/new line numbers to an edit script.
func number(ops []line) []line {
	aNum, bNum := 1, 1
	out := make([]line, len(ops))
	for i, o := range ops {
		o.aNum, o.bNum = 0, 0
		switch o.k {
		case eq:
			o.aNum, o.bNum = aNum, bNum
			aNum++
			bNum++
		case del:
			o.aNum = aNum
			aNum++
		case ins:
			o.bNum = bNum
			bNum++
		}
		out[i] = o
	}
	return out
}

// editScript computes an LCS-based edit script turning a into b.
func editScript(a, b []string) []line {
	n, m := len(a), len(b)
	if n > maxLCS || m > maxLCS {
		out := make([]line, 0, n+m)
		for _, s := range a {
			out = append(out, line{k: del, text: s})
		}
		for _, s := range b {
			out = append(out, line{k: ins, text: s})
		}
		return out
	}
	// dp[i][j] = LCS length of a[i:] and b[j:].
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out []line
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, line{k: eq, text: a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, line{k: del, text: a[i]})
			i++
		default:
			out = append(out, line{k: ins, text: b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, line{k: del, text: a[i]})
	}
	for ; j < m; j++ {
		out = append(out, line{k: ins, text: b[j]})
	}
	return out
}
