package tui

import (
	"strings"
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
// A table is drawn as ruled columns: a faint │ between adjacent columns with one space either
// side, one dim rule under the header and another between each pair of adjacent body rows, every
// one of them crossing every divider at a ┼, and nothing else — no outer frame, no corners, no rule
// above the first body row or below the last — so it still sits in the body column like any other
// paragraph rather than a boxed object dropped into the transcript, while both a column boundary
// and a row boundary stay readable (layout.md). The rules are one glyph set and one style: the
// header keeps its distinction through its bold cell text, not through a rule of its own. Every width is a display width (th.measure over the rendered,
// ANSI-carrying cell — width.go), never a byte count: the cells are styled before they are
// measured, so markup characters and escape bytes can never push a column open. The divider is
// drawn in the rule's own faint style, because the frame is not content. Nothing is ever cut: a
// cell too wide for its column wraps inside it, so one row is as many physical lines as its tallest
// cell needs, cells top-aligned and a short one blank-filled below its content. The line-oriented
// renderer above this file (render.go) is untroubled by that — a row simply contributes more lines
// to the block than one — and every one of those lines, continuation and filler included, is padded
// to the table's full width, so the block still shows one straight right edge.

// tableDivider separates two adjacent columns: the vertical rule with one space either side, so
// the columns are told apart without their text touching the stroke (layout.md).
// tableDividerWidth is its display width — the glyph is one cell under both width methods
// (TestTableDividerHoldsOneColumn), so the layout arithmetic is a constant rather than a
// measurement.
const (
	tableDivider      = " " + glyphTableColumn + " "
	tableDividerWidth = 3
)

// minTableColumnWidth is the narrowest a column may be squeezed to and still read as a column.
// Below four cells a wrapped cell comes apart into a letter or two per line — vertical text with a
// rule beside it rather than a table — and the plain paragraphs the block falls back to are more
// readable than that, which is why the floor is the fallback's threshold rather than a width the
// layout quietly rounds up to. It is a floor on the SHRINK, not a width every column is given: a
// column whose content is naturally narrower keeps its own width and is never charged the floor.
const minTableColumnWidth = 4

// renderTable lays a parsed table out as styled physical lines at the given width: bold header,
// one ─ rule the full width of the table crossing every divider at a ┼, then the body rows with
// that same rule between each adjacent pair of them, each cell inline-rendered, wrapped to its
// column, padded on the side its column's alignment names and separated from its neighbour by the
// vertical divider. The inter-row rule goes BETWEEN rows only: never above the first (the header's
// rule already sits there), never below the last (there is no bottom frame), and never inside a
// wrapped row — a row that spills onto further lines is still one row. A row contributes as many
// lines as its tallest cell needs, so the block is taller than its row count whenever a cell wraps —
// which is why the rows are appended rather than assigned. It reports false when the table cannot
// fit — the width cannot give every column minTableColumnWidth cells of content, once the dividers
// are paid for — and the caller then leaves the block to the paragraph path it would have taken
// before tables were rendered at all, which is always readable and never overflows.
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

	widths := tableColumnWidths(th, header, rows)
	if !fitColumns(widths, width-tableDividerWidth*(len(widths)-1), minTableColumnWidth) {
		return nil, false
	}

	out := make([]string, 0, 2*len(rows)+2)
	out = append(out, layoutTableRows(th, header, widths, tbl.align)...)
	out = append(out, tableRuleRow(th, widths))
	for i, row := range rows {
		if i > 0 {
			// Between adjacent rows only: the header's rule already separates the first from the
			// header above it, and the last row ends the block with nothing under it.
			out = append(out, tableRuleRow(th, widths))
		}
		out = append(out, layoutTableRows(th, row, widths, tbl.align)...)
	}
	return out, true
}

// tableColumnWidths measures each column's natural width: the widest rendered cell in it, header
// included, floored at one cell so a column of nothing but empty cells still holds a place
// between its dividers instead of running them together.
func tableColumnWidths(th theme, header []string, rows [][]string) []int {
	widths := make([]int, len(header))
	for i, cell := range header {
		widths[i] = max(1, th.measure.Width(cell))
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(widths) {
				break
			}
			widths[i] = max(widths[i], th.measure.Width(cell))
		}
	}
	return widths
}

