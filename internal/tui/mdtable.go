package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Markdown tables: detection and parsing
// ----------------------------------------------------------------------------
//
// GFM pipe tables in assistant text, whose visual contract is layout.md's "Markdown tables in
// assistant text". The file is two halves either side of the divider below: this one is pure
// data — no theme, no width, no ANSI — answering only "does a table start on this line, how far
// does it run, and what are its cells?", and the second lays those cells out as columns. Cell
// text comes out of the parser as markdown source, because the column widths that matter are the
// widths of the *rendered* cells.
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

// ----------------------------------------------------------------------------
// Markdown tables: layout and rendering
// ----------------------------------------------------------------------------
//
// A table is drawn borderless — columns of text and two spaces of gutter, with one dim rule
// under the header and no verticals anywhere — so it sits in the body column like any other
// paragraph rather than as a boxed object dropped into the transcript. Every width here is a
// display width (lipgloss.Width over the rendered, ANSI-carrying cell), never a byte count: the
// cells are styled before they are measured, so markup characters and escape bytes can never
// push a column open. One row is one physical line, which is what the line-oriented renderer
// above this file requires (render.go), so an over-wide cell is cut with a … rather than wrapped.

// tableGutter separates two adjacent columns: exactly two spaces, no vertical rule (layout.md).
const tableGutter = "  "

// renderTable lays a parsed table out as styled physical lines at the given width: bold header,
// a ─ rule per column, then the body rows, each cell inline-rendered and padded on the side its
// column's alignment names. It reports false when the table cannot be made to fit — the width is
// too narrow even with every column down to a single cell — and the caller then leaves the block
// to the paragraph path it would have taken before tables were rendered at all, which is always
// readable and never overflows.
func renderTable(th theme, tbl mdTable, width int) ([]string, bool) {
	if len(tbl.header) == 0 {
		return nil, false
	}

	header := make([]string, len(tbl.header))
	for i, cell := range tbl.header {
		header[i] = th.mdBold.Render(renderInline(th, cell))
	}
	rows := make([][]string, len(tbl.rows))
	for i, row := range tbl.rows {
		cells := make([]string, len(row))
		for j, cell := range row {
			cells[j] = renderInline(th, cell)
		}
		rows[i] = cells
	}

	widths := tableColumnWidths(header, rows)
	if !fitColumns(widths, width-len(tableGutter)*(len(widths)-1)) {
		return nil, false
	}

	out := make([]string, 0, len(rows)+2)
	out = append(out, layoutTableRow(header, widths, tbl.align))
	out = append(out, tableRuleRow(th, widths))
	for _, row := range rows {
		out = append(out, layoutTableRow(row, widths, tbl.align))
	}
	return out, true
}

// tableColumnWidths measures each column's natural width: the widest rendered cell in it, header
// included, floored at one cell so a column of nothing but empty cells still holds a place
// between its gutters instead of running them together.
func tableColumnWidths(header []string, rows [][]string) []int {
	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = max(1, lipgloss.Width(cell))
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			widths[i] = max(widths[i], lipgloss.Width(cell))
		}
	}
	return widths
}

// fitColumns shrinks widths in place until they sum to no more than budget — the width left over
// once the gutters are paid for — and reports whether they fit at all. Space is always taken from
// the widest column and, where two are equally wide, from the leftmost, so the same table lays out
// the same way on every repaint (layout.md). It steps a whole level at a time rather than one cell
// at a time — the identical outcome, without a loop proportional to the overflow — because the
// markdown walk re-runs over the whole transcript on every streamed token (model.go).
func fitColumns(widths []int, budget int) bool {
	total := 0
	for _, w := range widths {
		total += w
	}
	if len(widths) > budget {
		return false // even a single cell per column overflows the width
	}

	for total > budget {
		// The widest width, how many columns share it, and the width just below it: the level
		// this pass brings that group down to.
		top, next, count := 0, 0, 0
		for _, w := range widths {
			switch {
			case w > top:
				top, next, count = w, top, 1
			case w == top:
				count++
			case w > next:
				next = w
			}
		}
		// Never take more than the overflow: the last cells come off the group left to right,
		// which is what leaves the extra cell with the rightmost of equally wide columns.
		take := min(total-budget, count*(top-next))
		each, extra := take/count, take%count
		for i, w := range widths {
			if w != top {
				continue
			}
			cut := each
			if extra > 0 {
				cut++
				extra--
			}
			widths[i] -= cut
		}
		total -= take
	}
	return true
}

// layoutTableRow lays one row's rendered cells into the fitted column widths: a cell wider than
// its column is cut ANSI-aware with a … tail, the rest are padded on the side their column names,
// and the columns are joined by the gutter. The last column is padded like every other one, so
// EVERY line of a table — header, rule and body rows alike — is exactly the table's width. That
// straight right edge is load-bearing, not cosmetic: a short line leaves the transcript's right
// gutter wider beside that row than beside the rule above it, which reads as the scroll bar
// stepping inward beside the body, and it ends the row's selectable cells early where the mouse
// still addresses the full width (mouse.go). The trailing blanks cost the copied text nothing —
// transcriptSelectionText trims each line it cuts.
func layoutTableRow(cells []string, widths []int, align []mdAlign) string {
	var b strings.Builder
	for i, w := range widths {
		if i > 0 {
			b.WriteString(tableGutter)
		}
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		a := mdAlignLeft
		if i < len(align) {
			a = align[i]
		}
		b.WriteString(padTableCell(cell, w, a))
	}
	return b.String()
}

// padTableCell fits one rendered cell to its column: truncated with a … when it is too wide, then
// padded to the column with the spaces its alignment asks for — on the right for a left-aligned
// cell, on the left for a right-aligned one, split for a centred one with the odd cell going to
// its right (layout.md).
func padTableCell(cell string, width int, align mdAlign) string {
	if lipgloss.Width(cell) > width {
		cell = ansi.Truncate(cell, width, "…")
	}
	pad := width - lipgloss.Width(cell)
	if pad <= 0 {
		return cell
	}
	switch align {
	case mdAlignRight:
		return strings.Repeat(" ", pad) + cell
	case mdAlignCenter:
		left := pad / 2
		return strings.Repeat(" ", left) + cell + strings.Repeat(" ", pad-left)
	default:
		return cell + strings.Repeat(" ", pad)
	}
}

// tableRuleRow renders the line under the header: one ─ run per column, each as wide as its
// column, with the gutters between them left bare so it reads as a rule under each column rather
// than as a border drawn around the table.
func tableRuleRow(th theme, widths []int) string {
	rules := make([]string, len(widths))
	for i, w := range widths {
		rules[i] = th.mdRule.Render(strings.Repeat(glyphTableRule, w))
	}
	return strings.Join(rules, tableGutter)
}
