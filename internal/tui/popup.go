package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
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
//   - width is the TOTAL box width: the rounded border and the padding fold INTO width, so every
//     rendered line is exactly width display cells in the painter's own measure, and the pane is
//     exactly as many painted rows as it composed (drawBox, like renderStartupBox). Callers pass
//     m.width — the full window width, the same value the input box spans — so the pane's right
//     border lands on the terminal's last column, flush with the prompt box below it.
//   - The pane is filled solid black: the border style paints its border and padding cells black,
//     and every content line is padded out to the full inner width on the same black field, so no
//     interior cell (including the gap after a short row) is left on the terminal background.
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
//     and the flattened block is capped at spec.maxBodyRows — negative = uncapped, ZERO = no body
//     rows at all, the same sense maxRows carries. Past a positive cap the last row becomes an
//     explicit faint "… (+N more lines)" marker counting the hidden lines, so the body never
//     exceeds its cap and truncation is never silent. wrapText is ANSI-unaware, so body arrives
//     PLAIN and escape-stripped — the module wraps first and styles after.
//   - HIDING CONTENT IS NEVER SILENT, at any budget OR any width — and that holds for the ROWS as
//     much as for the body. A cap of zero leaves no row for the marker either, so the pane says it
//     on the row it always has: the marker moves onto the TITLE row (popupTitleLine) rather than
//     the content vanishing without a word. That is what lets a pane shrink to its irreducible four
//     rows on a 12-row terminal (popupBudget) and still be honest — the approval prompt is a
//     security surface, and a decision must never be taken against text the pane dropped quietly.
//     The marker rides the title only when the block that owes it has no row of its own to put it
//     on: with one body row it is the body's own last line, exactly as before, and a row window
//     granted at least one row SCROLLS around the selection (popupRowWindow), so the rows outside
//     it are reachable rather than hidden. A row budget of ZERO is the case scrolling cannot
//     answer — every choice or entry gone while the hint still offers ↑↓ — so those rows are
//     counted onto the title as well, in the SAME marker and the same wording: what the pane holds
//     and is not showing is one fact, and a title row too narrow to seat one count has no room for
//     two. A pane too narrow to seat the phrase beside its name sheds the phrase's NOUN before the
//     count (popupElisionMarkerFitting) and its own name before the number, so the count survives
//     every width a pane can be drawn at — a short window is usually a narrow one too.
//
// Every overlay pane — the /sessions browser, the command/file/skill dropdowns, and the ask and
// approval prompts — now paints through this module; no boxed overlay renders its own chrome
// (plan D3).

// popupChrome is a pane's irreducible height: its two borders, its title row and its hint row. It
// is what renderPopup draws for a spec whose every budget is zero — the smallest a pane can be and
// still name itself and say how to act on it — so it is also the floor the frame's row allocation
// hands out before any surface gets a comfortable row ([Model.frameRowPlan]). A pane the frame
// cannot give this many rows is not drawn at all.
const popupChrome = 4

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
// around the selection. The two caps read the same way: negative shows everything, and ZERO shows
// nothing — a window with no rows left to spare saying so (popupBudget) rather than the pane
// quietly showing all of them. Zero is the reading that keeps a pane inside the shortest window it
// can be drawn in at all, where its border, title and hint are the whole budget — and where a
// dropped body AND a dropped row list are both reported on the title row instead
// (popupTitleLine), so the cap costs the prose and the choices but never the knowledge that there
// are some.
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

	// Every content line sits on a solid-black field: the outer border style only paints its own
	// border and padding cells, so a line whose own styles carry no background would otherwise show
	// the terminal's default background through the pane (a black-hole strip). blackFill closes that
	// — title, plain rows, and hint all read as sitting on the same black pane — and drawBox pads
	// each line out to the full inner width on that same field. The selected row keeps th.userBlock's
	// dark-gray highlight bar, squared to the inner width itself, as its deliberate selection cue.
	//
	// It is deliberately NOT a Width style any more. lipgloss pads — and past its width WRAPS — in
	// GraphemeWidth whatever the painter is doing, so a line the authority calls exactly inner cells
	// wide can measure wider to lipgloss (any VARIATION SELECTOR-16 cluster does) and a Width style
	// would fold that one pane row into two: the pane outgrows the row budget popupBudget granted it
	// and neither half of the folded row reaches the pane's right border (ADR 0030 §5).
	blackFill := lipgloss.NewStyle().Background(colBlack)

	// BOTH content blocks are composed BEFORE the title row is written, because what they could not
	// fit changes what that row says: a pane granted no body rows and no row window reports those
	// elisions on its title, the one row it still has (popupTitleLine). Composing in the other order
	// would mean either a silent drop or a fifth row the frame has not got.
	body, hiddenBody := popupBodyLines(th, spec.body, spec.maxBodyRows, inner, blackFill)
	rows, hiddenRows := popupRowLines(th, spec, inner, blackFill)

	// The two counts merge into the ONE marker the title row can seat: a hidden body block and a
	// hidden row list are the same fact about the pane — content it holds and is not showing — and a
	// row narrow enough to make the pane trade wording for width has no room to state it twice. The
	// body's count only lands here when the body block had no row to put its own marker on.
	hidden := hiddenRows
	if len(body) == 0 {
		hidden += hiddenBody
	}

	lines := make([]string, 0, len(body)+len(rows)+2) //nolint:mnd // +2: the optional title and hint rows
	if title := popupTitleLine(th, spec.title, hidden, inner); title != "" {
		lines = append(lines, blackFill.Render(th.presentTitle.Render(truncateToWidth(th, title, inner))))
	}
	lines = append(lines, body...)
	lines = append(lines, rows...)

	if spec.hint != "" {
		lines = append(lines, blackFill.Render(th.statusFaint.Render(truncateToWidth(th, spec.hint, inner))))
	}

	// drawBox rather than th.popupBorder.Width(width).Render: the pane's own rows are DRAWN, squared
	// to the inner width in the painter's measure, so one composed line is always one painted row and
	// the pane is exactly as tall as the frame budgeted for it. lipgloss.JoinVertical went with it —
	// it left-aligned by padding every row out to the widest row IT measured, which is the same
	// GraphemeWidth pad one level in.
	return strings.Join(drawBox(th.measure, th.popupBorder, lines, width), "\n")
}

