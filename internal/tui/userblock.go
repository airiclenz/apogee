package tui

import (
	"strings"
	"unicode/utf8"
)

// renderUserBlock renders something the human said as a full-width white-on-dark-gray block: the
// marker on the first line, a hanging two-column indent on wrapped continuation lines, and
// the dark-gray background padded across the whole width on every line. The skills the message
// invoked are shown IN it: spans locates their "/tokens" in text and those very runs are painted
// in the skill violet (userBlockCellSpans), so the record of what the model was given is the
// sentence the human wrote rather than a badge restating it beside them.
//
// marker is the block's lead ("❯ " for a submitted prompt, "⧖ " for a delivered interjection):
// the two are the same voice and so share one shape, and the glyph is the whole of the
// difference the reader needs.
//
// A body that soft-wraps past promptCollapsedRows rows COLLAPSES to that many, the last of them
// truncated to make room for the right-aligned see-more marker counting what is left behind
// (promptSeeMore). expanded is the entry's own view state (transcript.setExpanded) and opens the
// body whole, closed again by the see-less marker the block then trails. Both are paint-time acts
// on a body the entry keeps in full — the trigger is measured against the width being painted, so
// a resize alone can collapse a prompt or open one, and nothing about the entry changes
// (layout.md, "Collapsed and expanded blocks").
//
// The accent is painted onto rows the block SHOWS: a token on a row the collapse hid simply is not
// painted, and one on the truncated row carrying the see-more marker is held inside that row's own
// content (promptMarkerContentCells), so an accent can never reach across the gap and recolour the
// marker. The transcript's drag-selection is shaded onto the composed frame afterwards
// (highlightTranscript, mouse.go), which is what keeps a selected token reading as SELECTED.
//
// The block is a click surface exactly when it has two shapes to move between, and then it is a
// click surface WHOLE: every row it paints is marked targetHeader — the marker row and the see-less
// row among them — because layout.md makes the whole prompt the toggle rather than one line of it.
// The mark is state-INDEPENDENT for the tool block's reason: an expanded prompt keeps it, which is
// the click that closes it again. A body inside the cap marks nothing at all, so a click on an
// ordinary prompt keeps its selection meaning.
func renderUserBlock(th theme, marker, text string, spans []skillSpan, width int, expanded bool) blockPaint {
	// The spans are stated in the text's OWN offsets, so they are re-based before the text is
	// expanded, and the expanded text is what both the wrap and the accent map are handed — a block
	// whose rows held spaces where the spans still counted a tab would light up the wrong run
	// (expandTabs).
	spans = expandTabsInSpans(text, spans)
	text = expandTabs(text)
	var out []string
	trailer := ""
	collapsible := false
	if text != "" {
		body := hangingPrefixes(th, marker, text, width)
		collapsible = len(body) > promptCollapsedRows
		shown, hidden := body, 0
		switch {
		case !expanded:
			shown, hidden = splitAtCap(body, promptCollapsedRows)
		case collapsible:
			trailer = promptMarkerRow(th, "", promptSeeLess, width)
		}
		accents := userBlockCellSpans(th, marker, text, width, spans)
		for i, ln := range shown {
			row, limit := "", th.measure.Width(ln) // limit: the cells of this row the block's own content holds
			if hidden > 0 && i == len(shown)-1 {
				row = promptMarkerRow(th, ln, promptSeeMore(hidden), width)
				limit = min(limit, promptMarkerContentCells(th, promptSeeMore(hidden), width))
			} else {
				// Squared in the authority's measure, the way promptMarkerRow below pads its own row,
				// rather than by a lipgloss Width style: lipgloss pads — and past its width WRAPS — in
				// GraphemeWidth whatever the painter is doing (ADR 0030). Now that wrapText breaks in
				// the painter's measure, a line it calls exactly width cells wide can measure wider to
				// lipgloss (any VARIATION SELECTOR-16 cluster does), and a Width style would fold that
				// one line into two — smuggling a "\n" into a single element of a []string the whole
				// line-oriented renderer counts rows with.
				row = th.userBlock.Render(squareLine(th.measure, ln, width))
			}
			out = append(out, accentRow(th, row, i, accents, limit))
		}
	}
	if trailer != "" {
		out = append(out, trailer)
	}
	kind := targetNone
	if collapsible {
		kind = targetHeader
	}
	var paint blockPaint
	paint.add(out, kind)
	return paint
}

// promptMarkerRow composes one row of a user block that carries a collapse marker near its right
// edge: the row's own content on the left, the highlighted marker held promptMarkerMargin columns
// off the block's right edge, and the block's dark-gray field spanning both the gap before it and
// that margin after — three independently styled segments on one line (the footerContent idiom), so
// the marker keeps its own colour while the row stays a solid block. The margin matters because the
// marker carries a background of its own: run flush to the edge and its highlight would touch the
// block's boundary and read as clipped. content is the unstyled row text ("" for the see-less row,
// which carries none).
//
// The content is truncated with the house ellipsis to leave promptMarkerGap columns clear before
// the marker, which is what makes the collapsed shape exactly promptCollapsedRows rows: the marker
// rides a content row instead of taking one of its own. A width too narrow for the marker itself
// truncates the marker rather than overrunning the block — the row is never wider than the block it
// belongs to, at any width the painter is handed.
func promptMarkerRow(th theme, content, marker string, width int) string {
	inner := max(0, width-promptMarkerMargin) // the columns left once the right margin is reserved
	tail := th.promptToggle.Render(th.measure.Truncate(marker, inner, "…"))
	tw := th.measure.Width(tail)
	content = th.measure.Truncate(content, promptMarkerContentCells(th, marker, width), "…")
	pad := strings.Repeat(" ", max(0, inner-tw-th.measure.Width(content)))
	margin := strings.Repeat(" ", min(promptMarkerMargin, width))
	return th.userBlock.Render(content+pad) + tail + th.userBlock.Render(margin)
}

