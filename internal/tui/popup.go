package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// The shared selector-popup painter (selector-popup plan §1)
// ----------------------------------------------------------------------------
//
// renderPopup is the single place overlay-pane chrome is painted: a titled, bordered pane holding
// an optional wrapping body block, a scrolled row list with the selected row highlighted, and a
// key-hint footer. The /sessions history browser (sessions.go), the command/file/skill autocomplete
// dropdown (autocomplete.go), and the ask and approval prompts (model.go) all compose their pane
// through it, so every overlay shares one look and one right edge. The dependency points inward
// only — callers compose a popupSpec and hand it here; renderPopup reaches back into neither
// overlay's state.
//
// Contract:
//   - width is the TOTAL box width, in lipgloss v2 semantics: the rounded border and the padding
//     fold INTO width, so every rendered line is exactly width display cells (like
//     renderStartupBox). Callers pass m.width — the full window width, the same value the input
//     box spans — so the pane's right border lands on the terminal's last column, flush with the
//     prompt box below it.
//   - The pane is filled solid black: the border style paints its border and padding cells black,
//     and renderPopup pads every content line to the full inner width on the same black field, so
//     no interior cell (including the gap after a short row) is left on the terminal background.
//   - The module owns the marker (glyphUser + a space on the selected row, two spaces
//     otherwise), the selected-row highlight (th.userBlock's full bar), the scroll windowing
//     (popupRowWindow), and — since the column contract below — the COLUMN ALIGNMENT of the rows
//     themselves: callers hand over the FULL row list plus the global selected index, and
//     renderPopup lays the rows out, windows around the selection, and truncates every content
//     line to the inner budget, so no line can ever wrap the box.
//   - A row is a popupRow — a slice of CELLS, not a pre-concatenated string — and the module lays
//     the cells of every row out as vertically aligned columns (layoutPopupRows): each column is
//     as wide as its widest cell measured in DISPLAY cells over ALL rows (not just the visible
//     window, so alignment never shifts while scrolling), adjacent columns are separated by a
//     two-space gutter, a column empty in every row collapses to nothing, and the composed line is
//     right-trimmed before the marker/highlight/truncate steps run on it. Each popup kind picks a
//     fixed column schema and leaves an absent tier as an empty cell, which pads like any other so
//     the later columns stay aligned; grammar separators ("— ", "· ") lead the cell they belong to,
//     so the separator glyphs line up too. Truncation is whole-row, never per-column. A
//     single-cell row (singleCellRows) is the degenerate case and renders exactly as it did before
//     columns existed. Cells arrive escape-stripped, as they always did.
//   - The optional body block (spec.body) sits between the title and the rows and carries prose the
//     rows cannot: it is the ONE part the module word-wraps rather than truncates, because a
//     question or an approval reason/args body must break across lines instead of losing its tail
//     to an ellipsis. Embedded newlines are honoured as layout (the pretty-printed args'
//     indentation and blank separators survive), each segment is word-wrapped to the inner budget,
//     and the flattened block is capped at spec.maxBodyRows (≤ 0 = uncapped): past the cap the last
//     row becomes an explicit faint "… (+N more lines)" marker counting the hidden lines, so the
//     body never exceeds its cap and truncation is never silent. wrapText is ANSI-unaware, so body
//     arrives PLAIN and escape-stripped — the module wraps first and styles after.
//
// Every overlay pane — the /sessions browser, the command/file/skill dropdowns, and the ask and
// approval prompts — now paints through this module; no boxed overlay renders its own chrome
// (plan D3).

// popupRow is one row of a popup as its columns: the escape-stripped cells the module lays out
// into vertically aligned columns. Every row of one spec follows that popup kind's fixed column
// schema — an absent optional tier is an empty cell, which still pads, so the columns after it
// stay aligned. A one-cell row (singleCellRows) is the degenerate case: one column, laid out as
// the plain label it holds.
type popupRow []string

// popupSpec describes one boxed selector popup. title and hint each drop their row when empty;
// body is plain, escape-stripped prose the module word-wraps to the inner budget and caps at
// maxBodyRows (an empty body adds no rows); rows are the escape-stripped cell rows the module
// aligns into columns; selected indexes rows (−1 = no highlight); maxRows caps the scroll window
// around the selection (≤ 0 shows every row).
type popupSpec struct {
	title       string
	body        string
	maxBodyRows int
	rows        []popupRow
	selected    int
	hint        string
	maxRows     int
}