// fitColumns shrinks widths in place until they sum to no more than budget — the width left over
// once the dividers are paid for — and reports whether they fit at all. Space is always taken from
// the widest column and, where two are equally wide, from the leftmost, so the same table lays out
// the same way on every repaint (layout.md). It steps a whole level at a time rather than one cell
// at a time — the identical outcome, without a loop proportional to the overflow — because the
// markdown walk re-runs over the whole transcript on every streamed token (model.go).
//
// No column is taken below floor, and the width the table REQUIRES is the sum of min(natural,
// floor) rather than one floor per column: a column already narrower than the floor is never
// widened to it, so it must not be charged it either — a table of naturally narrow columns fits
// wherever its own widths fit. That required width is the whole fit test, and it subsumes the "even
// a single cell per column overflows" guard a floor of one made necessary.
func fitColumns(widths []int, budget, floor int) bool {
	total, required := 0, 0
	for _, w := range widths {
		total += w
		required += min(w, floor)
	}
	if required > budget {
		return false // some column would have to go under the floor, or under its own content
	}

	for total > budget {
		// The widest width among the columns that can still give a cell up, how many share it, and
		// the width just below it: the level this pass brings that group down to, never past the
		// floor. A column already at or under the floor is not in the running — it is either as
		// narrow as a column may be drawn, or narrower by its own nature and not shrinkable at all.
		top, next, count := 0, floor, 0
		for _, w := range widths {
			switch {
			case w <= floor: // nothing left to give: it is at the floor or naturally under it
			case w > top:
				top, next, count = w, max(next, top), 1
			case w == top:
				count++
			case w > next:
				next = w
			}
		}
		if count == 0 {
			// Unreachable while required <= budget holds — every column is then at or under the
			// floor only when the widths already sum inside the budget — and it stays here as the
			// loop's own termination guarantee, because a render path that cannot terminate hangs
			// the whole TUI.
			return false
		}
		// Never take more than the overflow, and never more than the level allows — the level is
		// floored, so this is also what keeps the group off the floor. The last cells come off the
		// group left to right, which leaves the extra cell with the rightmost of equally wide
		// columns.
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

// wrapTableCell breaks one rendered cell into the lines its column holds. The fast path is the one
// that matters: almost every cell already fits, and the markdown walk re-runs over the whole
// transcript on every streamed token (model.go), so a cell inside its column is handed back as the
// single line it already is without being wrapped at all. Anything wider goes through wrapText
// rather than ansi.Wrap or th.measure.Wrap directly — wrapText is what carries an SGR run across a
// break, so a **bold** span survives being divided, and what enforces the painted cap on whatever
// the upstream wrapper hands back (render.go, ADR 0030 §7). The cap is enforced here, never assumed.
func wrapTableCell(th theme, cell string, width int) []string {
	if th.measure.Width(cell) <= width {
		return []string{cell}
	}
	return wrapText(th, cell, width)
}

// layoutTableRows lays one row's rendered cells into the fitted column widths and returns the
// physical lines it occupies: every cell is wrapped to its own column, the row is as tall as the
// tallest of them, and each line takes one line from every column — a run of spaces where a shorter
// cell has already run out, so cells are top-aligned and a short cell's blanks fall below its
// content rather than above it. Nothing is dropped and no height is capped: a cap would only put
// the truncation back at a different threshold. The columns are joined by the divider — drawn in
// the rule's faint style, the same reasoning theme.go's mdRule comment gives for the rule itself:
// the frame is not content, so it must not read as loudly as the cells it separates. The last
// column is padded like every other one, so EVERY line of a table — header, rule, body rows and the
// continuation and filler lines a wrapped row adds — is exactly the table's width. That straight
// right edge is load-bearing, not cosmetic: a short line leaves the transcript's right gutter wider
// beside that line than beside the rule above it, which reads as the scroll bar stepping inward
// beside the body, and it ends the line's selectable cells early where the mouse still addresses
// the full width (mouse.go). The trailing blanks cost the copied text nothing —
// transcriptSelectionText trims each line it cuts.
func layoutTableRows(th theme, cells []string, widths []int, align []mdAlign) []string {
	wrapped := make([][]string, len(widths))
	height := 1
	for i, w := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		wrapped[i] = wrapTableCell(th, cell, w)
		height = max(height, len(wrapped[i]))
	}

	divider := th.mdRule.Render(tableDivider)
	out := make([]string, height)
	for line := range height {
		var b strings.Builder
		for i, w := range widths {
			if i > 0 {
				b.WriteString(divider)
			}
			if line >= len(wrapped[i]) {
				b.WriteString(strings.Repeat(" ", w))
				continue
			}
			a := mdAlignLeft
			if i < len(align) {
				a = align[i]
			}
			b.WriteString(padTableCell(th, wrapped[i][line], w, a))
		}
		out[line] = b.String()
	}
	return out
}

// padTableCell pads one wrapped line of a cell out to its column with the spaces its alignment asks
// for — on the right for a left-aligned cell, on the left for a right-aligned one, split for a
// centred one with the odd cell going to its right (layout.md). Every wrapped line of a cell is
// padded, not just its first, so a right- or centre-aligned column stays aligned all the way down.
// It never cuts: the line arrives already wrapped to the column, and the only thing that can still
// exceed it is a single grapheme wider than the whole column, which no break can divide and which
// layout.md's width cap exempts — wrapText gives it a line to itself and it keeps it.
func padTableCell(th theme, cell string, width int, align mdAlign) string {
	pad := width - th.measure.Width(cell)
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

// tableRuleRow renders one horizontal rule — the line under the header and the line between two
// adjacent body rows are the same line, drawn by this one builder in the one glyph set and the one
// faint style, because a second rule shape would say the two boundaries mean different things. The
// header is told from the body by its bold cells, not by its rule. It is a ─ run per column, joined
// by ─┼─ where the divider passes through, so the table is ruled across its whole width rather than
// by one dash per column broken at every column division, and each crossing sits in exactly the cell
// the divider occupies on the rows above and below it. The joint is tableDividerWidth cells like the
// divider itself, so the rule comes out exactly as wide as every other line of the block — the
// same arithmetic layoutTableRows walks.
func tableRuleRow(th theme, widths []int) string {
	columns := make([]string, len(widths))
	for i, w := range widths {
		columns[i] = strings.Repeat(glyphTableRule, w)
	}
	crossing := glyphTableRule + glyphTableCross + glyphTableRule
	return th.mdRule.Render(strings.Join(columns, crossing))
}
