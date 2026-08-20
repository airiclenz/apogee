package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/airiclenz/apogee/internal/domain"
)

// The split composer — recorded Edit regions as two panes, and nothing else
//
// This file is the SPLIT reading of a diff body (ADR 0052, docs/layout/split-diff-layout.md):
// the before file down the left pane, the after file down the right, each with its own line
// numbers and its own marker column, parted by a damped divider. It is the wide twin of the
// stacked reading diffbody.go builds (stackedDiffLines) — the same regions, the same numbers,
// the same context, arranged across instead of down.
//
// It is a PURE composition module: regions and a width in, styled rows out. Nothing here knows
// what a block, an entry or a fold state is, and nothing here decides WHICH reading a body gets —
// that choice is the painter's, made per paint against the width it holds (splitDiffFits), so a
// resize re-flows the wrap and can flip the reading without any state to keep in step. One
// composer serves every paint path a diff-bodied block can reach, for the same reason
// stackedDiffLines is one builder: the wide reading of a diff must not come to differ per tool or
// per path.

// splitPaneMinCols is the floor the split reading is worth painting at: the columns each pane must
// be able to give the CODE, after its number gutter and its marker column, before two panes beat
// one (ratified call 5, ADR 0052 §3). It is an argument about readable panes rather than a
// terminal width — 40 columns is about where a wrapped line of code still reads as a line rather
// than as a column of fragments — and in practice it lands around a 100-column terminal.
const splitPaneMinCols = 40

// splitPaneDivider is the rule between the two panes, worn in the muted role like the numbers
// beside it (docs/layout/split-diff-layout.md, "Gutters"). Its shape is the sub-agent rail's and
// the table column's, and it is deliberately NOT shared with either const: a pane boundary is a
// third element that moves on its own, and theme.go's glyph block already keeps those apart by
// meaning rather than by shape.
const splitPaneDivider = "│"

// splitDividerCells is what the divider costs the row: the glyph plus one blank column each side,
// so the two panes are parted by air rather than butted against the rule. The right pane's own
// blank is emitted only where that pane has something on the row, which is what keeps a padded
// row from ending in styled whitespace.
const splitDividerCells = 3

// splitMarkerCells is the marker column's width in a pane, the same two cells the stacked reading
// spends (stackedRemovedMarker and its siblings) — the glyph and the space parting it from the
// code. The two readings mark a change identically because they are one body in two arrangements;
// TestSplitDiffMarkerColumnMatchesTheStackedReading pins the pair.
const splitMarkerCells = 2

// splitDiffFits reports whether regions can be painted as two panes in width columns: the code
// column each pane is left with, once the divider is taken off the row and the number gutter and
// the marker column off the pane, is at least splitPaneMinCols wide.
//
// The gutter is measured from the regions themselves — the widest number either pane will show —
// so a diff deep in a long file spends the columns its numbers actually need and a short file
// spends fewer. That is what makes the answer a property of THIS body at THIS width rather than a
// terminal-width threshold, and it is asked again at every paint.
//
// No regions is no split: there is nothing to arrange, and the caller keeps whatever body it was
// presented with (ratified call 9).
func splitDiffFits(regions []domain.EditRegion, width int) bool {
	rows := splitRowPlan(regions)
	if len(rows) == 0 {
		return false
	}
	return splitCodeCells(width, splitNumberGutter(rows)) >= splitPaneMinCols
}

// splitDiffRows composes recorded Edit regions into the styled rows of a split diff, laid out for
// width columns: per region the leading context in both panes, the removed lines left and the
// inserted lines right starting on the same row with the shorter side padding, then the trailing
// context in both panes — and one damped ⋯ rule between two regions that do NOT meet in the file's
// numbering (regionsMeet, the elision question stackedRows asks in the same words).
//
// Rows are composed at PAINT time and against the width authority (th.measure, ADR 0030): the wrap
// is chosen in the measure the painter draws in, so a row is never a column wider than it was
// counted, and no row is composed ahead of the width it is composed for.
//
// It does not enforce the reading: a caller that paints split without asking splitDiffFits gets
// narrow panes rather than a refusal. What it does refuse is the impossible — a width leaving the
// code column under one cell composes nothing at all, so no row this function returns can overrun
// the width it was given (layout.md's absolute cap).
func splitDiffRows(th theme, regions []domain.EditRegion, width int) []string {
	rows := splitRowPlan(regions)
	if len(rows) == 0 {
		return nil
	}
	gutter := splitNumberGutter(rows)
	code := splitCodeCells(width, gutter)
	if code < 1 {
		return nil
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.paint(th, gutter, code)...)
	}
	return out
}

