package tui

import (
	"strings"
	"sync"

	lipgloss "charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Box and join paint primitives
// ----------------------------------------------------------------------------

// squareLine makes one composed line occupy exactly w columns WHEN PAINTED: short lines are padded
// with blanks, and a line that would run past w is cut (ANSI-aware, no ellipsis — a cut here means
// something upstream broke the cap, and adding a … would only spend another column).
//
// It measures with the width authority (width.go) rather than lipgloss.Width, and that is the whole
// reason it exists. Every squaring primitive the charm stack offers — lipgloss.JoinVertical and
// JoinHorizontal, Style.Width, the viewport's own Width() padding
// (bubbles/v2@v2.1.0/viewport/viewport.go:743-746) — pads to a width measured in ansi.GraphemeWidth,
// which is not the measure the terminal paints in unless it answered mode 2027. A row carrying a
// VS16 grapheme is padded one cell short of where it paints, and everything the layout hangs off
// that row's right edge moves with it.
func squareLine(measure widthAuthority, s string, w int) string {
	over := measure.Width(s) - w
	if over > 0 {
		return measure.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", -over)
}

// squareOnField is squareLine with the pad painted on field rather than left bare — the same split
// lipgloss makes between the run it styles as text and the run it styles as whitespace
// (lipgloss/v2@v2.0.4/align.go:12-56). It matters wherever the surface is a solid field: a content
// line carries an SGR reset of its own at its end, so a bare pad after it would show the terminal's
// background through the gap between the text and the box's right edge.
func squareOnField(measure widthAuthority, field lipgloss.Style, s string, w int) string {
	over := measure.Width(s) - w
	if over > 0 {
		return measure.Truncate(s, w, "")
	}
	return s + field.Render(strings.Repeat(" ", -over))
}

// drawBox frames content in style's border and horizontal padding and returns the box's rows: one
// string per PAINTED row, each exactly width columns in the authority's measure, ONE row per line of
// content it was handed.
//
// It stands in for style.Width(width).Render(…), and for squareLine's reason one level up. A
// lipgloss Width does not merely pad: past its width it WRAPS, and it measures in GraphemeWidth
// whatever the painter is doing (ADR 0030 §5). So a composed row the authority calls exactly the
// box's inner width can be wider than that to lipgloss — any VARIATION SELECTOR-16 cluster makes it
// so — and the style folds that ONE composed row into TWO painted rows: the row's tail moves to a
// line of its own, the box grows past the row budget it was drawn for, and the fold leaves rows that
// stop short of the box's right border. Drawing the rows here keeps one composed row one painted row
// at every width, under either measure.
//
// Only the shape the two boxed surfaces use is honoured — a four-sided border, horizontal padding,
// and the field colour that padding sits on — because that is the whole of what theme.go's
// startupBorder and popupBorder carry. Where the width cannot pay for the padding the PADDING gives
// way before the border does, so a box that is drawn at all is exactly width columns wide; narrower
// than its own two border glyphs there is no box to draw and the answer is no rows.
//
// A surface that would rather carry its name IN the top border than on a row of its own asks
// drawTitledBox for it; this is that same function with nothing to splice.
func drawBox(measure widthAuthority, style lipgloss.Style, content []string, width int) []string {
	return drawTitledBox(measure, style, content, width, "", lipgloss.NewStyle())
}

// drawTitledBox is drawBox with a name spliced INTO the top border — ╭────── Approve terminal? ──────╮
// — for a surface that would rather spend the row on what the human is deciding about than on a
// heading of its own (popupSpec.titleInBorder). The title is centred between two runs of the
// border's own rune with one space each side, so the box still closes itself at exactly width
// columns: the corners, the two dash runs, and the label are measured in the authority the rest of
// the box is drawn in, never in lipgloss's (ADR 0030). An EMPTY title is the plain border drawBox
// has always drawn, which is what makes drawBox exactly this function with nothing to splice — one
// border assembly, so a titled box and a plain one cannot drift apart. A title too wide for the
// span is dropped rather than overflowed; every caller fits it first (renderPopup composes it TO
// the pane's inner width), so the fallback is a backstop and not a silent elision.
//
// titleStyle is how the name is painted; it is re-backgrounded onto the top border's own field so
// the spliced label carries a background like every other cell of the box (the pane is filled
// solid black — a bare cell in the middle of the border would read as a hole in it).
func drawTitledBox(measure widthAuthority, style lipgloss.Style, content []string, width int, title string, titleStyle lipgloss.Style) []string {
	edges := style.GetHorizontalBorderSize()
	if width < edges {
		return nil
	}
	border := style.GetBorderStyle()
	span := width - edges // the columns between the two vertical border glyphs
	padLeft := min(style.GetPaddingLeft(), span)
	padRight := min(style.GetPaddingRight(), span-padLeft)
	inner := span - padLeft - padRight

	// Each edge takes its own colours off the style, and the padding takes the box's field, so a
	// transparent card (startupBorder) and a solid-black pane (popupBorder) both come out of the one
	// routine. An unset colour reads back as lipgloss.NoColor, which renders as no sequence at all.
	top := lipgloss.NewStyle().Foreground(style.GetBorderTopForeground()).Background(style.GetBorderTopBackground())
	bottom := lipgloss.NewStyle().Foreground(style.GetBorderBottomForeground()).Background(style.GetBorderBottomBackground())
	left := lipgloss.NewStyle().Foreground(style.GetBorderLeftForeground()).Background(style.GetBorderLeftBackground())
	right := lipgloss.NewStyle().Foreground(style.GetBorderRightForeground()).Background(style.GetBorderRightBackground())
	field := lipgloss.NewStyle().Background(style.GetBackground())

	lead := left.Render(border.Left) + field.Render(strings.Repeat(" ", padLeft))
	tail := field.Render(strings.Repeat(" ", padRight)) + right.Render(border.Right)

	topRow := top.Render(border.TopLeft + strings.Repeat(border.Top, span) + border.TopRight)
	if title != "" {
		label := " " + title + " " // one space each side: the name is set off the border, not welded to it
		if w := measure.Width(label); w <= span {
			dashes := (span - w) / 2 // centred, an odd cell going to the right — the mockup's own drawing
			topRow = top.Render(border.TopLeft+strings.Repeat(border.Top, dashes)) +
				titleStyle.Background(style.GetBorderTopBackground()).Render(label) +
				top.Render(strings.Repeat(border.Top, span-w-dashes)+border.TopRight)
		}
	}

	rows := make([]string, 0, len(content)+2) //nolint:mnd // +2: the top and bottom border rows
	rows = append(rows, topRow)
	for _, ln := range content {
		rows = append(rows, lead+squareOnField(measure, field, ln, inner)+tail)
	}
	return append(rows, bottom.Render(border.BottomLeft+strings.Repeat(border.Bottom, span)+border.BottomRight))
}

// scrollbarThumb places the position marker on a scroll bar: given the window a surface shows
// (start, window) over the whole list (total) and the height the bar is painted at, it answers where
// the thumb starts and how many painted lines it spans. Both bars are placed from it — the
// transcript's gutter ([Model.renderScrollbar]) and a popup row block's overflow bar
// (popupRowScrollbar).
//
// The two counts a scroll bar conflates are separate parameters here, because a painted LINE of the
// bar is not always a ROW of the list. In the transcript a line is a row and the two coincide; a
// popup row can cost several lines. So the thumb is SIZED from the row counts — the seated window
// over the whole list — and DRAWN in the bar's painted lines. The window's position maps the same
// way: start over the rows the list cannot show at once, onto the lines the thumb leaves free. That
// puts the thumb flush at the top with the first row seated and flush at the bottom with the last
// one, at every height in between the surface can be drawn at, and never smaller than one line, so a
// list long enough to round the thumb away still shows where the view sits.
//
// A window that shows the whole list, or a bar with no lines to paint, has no position to describe:
// the answer is a zero-length thumb. Callers paint that case themselves — a blank gutter, an
// unbarred block — before they ever ask.
func scrollbarThumb(start, window, total, height int) (pos, size int) {
	if height <= 0 || window <= 0 || window >= total {
		return 0, 0
	}
	size = clampInt(height*window/total, 1, height)
	// start is clamped the way the viewport clamps its own scroll percent: a window reported past the
	// end of the list seats the thumb flush at the bottom rather than off the end of the bar.
	pos = clampInt(start, 0, total-window) * (height - size) / (total - window) // 0 … height-size
	return pos, size
}

// scrollbarCells paints a scroll bar's column: one cell per bar line, the thumb's lines (placed by
// scrollbarThumb from the same four counts) in th.scrollThumb and the rest of the groove in
// th.scrollTrack. It is the one painter of the stroke the transcript's gutter and the prompt box's
// bar both draw; a window that shows the whole list comes back as bare track, so a caller that
// keeps a blank gutter there asks for it before calling.
func scrollbarCells(th theme, start, window, total, height int) []string {
	cells := make([]string, max(0, height))
	pos, size := scrollbarThumb(start, window, total, height) // pos: 0 (top) … height-size (bottom)
	for i := range cells {
		if i >= pos && i < pos+size {
			cells[i] = th.scrollThumb.Render(glyphScrollThumb)
		} else {
			cells[i] = th.scrollTrack.Render(glyphScrollTrack)
		}
	}
	return cells
}

// joinScrollbar hangs the scroll-bar gutter off the right edge of the transcript body: every body
// row is squared to the viewport's width first (squareLine), then its bar cell is appended.
//
// It stands in for the lipgloss.JoinHorizontal this used to be. JoinHorizontal pads each block's
// rows to that block's widest row in GraphemeWidth, so on a terminal painting in WcWidth the ⚠️ row
// reached the gutter a column short and dropped its bar cell one column left of every other row's.
// Doing the padding here, in the painter's measure, is what makes the bar a straight column.
func (m Model) joinScrollbar(body, bar string) string {
	if bar == "" {
		return body
	}
	rows := strings.Split(body, "\n")
	barRows := strings.Split(bar, "\n")
	if n := len(barRows); n > len(rows) {
		rows = append(rows, make([]string, n-len(rows))...) // a bar taller than the body keeps its cells
	}
	w := max(0, m.viewport.Width())
	for i, row := range rows {
		row = squareLine(m.th.measure, row, w)
		if i < len(barRows) {
			row += barRows[i]
		}
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

// joinColumn hangs a column off the right edge of a run of stacked blocks and returns them as one
// block: every physical line of blocks is squared to w in the painter's measure (squareLine), then
// the column's row of the same index is appended. A line past the column's last row keeps its w
// columns and nothing after them; a column row past the blocks' last line is not drawn.
//
// It is joinScrollbar's reason one block wider: a lipgloss.JoinHorizontal or a Style.Width would
// pad the left side to a width measured in GraphemeWidth, so on a terminal painting in WcWidth a
// row carrying a VS16 grapheme would start the column a cell left of every other row's (ADR 0030).
// The side panel's column hangs here, off the rows above the session-name rule (View).
func joinColumn(measure widthAuthority, blocks []string, w int, column []string) string {
	lines := make([]string, 0, len(column))
	for _, block := range blocks {
		lines = append(lines, strings.Split(block, "\n")...)
	}
	for i, ln := range lines {
		ln = squareLine(measure, ln, w)
		if i < len(column) {
			ln += column[i]
		}
		lines[i] = ln
	}
	return strings.Join(lines, "\n")
}

// joinFrame stacks the frame's blocks into the one string the View hands bubbletea, squaring every
// physical line to the window width in the painter's measure.
//
// It stands in for the lipgloss.JoinVertical this used to be, for the same reason joinScrollbar
// stands in for JoinHorizontal: JoinVertical left-aligns by padding every row out to the widest row
// it was given, measured in GraphemeWidth. That is the window width right up until one block
// measures wider than the window in that measure — which is exactly what a frame squared for a
// WcWidth painter does — and then the pad it applies to EVERY OTHER row pushes the whole frame past
// the terminal's last column. Squaring to m.width instead makes layout.md's absolute width cap hold
// at the layer the terminal actually reads, whichever measure that is.
func (m Model) joinFrame(blocks []string) string {
	lines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		for _, ln := range strings.Split(block, "\n") {
			lines = append(lines, sealRowEdges(m.th.measure, squareLine(m.th.measure, ln, m.width), m.width))
		}
	}
	return strings.Join(lines, "\n")
}

// rowEdgeBuffers holds the one-row screen buffers [sealRowEdges] parses a line into. A pool rather
// than a Model field or a package-level buffer: View runs on value copies of the Model, in parallel
// under test, and the theme it paints with is shared and immutable (doc.go), so the scratch a parse
// writes into belongs to the one call that took it.
var rowEdgeBuffers = sync.Pool{New: func() any {
	buf := uv.NewScreenBuffer(0, 1)
	return &buf
}}

// sealRowEdges returns ln — one frame line, already squared to w columns — with its last cell, and
// separately its first, written as a concealed space when that cell is a painted blank: a width-1
// space with a background set. Every other line comes back unchanged and unallocated, and so does
// every cell but the two edges, which keep their content (a space, so copied text is unchanged) and
// their style, conceal added.
//
// It exists because of how ultraviolet's renderer finishes a row. A row whose trailing run is
// blanks it may clear with (canClearWith: a plain space, no underline, no link, no attribute
// beyond bold, faint, italic or blink) is not written out: clearToEnd sends EL 0 with that run's
// pen set, and transformLine's leading-run path sends EL 1 the same way. That is correct only on a
// terminal that erases in the current background (bce). Terminal.app erases in its own background
// instead, so every painted field reaching the right border — the bottom chrome, a sent prompt
// block, the breadcrumb, the staged band — showed the terminal's background through its tail
// there and nowhere else. Conceal (SGR 8) is outside canClearWith's set, so a sealed edge makes
// the renderer write the row's run as characters, and a concealed space paints its background like
// any other. Mid-row runs the renderer sends as ECH render correctly and are left alone; an
// unpainted edge (no background) erases in the default pen and is left alone too.
//
// The decision reads ultraviolet's own parse of the line, the one the renderer runs, and widths go
// through measure as squareLine's do. Only a line whose bytes carry a background SGR is parsed.
func sealRowEdges(measure widthAuthority, ln string, w int) string {
	if w <= 0 || !carriesBackgroundSGR(ln) {
		return ln
	}
	buf := rowEdgeBuffers.Get().(*uv.ScreenBuffer)
	defer rowEdgeBuffers.Put(buf)
	if buf.Width() != w {
		buf.Resize(w, 1)
	}
	buf.Method = measure.Method()
	uv.NewStyledString(ln).Draw(buf, buf.Bounds())

	first, last := buf.CellAt(0, 0), buf.CellAt(w-1, 0)
	if w == 1 {
		if isPaintedBlank(last) {
			return sealedBlank(last.Style)
		}
		return ln
	}
	if isPaintedBlank(last) {
		ln = measure.Truncate(ln, w-1, "") + sealedBlank(last.Style)
	}
	if isPaintedBlank(first) {
		ln = sealedBlank(first.Style) + measure.Cut(ln, 1, w)
	}
	return ln
}

// isPaintedBlank reports whether c is a cell ultraviolet's renderer would leave to an erase on a
// painted field: a width-1 space with a background set, not already concealed.
func isPaintedBlank(c *uv.Cell) bool {
	return c != nil && c.Width == 1 && c.Content == " " && c.Style.Bg != nil &&
		c.Style.Attrs&uv.AttrConceal == 0
}

// sealedBlank is one space in st with conceal added, reset on both sides: uv's Style.String emits
// no reset of its own, so without the leading one the line's open attributes (an underline, say)
// would carry into the cell, and without the trailing one the conceal would carry past it.
func sealedBlank(st uv.Style) string {
	st.Attrs |= uv.AttrConceal
	return ansi.ResetStyle + st.String() + " " + ansi.ResetStyle
}

// carriesBackgroundSGR reports whether ln holds an SGR sequence that may set a background: a 48
// (extended colour), 40–47 or 100–107 parameter. It is the cheap gate in front of
// [sealRowEdges]'s parse, so it errs towards yes — a colour index or channel value that happens to
// read 41 passes it, and the parse then decides — and never answers no for a painted line.
func carriesBackgroundSGR(ln string) bool {
	for {
		start := strings.Index(ln, csiIntroducer)
		if start < 0 {
			return false
		}
		ln = ln[start+len(csiIntroducer):]
		end := strings.IndexFunc(ln, func(r rune) bool { return !isSGRParamByte(r) })
		if end < 0 {
			return false
		}
		if ln[end] == sgrFinal && sgrSetsBackground(ln[:end]) {
			return true
		}
		ln = ln[end:]
	}
}

// csiIntroducer opens a control sequence, and sgrFinal closes one that is an SGR.
const (
	csiIntroducer = "\x1b["
	sgrFinal      = 'm'
)

// isSGRParamByte reports whether r may sit in an SGR's parameter string: a digit, or the ';' and ':'
// separators.
func isSGRParamByte(r rune) bool {
	return r >= '0' && r <= '9' || r == ';' || r == ':'
}

// sgrSetsBackground reports whether an SGR's parameter string holds a parameter that may set the
// background: 48, 40–47 or 100–107.
func sgrSetsBackground(params string) bool {
	for param := range strings.FieldsFuncSeq(params, func(r rune) bool { return r == ';' || r == ':' }) {
		if param == "48" ||
			len(param) == 2 && param[0] == '4' && param[1] <= '7' ||
			len(param) == 3 && param[:2] == "10" && param[2] <= '7' {
			return true
		}
	}
	return false
}
