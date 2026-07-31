package tui

import "strings"

// ----------------------------------------------------------------------------
// Markdown tables: detection and parsing
// ----------------------------------------------------------------------------
//
// GFM pipe tables in assistant text, whose visual contract is layout.md's "Markdown tables in
// assistant text". This is the pure data half: no theme, no width, no ANSI — it answers only
// "does a table start on this line, how far does it run, and what are its cells?", leaving every
// styling and layout decision to the renderer. Cell text comes out as markdown source, because
// the column widths that matter are the widths of the *rendered* cells.
//
// Detection is the two-line lookahead GFM describes: a line carrying at least one unescaped pipe,
// immediately followed by a delimiter row of the same cell count. Everything here is prefix and
// byte-scan work with no regex — the markdown walk re-runs over the whole transcript on every
// streamed token (model.go), so the cost of asking "is this a table?" is paid on every line of
// every message. A header row whose delimiter has not arrived yet simply is not a table, which is
// what makes a half-streamed table degrade to ordinary paragraphs for free.

// mdAlign is a table column's horizontal alignment, as named by its delimiter-row cell.
type mdAlign int

const (
	mdAlignLeft   mdAlign = iota // "---" or ":--"
	mdAlignCenter                // ":-:"
	mdAlignRight                 // "--:"
)

// mdTable is one parsed pipe table: its header cells, one alignment per column, and its body
// rows, each already normalised to the header's column count. The header alone sets the shape,
// so len(align) == len(header) == len(row) for every row.
type mdTable struct {
	header []string
	align  []mdAlign
	rows   [][]string
}

// matchTableBlock reports whether a pipe table begins at lines[start] and, when it does, returns
// the parsed table together with the number of source lines it spans (header + delimiter + body).
// The block ends at the first line with no unescaped pipe — a blank line carries no pipe either,
// so that one test closes the table on both the blank line and the pipe-free line of layout.md.
// A delimiter-shaped line with no header above it never opens a table: the line before it is what
// makes it a delimiter at all.
func matchTableBlock(lines []string, start int) (mdTable, int, bool) {
	if start < 0 || start+1 >= len(lines) || !hasUnescapedPipe(lines[start]) {
		return mdTable{}, 0, false
	}
	align, ok := parseDelimiterRow(lines[start+1])
	if !ok {
		return mdTable{}, 0, false
	}
	header := splitTableRow(lines[start])
	if len(align) != len(header) {
		return mdTable{}, 0, false
	}

	end := start + 2
	var rows [][]string
	for ; end < len(lines) && hasUnescapedPipe(lines[end]); end++ {
		rows = append(rows, fitRow(splitTableRow(lines[end]), len(header)))
	}
	return mdTable{header: header, align: align, rows: rows}, end - start, true
}

// parseDelimiterRow reports whether line is a table's delimiter row and returns the alignment it
// names for each column. A delimiter row carries nothing but pipes, hyphens, colons and spaces,
// and every one of its cells is a run of hyphens with an optional colon at either end.
func parseDelimiterRow(line string) ([]mdAlign, bool) {
	source := strings.TrimSpace(line)
	if source == "" {
		return nil, false
	}
	for i := 0; i < len(source); i++ {
		switch source[i] {
		case '|', '-', ':', ' ':
		default:
			return nil, false
		}
	}

	cells := splitTableRow(source)
	align := make([]mdAlign, 0, len(cells))
	for _, cell := range cells {
		a, ok := delimiterCellAlign(cell)
		if !ok {
			return nil, false
		}
		align = append(align, a)
	}
	return align, true
}

// delimiterCellAlign parses one delimiter cell — "-+", ":-+", "-+:" or ":-+:" — into the
// alignment it names. Anything else is not a delimiter cell: an empty cell, a bare colon, or a
// hyphen run broken by anything at all, which is what keeps prose that happens to contain a dash
// out of the table path.
func delimiterCellAlign(cell string) (mdAlign, bool) {
	leading := strings.HasPrefix(cell, ":")
	trailing := len(cell) > 1 && strings.HasSuffix(cell, ":")
	hyphens := cell
	if leading {
		hyphens = hyphens[1:]
	}
	if trailing {
		hyphens = hyphens[:len(hyphens)-1]
	}
	if hyphens == "" {
		return mdAlignLeft, false
	}
	for i := 0; i < len(hyphens); i++ {
		if hyphens[i] != '-' {
			return mdAlignLeft, false
		}
	}

	switch {
	case leading && trailing:
		return mdAlignCenter, true
	case trailing:
		return mdAlignRight, true
	default:
		return mdAlignLeft, true
	}
}

// splitTableRow splits one table row into its cells on unescaped pipes. The row's optional
// leading and trailing pipes are dropped, "\|" becomes the literal pipe it stands for inside the
// cell holding it, and every cell is trimmed of the spaces that padded the source — the source's
// own padding is never the alignment, the column widths are.
func splitTableRow(line string) []string {
	source := strings.TrimSpace(line)
	var cells []string
	var cell strings.Builder
	endsOnPipe := false
	for i := 0; i < len(source); i++ {
		switch {
		case source[i] == '\\' && i+1 < len(source) && source[i+1] == '|':
			cell.WriteByte('|')
			i++
			endsOnPipe = false
		case source[i] == '|':
			cells = append(cells, strings.TrimSpace(cell.String()))
			cell.Reset()
			endsOnPipe = i == len(source)-1
		default:
			cell.WriteByte(source[i])
			endsOnPipe = false
		}
	}
	cells = append(cells, strings.TrimSpace(cell.String()))

	// The pipes on either end of a row are decoration, not an empty first or last cell.
	if len(cells) > 1 && strings.HasPrefix(source, "|") {
		cells = cells[1:]
	}
	if len(cells) > 1 && endsOnPipe {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// fitRow normalises one body row to the table's column count: a short row is padded out with
// empty cells and a long one loses its excess, so a ragged row can never widen or narrow the
// table the header declared.
func fitRow(cells []string, columns int) []string {
	if len(cells) > columns {
		return cells[:columns]
	}
	for len(cells) < columns {
		cells = append(cells, "")
	}
	return cells
}

// hasUnescapedPipe reports whether s carries at least one pipe that is not escaped as "\|" — the
// single cheap test that both opens a table block (the header row must have one) and, failing,
// closes it (the first line without one ends the table).
func hasUnescapedPipe(s string) bool {
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '|':
			i++
		case s[i] == '|':
			return true
		}
	}
	return false
}