// splitCell is one pane's content on one logical row before any width is known: the line number it
// shows, the marker it wears, the colour its kind gives it, and its text. A cell with no marker is
// a PAD — the row where the other pane has a line and this one has none — and it is the one shape
// the pane painter draws as blank columns.
type splitCell struct {
	number int
	marker string
	kind   detailKind
	text   string
}

// filled reports whether this pane has a line on the row. The marker is the question rather than
// the text, because an inserted or removed EMPTY line is a line like any other and only the pad
// has nothing to say.
func (c splitCell) filled() bool { return c.marker != "" }

// splitRow is one logical row of the split reading: what the before pane shows, what the after
// pane shows, and whether the row is the ⋯ rule that stands between two regions instead of either.
type splitRow struct {
	left  splitCell
	right splitCell
	rule  bool
}

// splitRowPlan lays the regions out as unsized rows, in file order.
//
// Within a region the removed stack and the inserted stack start on the SAME row and the shorter
// side pads (docs/layout/split-diff-layout.md, "Alignment"): the panes are read across as well as
// down, and a replacement whose two halves began on different rows would have to be traced rather
// than seen. Context appears in BOTH panes — it is unchanged, so both files hold it — each pane
// numbering it from its own counter, which is how the two numberings drift apart across a region
// while the rows stay level.
//
// The ⋯ rule is laid between two regions that do not meet, and only there: a tool records
// neighbouring changes as separate regions whose context tiles the lines between them without
// overlap (domain.EditRegion), so regions that meet paint end to end and the result is exactly
// what one merged region would have painted. This is the rule stackedRows draws by the same
// predicate, so the two readings of one body elide in the same places.
func splitRowPlan(regions []domain.EditRegion) []splitRow {
	rows := make([]splitRow, 0, len(regions)*4)
	for i, region := range regions {
		if i > 0 && !regionsMeet(regions[i-1], region) {
			rows = append(rows, splitRow{rule: true})
		}
		before, after := region.BeforeStart, region.AfterStart
		for _, text := range region.Leading {
			rows = append(rows, splitContextRow(before, after, text))
			before, after = before+1, after+1
		}
		for j := range max(len(region.Removed), len(region.Inserted)) {
			var row splitRow
			if j < len(region.Removed) {
				row.left = splitCell{number: before, marker: stackedRemovedMarker,
					kind: detailDiffRemoved, text: region.Removed[j]}
				before++
			}
			if j < len(region.Inserted) {
				row.right = splitCell{number: after, marker: stackedInsertedMarker,
					kind: detailDiffAdded, text: region.Inserted[j]}
				after++
			}
			rows = append(rows, row)
		}
		for _, text := range region.Trailing {
			rows = append(rows, splitContextRow(before, after, text))
			before, after = before+1, after+1
		}
	}
	return rows
}

// splitContextRow is one unchanged line as both panes show it: the same text twice, each pane
// under its own number, neither marked.
func splitContextRow(before, after int, text string) splitRow {
	return splitRow{
		left:  splitCell{number: before, marker: stackedContextMarker, text: text},
		right: splitCell{number: after, marker: stackedContextMarker, text: text},
	}
}

// splitNumberGutter is how many cells a pane spends on its number column: the digits of the widest
// number ANY row shows in EITHER pane, plus the one space parting the number from the marker.
//
// One width for both panes and for the whole body is what makes the numbers a column rather than a
// ragged edge, and measuring across both panes is what keeps the two panes the same width once the
// after file's numbering has drifted past the before file's. The space is counted here rather than
// with the marker because it belongs to the number: a row with no number pads the whole gutter,
// space included.
func splitNumberGutter(rows []splitRow) int {
	widest := 0
	for _, row := range rows {
		widest = max(widest, row.left.number, row.right.number)
	}
	return len(strconv.Itoa(widest)) + 1
}