// renderPopup paints the bordered selector pane described by spec at the given TOTAL width
// (lipgloss v2 folds the border and padding into width, so every returned line is exactly width
// cells). The inner content budget follows the border style — like renderStartupBox — rather
// than a hard-coded frame, and every content line (title, rows, hint) is truncated to it so none
// can wrap the box. The selected row within the scrolled window carries the glyphUser marker and
// the full-bar highlight; the others render faint.
func renderPopup(th theme, spec popupSpec, width int) string {
	frame := th.popupBorder.GetHorizontalFrameSize()
	if width <= frame {
		// No room for even one content cell inside the border + padding: lipgloss cannot render a
		// bordered box narrower than frame+1, so a box here would overflow the View. Degrade to
		// nothing instead — the same way footerView blanks below 3 columns (plan D3).
		return ""
	}
	inner := max(1, width-frame)

	// Every non-highlight line is padded to the full inner width on a solid-black field: the outer
	// border style only paints its own border and padding cells, so a line shorter than inner would
	// otherwise leave the gap between its text and the right border on the terminal's default
	// background (a black-hole strip). blackFill closes that — title, plain rows, and hint all read
	// as sitting on the same black pane. The selected row keeps th.userBlock's dark-gray highlight
	// bar (already full-inner-width) as its deliberate selection cue.
	blackFill := lipgloss.NewStyle().Background(colBlack).Width(inner)

	lines := make([]string, 0, len(spec.rows)+2) //nolint:mnd // +2: the optional title and hint rows
	if spec.title != "" {
		lines = append(lines, blackFill.Render(th.presentTitle.Render(truncateToWidth(spec.title, inner))))
	}

	if spec.body != "" {
		lines = append(lines, popupBodyLines(th, spec.body, spec.maxBodyRows, inner, blackFill)...)
	}

	// The columns are measured and padded over the WHOLE row list before any windowing, so a row
	// scrolled out of view still holds its column open and the alignment never shifts as the
	// selection moves through a long list.
	rows := layoutPopupRows(spec.rows)

	capRows := spec.maxRows
	if capRows <= 0 {
		capRows = len(rows) // ≤ 0 shows every row (popupRowWindow returns [0, total) when total ≤ cap)
	}
	start, end := popupRowWindow(spec.selected, len(rows), capRows)
	for i := start; i < end; i++ {
		selected := i == spec.selected
		marker := "  "
		if selected {
			marker = glyphUser + " "
		}
		row := truncateToWidth(marker+rows[i], inner)
		if selected {
			lines = append(lines, th.userBlock.Width(inner).Render(row))
		} else {
			lines = append(lines, blackFill.Render(th.statusFaint.Render(row)))
		}
	}

	if spec.hint != "" {
		lines = append(lines, blackFill.Render(th.statusFaint.Render(truncateToWidth(spec.hint, inner))))
	}

	content := lipgloss.JoinVertical(lipgloss.Left, lines...)
	return th.popupBorder.Width(width).Render(content)
}

// popupGutter separates two adjacent popup columns: two spaces, the minimum gap between the widest
// cell of one column and the first cell of the next. It is the same gutter the markdown tables
// (tableGutter) and the start-up info block use, so every aligned block in the UI reads with one
// rhythm rather than three.
const popupGutter = "  "

// layoutPopupRows composes the spec's cell rows into the plain lines the pane paints, one line per
// row, every column padded to a single width so the columns line up vertically down the pane. The
// widths are measured over ALL rows — not just the window popupRowWindow will show — so scrolling
// a long list can never shift a column sideways. Each composed line is right-trimmed: the trailing
// pad of the last column carries no information, and the pane's black fill covers that gap
// already, which is what keeps a single-cell spec byte-identical to the plain labels it was
// composed from.
func layoutPopupRows(rows []popupRow) []string {
	widths := popupColumnWidths(rows)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = layoutPopupRow(row, widths)
	}
	return out
}

// popupColumnWidths measures each column of the spec: the widest cell in it across every row, in
// DISPLAY cells (ansi.StringWidth — a CJK glyph is two cells wide and an escape sequence none),
// never a rune or byte count, so a wide-rune cell cannot under-measure its column and let the row
// spill past the pane's right border. A column no row filled measures 0, which is the signal
// layoutPopupRow collapses it on.
func popupColumnWidths(rows []popupRow) []int {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	widths := make([]int, columns)
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], ansi.StringWidth(cell))
		}
	}
	return widths
}

