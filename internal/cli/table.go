package cli

import "strings"

// ─── Column-aligned table parsing ─────────────────────────────────────────────
// kubectl and docker both print whitespace-padded tables whose columns are
// aligned to the header. Splitting rows on whitespace loses cells that contain
// spaces ("Up 2 hours", "2 hours ago") and mis-shifts every row that has an
// empty cell, so the column offsets from the header are used instead.

type table struct {
	// cols are the header names, in order.
	cols []string
	// starts[i] is the character offset where column i begins.
	starts []int
	// rows[r][c] is one already-trimmed cell.
	rows [][]string
}

func (t *table) index(name string) int {
	for i, c := range t.cols {
		if strings.EqualFold(c, name) {
			return i
		}
	}
	return -1
}

// cell returns a row's value for a named column, or "" when the table has no
// such column. Callers use it so a layout without, say, a STATUS column
// degrades instead of panicking.
func (t *table) cell(row []string, name string) string {
	i := t.index(name)
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// parseTable reads a column-aligned table. It returns ok=false whenever the
// layout cannot be trusted — a header with one column, a row whose cells have
// overflowed their column and shifted everything right. A partly-misread table
// would produce a confident summary of the wrong thing, which is worse than
// passing the raw output through.
func parseTable(raw string) (*table, bool) {
	lines := strings.Split(strings.TrimRight(raw, "\n"), "\n")
	// Skip any leading blank or non-table preamble.
	h := 0
	for h < len(lines) && strings.TrimSpace(lines[h]) == "" {
		h++
	}
	if h >= len(lines) {
		return nil, false
	}

	header := lines[h]
	cols, starts := splitAlignedHeader(header)
	if len(cols) < 2 {
		return nil, false
	}

	t := &table{cols: cols, starts: starts}
	for _, line := range lines[h+1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		row, ok := sliceAligned(line, starts)
		if !ok {
			// A cell wider than its header column shifts everything after it,
			// so the offsets cannot be used. Splitting on runs of two or more
			// spaces is unambiguous whenever it yields exactly the expected
			// number of cells, and is then the correct reading of the row.
			row, ok = splitWideGaps(line, len(cols))
			if !ok {
				return nil, false
			}
		}
		t.rows = append(t.rows, row)
	}
	if len(t.rows) == 0 {
		return nil, false
	}
	return t, true
}

// splitAlignedHeader breaks a header on runs of two or more spaces, so a header
// name that contains a single space ("CONTAINER ID") stays one column.
func splitAlignedHeader(header string) (cols []string, starts []int) {
	i := 0
	for i < len(header) {
		for i < len(header) && header[i] == ' ' {
			i++
		}
		if i >= len(header) {
			break
		}
		start := i
		for i < len(header) {
			if header[i] == ' ' && i+1 < len(header) && header[i+1] == ' ' {
				break
			}
			if header[i] == ' ' && i+1 >= len(header) {
				break
			}
			i++
		}
		cols = append(cols, strings.TrimSpace(header[start:i]))
		starts = append(starts, start)
	}
	return cols, starts
}

// sliceAligned cuts a data row at the header's column offsets. A cell that has
// grown past its column would push the next one right and silently corrupt
// every value after it, so that case is reported rather than sliced.
func sliceAligned(line string, starts []int) ([]string, bool) {
	out := make([]string, 0, len(starts))
	for i, s := range starts {
		if s > len(line) {
			// The row is short: the remaining columns are genuinely empty.
			out = append(out, "")
			continue
		}
		// Every column but the first must be preceded by whitespace, otherwise
		// the previous cell has overflowed into it.
		if i > 0 && s > 0 && s <= len(line) && line[s-1] != ' ' {
			return nil, false
		}
		end := len(line)
		if i+1 < len(starts) && starts[i+1] < end {
			end = starts[i+1]
		}
		out = append(out, strings.TrimSpace(line[s:end]))
	}
	return out, true
}

// splitWideGaps splits a row on runs of two or more spaces. It only reports
// success when the result has exactly the expected number of cells: any other
// count means a cell was empty or itself contained a wide gap, and guessing
// which would attribute a value to the wrong column.
func splitWideGaps(line string, want int) ([]string, bool) {
	var out []string
	i := 0
	for i < len(line) {
		for i < len(line) && line[i] == ' ' {
			i++
		}
		if i >= len(line) {
			break
		}
		start := i
		for i < len(line) {
			if line[i] == ' ' && i+1 < len(line) && line[i+1] == ' ' {
				break
			}
			if line[i] == ' ' && i+1 >= len(line) {
				break
			}
			i++
		}
		out = append(out, strings.TrimSpace(line[start:i]))
	}
	if len(out) != want {
		return nil, false
	}
	return out, true
}

// dedupConsecutive collapses runs of identical lines into one line plus a
// count. Container and pod logs are mostly repetition — a health check every
// second, the same warning on every request — and the repetition carries no
// information beyond how often it happened.
func dedupConsecutive(lines []string) []string {
	var out []string
	i := 0
	for i < len(lines) {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		if n := j - i; n > 1 {
			out = append(out, lines[i]+"  (x"+itoa(n)+")")
		} else {
			out = append(out, lines[i])
		}
		i = j
	}
	return out
}

// capLines keeps the head and tail of a long output and says how much was
// dropped. The two ends are where the answer usually is: what was asked for,
// and how it finished.
func capLines(lines []string, max int) []string {
	if len(lines) <= max {
		return lines
	}
	head := max * 2 / 3
	tail := max - head - 1
	out := make([]string, 0, max)
	out = append(out, lines[:head]...)
	out = append(out, "... ("+itoa(len(lines)-head-tail)+" lines omitted; re-run with --raw for all)")
	out = append(out, lines[len(lines)-tail:]...)
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