// splitPaneCells is how wide one pane is in a row of width columns: what is left once the divider
// has taken its three, halved. An odd remainder is left unspent rather than given to one pane —
// two panes of different widths would read as a layout mistake at every row.
func splitPaneCells(width int) int { return (width - splitDividerCells) / 2 }

// splitCodeCells is how much of a pane the CODE gets: the pane less its number gutter and its
// marker column. It is the quantity the width rule is stated in (splitPaneMinCols) and the limit
// every line in either pane is wrapped to, so the two questions cannot drift apart.
func splitCodeCells(width, gutter int) int {
	return splitPaneCells(width) - gutter - splitMarkerCells
}

// paint renders one logical row as the physical rows it takes: one per wrapped line of the taller
// side, with the shorter side padding to keep both panes level (docs/layout/split-diff-layout.md,
// "Wrap, don't clip"). A FILLED cell squares its own rows to the pane (splitCell.paint), so the
// divider stands in one column down the whole body and a tinted row's band reaches the pane's edge;
// what is left here is the PAD cell — the row where this pane has no line at all — which fills its
// columns with untinted blanks, because a pad is the absence of a line rather than a blank one.
//
// The rule row spans both panes and the divider between them, because what it stands for — the
// lines elided between two regions — happened in both files at once.
func (r splitRow) paint(th theme, gutter, code int) []string {
	pane := gutter + splitMarkerCells + code
	if r.rule {
		return []string{th.toolDetail.Render(strings.Repeat(glyphLeaderDot, pane*2+splitDividerCells))}
	}
	left := r.left.paint(th, gutter, code)
	right := r.right.paint(th, gutter, code)
	divider := " " + th.toolDetail.Render(splitPaneDivider)
	out := make([]string, 0, max(len(left), len(right)))
	for i := range max(len(left), len(right)) {
		row := strings.Repeat(" ", pane)
		if i < len(left) {
			row = left[i]
		}
		row += divider
		if i < len(right) {
			row += " " + right[i]
		}
		out = append(out, row)
	}
	return out
}

// paint renders one pane's cell as its own physical rows, each exactly one pane wide: the first
// carries the number and the marker, and every continuation row carries neither — a wrapped line is
// one line, and a second number or a second marker would claim it was two. A pad cell paints
// nothing at all and leaves the caller to fill its columns.
//
// The number wears the muted role OUTSIDE the band and the marker rides INSIDE it, so a diff line's
// tint runs from the marker column to the pane's edge on the first row and on every continuation
// row alike, while the number gutter beside it stays chrome (ratified calls 2 and 3 of
// docs/plans/"2026-08-19 - 05"). The marker is a glyph signal ON the band rather than a colour of
// its own — the mark that still reads on a monochrome pipe and on a terminal that drops backgrounds
// — which is ADR 0052's 2026-08-19 amendment superseding the "the marker travels with the TEXT's
// colour" rationale of its ratified calls 6 and 7.
//
// The row is squared to the pane HERE rather than by the caller, because how a filled cell spends
// its columns is now the cell's own business: renderToRail fills a BANDED style out to the code
// rail from inside the style — blanks appended after it would show the terminal's own background
// through the very band they were added to fill — and squareLine takes a plain row the rest of the
// way, so the divider stands in one column whether the row is tinted or not. Both count in the
// width authority's measure (ADR 0030), so the escapes a style left in the row cost nothing and a
// wide glyph costs the cells the painter will actually spend on it.
func (c splitCell) paint(th theme, gutter, code int) []string {
	if !c.filled() {
		return nil
	}
	style := detailStyle(th, c.kind, true)
	lines := wrapText(th, c.text, code)
	out := make([]string, 0, len(lines))
	for i, ln := range lines {
		number, band := strings.Repeat(" ", gutter), strings.Repeat(" ", splitMarkerCells)+ln
		if i == 0 {
			number, band = th.toolDetail.Render(fmt.Sprintf("%*d ", gutter-1, c.number)), c.marker+ln
		}
		row := number + renderToRail(th, style, band, splitMarkerCells+code)
		out = append(out, squareLine(th.measure, row, gutter+splitMarkerCells+code))
	}
	return out
}