// popupGutter separates two adjacent popup columns: two spaces, the minimum gap between the widest
// cell of one column and the first cell of the next. A pop-up's columns are told apart by that gap
// alone — no rule between them, unlike a markdown table's (mdtable.go) — because a pane already
// has a border of its own and a second stroke inside it would read as a grid.
const popupGutter = "  "

// layoutPopupRows composes the spec's cell rows into the plain lines the pane paints, one line per
// row, every column padded to a single width so the columns line up vertically down the pane. The
// widths are measured over ALL rows — not just the window popupRowWindow will show — so scrolling
// a long list can never shift a column sideways. Each composed line is right-trimmed: the trailing
// pad of the last column carries no information, and the pane's black fill covers that gap
// already, which is what keeps a single-cell spec byte-identical to the plain labels it was
// composed from.
func layoutPopupRows(th theme, rows []popupRow) []string {
	widths := popupColumnWidths(th, rows)
	out := make([]string, len(rows))
	for i, row := range rows {
		out[i] = layoutPopupRow(th, row, widths)
	}
	return out
}

// popupColumnWidths measures each column of the spec: the widest cell in it across every row, in
// DISPLAY cells (th.measure, the package's width authority — a CJK glyph is two cells wide and an
// escape sequence none), never a rune or byte count, so a wide-rune cell cannot under-measure its
// column and let the row spill past the pane's right border. The authority rather than
// ansi.StringWidth because the pad computed from this width is painted, and a column measured in a
// measure the painter is not on lands a cell off on any row carrying VARIATION SELECTOR-16
// (ADR 0030). A column no row filled measures 0, which is the signal layoutPopupRow collapses it on.
func popupColumnWidths(th theme, rows []popupRow) []int {
	columns := 0
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	widths := make([]int, columns)
	for _, row := range rows {
		for i, cell := range row {
			widths[i] = max(widths[i], th.measure.Width(cell))
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
func layoutPopupRow(th theme, row popupRow, widths []int) string {
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
		if pad := w - th.measure.Width(cell); pad > 0 {
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

// popupRowLines composes the spec's rows into the styled, black-filled content lines the pane
// paints — the selected row within the scroll window carrying the glyphUser marker and the
// full-bar highlight, the others faint — and reports how many rows it is NOT showing.
//
// The columns are measured and padded over the WHOLE row list before any windowing, so a row
// scrolled out of view still holds its column open and the alignment never shifts as the selection
// moves through a long list. Every composed line is truncated to inner, so a wide row can never
// wrap the box.
//
// The hidden count is ALL-OR-NOTHING on purpose, and it is not the body's rule with the words
// changed. A window granted at least one row scrolls around the selection (popupRowWindow): the
// rows outside it are one keypress away, so they are off-screen rather than hidden, and a marker
// counting them would cost a row of the very list it is describing. A window granted ZERO rows is
// the case scrolling cannot answer — every choice or entry gone, no way to reach one, and a hint
// still offering ↑↓ to select among them — so THAT is what the pane owes an accounting for, and
// renderPopup carries the count to the title row (popupTitleLine), the one row the pane always has.
func popupRowLines(th theme, spec popupSpec, inner int, blackFill lipgloss.Style) ([]string, int) {
	rows := layoutPopupRows(th, spec.rows)

	capRows := spec.maxRows
	if capRows < 0 {
		capRows = len(rows) // negative shows every row (popupRowWindow returns [0, total) when total ≤ cap)
	}
	start, end := popupRowWindow(spec.selected, len(rows), capRows)
	if start == end {
		// No row on the screen: with rows on offer this is the budget's call, and the pane owes the
		// human the count (an empty offering owes nothing — there is no list to hide).
		return nil, len(rows)
	}

	out := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		selected := i == spec.selected
		marker := "  "
		if selected {
			marker = glyphUser + " "
		}
		row := truncateToWidth(th, marker+rows[i], inner)
		if selected {
			// Squared in the authority's measure before the style is applied, the way renderUserBlock
			// pads its own rows: the highlight bar spans the full inner width on the block's OWN
			// dark-gray field rather than the pane's black, and a lipgloss Width would fold the row
			// instead of padding it whenever the two measures disagree about it (ADR 0030 §5).
			out = append(out, th.userBlock.Render(squareLine(th.measure, row, inner)))
		} else {
			out = append(out, blackFill.Render(th.statusFaint.Render(row)))
		}
	}
	return out, 0
}

// popupBodyLines word-wraps spec.body into styled, black-filled content lines for the pane, sitting
// between the title and the rows, and reports how many wrapped lines it is NOT showing. Each
// embedded newline is layout the caller composed (the approval args' JSON indentation and its blank
// separator lines), so the block is split on "\n" and each segment is word-wrapped to inner
// independently — an empty segment yields one blank row. When the flattened line count exceeds
// maxBodyRows (> 0), the block keeps the first maxBodyRows−1 lines and appends a faint
// "… (+N more lines)" marker counting the hidden lines — the short "… +N" form on a pane too narrow
// to seat that phrase (popupElisionMarkerFitting), so the count is stated rather than clipped — so
// it never exceeds maxBodyRows rows and the truncation is never silent; a NEGATIVE maxBodyRows
// shows every wrapped line and ZERO shows none at all. Body lines render normal (th.popupBody) —
// the marker faint (th.statusFaint) — each padded on the same black field as every other content
// line and clipped to inner so, like every popup line, none can wrap the box.
//
// The hidden count is returned rather than kept private because a budget of ZERO leaves no row to
// put the marker on: the block is empty, the count is the whole body, and renderPopup carries the
// marker up to the title row (popupTitleLine). So "hidden > 0 with no lines" is the pane's signal
// that it owes the human a word about prose it cannot show, not a licence to drop it quietly.
func popupBodyLines(th theme, body string, maxBodyRows, inner int, blackFill lipgloss.Style) ([]string, int) {
	if body == "" {
		return nil, 0
	}

	var wrapped []string
	for _, seg := range strings.Split(body, "\n") {
		wrapped = append(wrapped, wrapText(th, seg, inner)...)
	}

	if maxBodyRows == 0 {
		// A window with nothing left to spend on prose (popupBudget), not an invitation to show all
		// of it: honouring the budget is what keeps the pane inside the frame it is drawn in. The
		// lines are still COUNTED — dropping them is the budget's call, hiding the fact is nobody's.
		return nil, len(wrapped)
	}

	marker := ""
	hidden := 0
	if maxBodyRows > 0 && len(wrapped) > maxBodyRows {
		hidden = len(wrapped) - (maxBodyRows - 1)
		wrapped = wrapped[:maxBodyRows-1]
		marker = popupElisionMarkerFitting(th, hidden, inner)
	}

	out := make([]string, 0, len(wrapped)+1)
	for _, ln := range wrapped {
		out = append(out, blackFill.Render(th.popupBody.Render(truncateToWidth(th, ln, inner))))
	}
	if marker != "" {
		out = append(out, blackFill.Render(th.statusFaint.Render(truncateToWidth(th, marker, inner))))
	}
	return out, hidden
}

// popupElisionMarker is the ONE phrase a pane uses to say prose it holds is not on the screen,
// wherever that phrase has to be shown — the body block's last row, or the title row when the body
// block has no rows at all. One wording, so the same fact never reads as two different ones.
func popupElisionMarker(hidden int) string {
	return fmt.Sprintf("… (+%d more lines)", hidden)
}

// popupElisionMarkerShort is that same phrase at the width a NARROW pane can pay: the count with
// the noun dropped. It keeps the "… +" lead-in a clipped tool result already carries in the
// transcript (outputDetail), so it reads as the same statement made shorter rather than a second
// wording — and it costs some thirteen cells less, which is the difference between a title row that
// seats the count and one that clips it off the end. A pane short enough to have no body row is
// usually a split pane, and a split pane is usually narrow too, so the two squeezes arrive
// together; the noun is what the row can afford to lose, because the row already says which pane it
// belongs to.
func popupElisionMarkerShort(hidden int) string {
	return fmt.Sprintf("… +%d", hidden)
}

// popupElisionMarkerFitting picks the widest form of the marker that fits budget display cells: the
// full phrase where there is room for it, the short form where there is not. It is the ONE place
// the pane trades wording for width, so the body block's last row and the title row shed the same
// words in the same order. When even the short form is wider than the budget the pane is narrower
// than any statement of the fact can be — the caller's truncateToWidth still holds the width
// contract, and the count leads the short form, so what survives the clip is the number rather than
// the noun.
func popupElisionMarkerFitting(th theme, hidden, budget int) string {
	if marker := popupElisionMarker(hidden); th.measure.Width(marker) <= budget {
		return marker
	}
	return popupElisionMarkerShort(hidden)
}

// popupTitleLine composes the pane's title row: the spec's title, plus the elision marker for the
// hidden lines that have no row of their own to be counted on — the body block when it got NO rows,
// the row list when the window granted it NONE, or both together (renderPopup sums them; one fact,
// one marker). This is the fallback that keeps hidden content from ever being silent. On a
// 12–15-row terminal popupBudget grants a prose-bearing pane a body budget of zero and an
// eight-entry offering a row window of zero — a fifth row would push the input box off the frame
// (D2) — so the marker has to ride a row the pane already owns, and the title is the row every pane
// draws. It is deliberately not the hint: the hint is how the human acts on the pane, and on the
// approval prompt that legend is the decision itself.
//
// A title-less spec with hidden content gets the marker AS its title row rather than losing it: no
// caller composes one today (every body-bearing pane titles itself), and if one ever does, the
// honest row is worth more than the row it costs.
//
// NARROWNESS IS NOT AN EXCUSE EITHER. The row is composed TO the pane's inner width rather than
// composed long and clipped to it: clipping put the count at the end of a row it did not fit, so at
// 42 columns and below the name won and the elision went silent again — on a terminal that is both
// short and narrow, which is one split pane, not two unlikely coincidences. So the width is spent
// in the order the row is read for: the pane's name, then the count, then the phrasing around the
// count. Full phrase beside the whole name where both fit; the short form ("… +3") where they do
// not; and only past that is the NAME clipped to an ellipsis, never the number — a decision is
// taken against a name the human can still read, and taken knowing there is text behind it. When
// not even a clipped name survives, the count is the whole row: on a pane that narrow the name is
// no longer identifying anything anyway.
func popupTitleLine(th theme, title string, hidden, inner int) string {
	if hidden == 0 {
		return title
	}
	if title == "" {
		return popupElisionMarkerFitting(th, hidden, inner)
	}
	gutter := th.measure.Width(popupGutter)
	marker := popupElisionMarkerFitting(th, hidden, inner-gutter-th.measure.Width(title))
	if room := inner - gutter - th.measure.Width(marker); th.measure.Width(title) > room {
		if title = truncateToWidth(th, title, room); title == "" {
			return marker
		}
	}
	return title + popupGutter + marker
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
// overflows the terminal and breaks the overlay's layout. The measure is th.measure, the package's
// width authority (width.go), not a rune count: a CJK glyph occupies two terminal cells and an
// escape sequence none, so counting runes would let a wide row spill past the right border while
// measuring as if it fit — the exact bug the pane's every-line-is-exactly-width contract cannot
// survive. It measures AND cuts through the authority rather than through ansi.StringWidth and
// ansi.Truncate, because a width measured in one method and cut in another is the same defect one
// step later (ADR 0030 §3) and the cut is what the painter then draws. A width of 1 or less leaves
// no room for text beside the ellipsis, so the line degrades to nothing rather than to a lone "…".
func truncateToWidth(th theme, s string, width int) string {
	if width <= 1 {
		return ""
	}
	if th.measure.Width(s) <= width {
		return s
	}
	return th.measure.Truncate(s, width, "…")
}
