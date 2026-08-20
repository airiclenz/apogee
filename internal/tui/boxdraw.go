package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
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
			lines = append(lines, squareLine(m.th.measure, ln, m.width))
		}
	}
	return strings.Join(lines, "\n")
}