// promptMarkerContentCells is how many columns of its OWN content a row carrying marker keeps: the
// block's width less the right margin, the marker itself and the gap held clear before it. It is
// the truncation promptMarkerRow applies, named so the accent pass can respect the same bound —
// shade past it and a token would recolour the gap and then the marker, which is apogee talking
// rather than the human (renderUserBlock).
func promptMarkerContentCells(th theme, marker string, width int) int {
	inner := max(0, width-promptMarkerMargin)
	tail := th.promptToggle.Render(th.measure.Truncate(marker, inner, "…"))
	return max(0, inner-th.measure.Width(tail)-promptMarkerGap)
}

// skillCellSpan is one row-slice of an accented "/token" in a sent user block: the body row it
// falls on (indexed into the block's wrapped rows BEFORE any collapse, so the caller can drop the
// ones its collapse hid) and the display-cell range [c0,c1) the token covers there. It is
// [inputCellSpan]'s transcript twin — the same idea against the other wrap.
type skillCellSpan struct{ row, c0, c1 int }

// userBlockCellSpans maps a sent message's [skillSpan]s — byte ranges into its own text, captured
// at send time — onto the rows and display cells the user block draws them at. A token confined to
// one row yields one span; a token straddling a soft-wrap yields one per row it spans, so it lights
// up on both halves exactly as the prompt box's accent does (inputaccent.go).
//
// The rows are re-wrapped here rather than passed in, deliberately: wrapText is a pure function of
// the same three arguments hangingPrefixes just gave it, so this asks the SAME oracle the block's
// rows came off rather than trying to unpick a marker prefix back off them.
//
// The mapping itself is an alignment walk, because the wrap DROPS characters — the space it broke
// at, the newline it broke on — and inserts none. Each row's runes are matched forward against the
// text from where the previous row left off, which yields every rune's own byte offset in the text
// and so the cell columns of any range of it. A row that fails to align (text and rows out of step,
// which nothing in wrapText should produce) simply stops the walk: the block then paints plain,
// which is what an entry recorded before spans existed already looks like.
func userBlockCellSpans(th theme, marker, text string, width int, spans []skillSpan) []skillCellSpan {
	if len(spans) == 0 || text == "" {
		return nil
	}
	lead := th.measure.Width(marker) // the marker, and on every later row the blank indent matching it
	if hangCollapses(width, lead) {
		// The block shed its marker whole rather than squeeze it (hangingPrefixes), so the rows it
		// composed start at column 0 and were wrapped to the full width. Asking the same oracle
		// means taking the same collapse: a lead counted here that the block did not draw would
		// shift every accent right by the marker it no longer has.
		lead = 0
	}
	var out []skillCellSpan
	pos := 0 // how far into text the walk has consumed
	for r, row := range wrapText(th, text, max(1, width-lead)) {
		runes := alignRow(text, &pos, row)
		for _, sp := range spans {
			lo, hi := -1, -1
			for _, ru := range runes {
				if ru.src < sp.start || ru.src >= sp.end {
					continue
				}
				if lo < 0 {
					lo = ru.at
				}
				hi = ru.end
			}
			if lo < 0 {
				continue // no part of this token landed on this row
			}
			out = append(out, skillCellSpan{
				row: r,
				c0:  lead + th.measure.Width(row[:lo]),
				c1:  lead + th.measure.Width(row[:hi]),
			})
		}
	}
	return out
}

// alignedRune is one rune of a wrapped row placed back in the text it was wrapped from: where it
// sits in the row (at, end — byte offsets, so the row can be sliced for a width) and where it came
// from in the text (src — the offset a span is stated in).
type alignedRune struct{ at, end, src int }

// alignRow walks one wrapped row against the text it came from, advancing *pos past what the row
// consumed. A rune the wrap dropped is skipped over on the way: the search for each of the row's
// runes runs forward through the text until it matches, which is exact for a wrap that only ever
// drops whitespace at the break it took. It stops at the first rune it cannot find, leaving the
// rest of the row unmapped rather than guessing.
func alignRow(text string, pos *int, row string) []alignedRune {
	out := make([]alignedRune, 0, len(row))
	for i, ch := range row {
		for *pos < len(text) {
			r, size := utf8.DecodeRuneInString(text[*pos:])
			if r == ch {
				break
			}
			*pos += size
		}
		if *pos >= len(text) {
			break
		}
		out = append(out, alignedRune{at: i, end: i + utf8.RuneLen(ch), src: *pos})
		*pos += utf8.RuneLen(ch)
	}
	return out
}

// accentRow paints the skill accent onto one composed row of a user block: every mapped cell span
// belonging to row index, clamped to limit — the cells that row's own content occupies — and
// re-styled in place (shadeCells, the accentTokens idiom). The flanking cells keep the block's own
// styling, so the row stays one solid dark-gray band with a violet run in it.
func accentRow(th theme, row string, index int, accents []skillCellSpan, limit int) string {
	for _, a := range accents {
		if a.row != index {
			continue
		}
		c0 := clampInt(a.c0, 0, limit)
		c1 := clampInt(a.c1, c0, limit)
		if c1 <= c0 {
			continue
		}
		row = shadeCells(th.measure, row, c0, c1, th.skillAccent)
	}
	return row
}