// layoutPopupRow lays one row's cells into the measured column widths: each cell padded out to its
// column, the columns joined by popupGutter, and the whole line right-trimmed. A column that is
// empty in every row (width 0) collapses entirely — no width and no gutter — so an optional tier
// no row filled costs the pane nothing; an empty cell in a column another row DID fill still pads,
// which is what keeps the columns after an absent tier aligned. A row shorter than the schema is
// treated as ending in empty cells, so a producer may leave trailing tiers off.
func layoutPopupRow(row popupRow, widths []int) string {
	var b strings.Builder
	written := 0
	for i, w := range widths {
		if w == 0 {
			continue // a column empty in every row collapses: no width, no gutter
		}
		if written > 0 {
			b.WriteString(popupGutter)
		}
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		b.WriteString(cell)
		if pad := w - ansi.StringWidth(cell); pad > 0 {
			b.WriteString(strings.Repeat(" ", pad))
		}
		written++
	}
	return strings.TrimRight(b.String(), " ")
}

// singleCellRows lifts a plain label list into one-cell popup rows — the shape a producer with
// nothing to align (a file suggestion, an ask_user choice) hands the module. One cell is one
// column, so the composed line is the label itself: the contract's promise that a single-cell row
// renders exactly as it did before the popup grew columns.
func singleCellRows(labels []string) []popupRow {
	rows := make([]popupRow, len(labels))
	for i, label := range labels {
		rows[i] = popupRow{label}
	}
	return rows
}

// popupBodyLines word-wraps spec.body into styled, black-filled content lines for the pane, sitting
// between the title and the rows. Each embedded newline is layout the caller composed (the approval
// args' JSON indentation and its blank separator lines), so the block is split on "\n" and each
// segment is word-wrapped to inner independently — an empty segment yields one blank row. When the
// flattened line count exceeds maxBodyRows (> 0), the block keeps the first maxBodyRows−1 lines and
// appends a faint "… (+N more lines)" marker counting the hidden lines, so it never exceeds
// maxBodyRows rows and the truncation is never silent; maxBodyRows ≤ 0 shows every wrapped line.
// Body lines render normal (th.popupBody) — the marker faint (th.statusFaint) — each padded on the
// same black field as every other content line and clipped to inner so, like every popup line, none
// can wrap the box.
func popupBodyLines(th theme, body string, maxBodyRows, inner int, blackFill lipgloss.Style) []string {
	var wrapped []string
	for _, seg := range strings.Split(body, "\n") {
		wrapped = append(wrapped, wrapText(seg, inner)...)
	}

	marker := ""
	if maxBodyRows > 0 && len(wrapped) > maxBodyRows {
		hidden := len(wrapped) - (maxBodyRows - 1)
		wrapped = wrapped[:maxBodyRows-1]
		marker = fmt.Sprintf("… (+%d more lines)", hidden)
	}

	out := make([]string, 0, len(wrapped)+1)
	for _, ln := range wrapped {
		out = append(out, blackFill.Render(th.popupBody.Render(truncateToWidth(ln, inner))))
	}
	if marker != "" {
		out = append(out, blackFill.Render(th.statusFaint.Render(truncateToWidth(marker, inner))))
	}
	return out
}

// popupRowWindow returns the [start, end) slice of a list of total rows to show at once, capped
// at capRows and scrolled to keep the selection roughly centred so a long list never overflows
// the pane.
func popupRowWindow(selected, total, capRows int) (int, int) {
	if total <= capRows {
		return 0, total
	}
	start := selected - capRows/2
	if start < 0 {
		start = 0
	}
	if start+capRows > total {
		start = total - capRows
	}
	return start, start + capRows
}

// truncateToWidth clips s to at most width DISPLAY cells, ending in an ellipsis when it had to cut
// — so a long file path, a wide-rune title, or an aligned row too wide for the pane never
// overflows the terminal and breaks the overlay's layout. The measure is ansi.StringWidth, not a
// rune count: a CJK glyph occupies two terminal cells and an escape sequence none, so counting
// runes would let a wide row spill past the right border while measuring as if it fit — the exact
// bug the pane's every-line-is-exactly-width contract cannot survive. A width of 1 or less leaves
// no room for text beside the ellipsis, so the line degrades to nothing rather than to a lone "…".
func truncateToWidth(s string, width int) string {
	if width <= 1 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, width, "…")
}
