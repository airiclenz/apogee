package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// ----------------------------------------------------------------------------
// Wrapping primitives
// ----------------------------------------------------------------------------

// hangingWrap word-wraps text under a leading marker, then styles each physical line: the
// marker leads the first line and a same-width blank indent leads every continuation line, so
// a wrapped block stays aligned under its marker (the ✦/┝ hanging indent of layout.md). The
// style colours the line's text; widths are ANSI-agnostic, so styling never perturbs the
// soft-wrap arithmetic.
//
// Its rail is the block's own width, and a BANDED style is filled out to it with its hanging prefix
// held outside the band (renderHangingRow): a diff line's tint reaches the same column on a two-word
// line as on a full one, and the marker column beside it stays chrome.
func hangingWrap(th theme, style lipgloss.Style, marker, text string, width int) []string {
	prefixed := hangingPrefixes(th, marker, text, width)
	pw := hangPrefixWidth(th, marker, width)
	out := make([]string, len(prefixed))
	for i, ln := range prefixed {
		out[i] = renderHangingRow(th, style, ln, pw, width)
	}
	return out
}

// wrapRail is the column a wrap's own lines are held to: the width it was given, floored at the one
// column wrapText floors its limit at, so a rail asked of a zero-width block still matches the line
// that block produced rather than cutting it back to nothing.
func wrapRail(width int) int { return max(1, width) }

// renderToRail renders ONE wrapped line on style, padding it out to rail columns inside that style
// when — and only when — the style carries a background. It is the one rule every wrap rail in this
// package inherits, which is what lets the diff band reach the block's edge with no call site
// knowing anything about diffs (ratified call 6 of docs/plans/"2026-08-19 - 05"; the six detailStyle
// painters are unchanged).
//
// The BACKGROUND is what the question asks, not the kind of line: a band that stopped at the last
// glyph would draw a ragged right edge down a body of unequal lines and would say nothing under a
// short line's trailing space, where the whole point of moving the diff signal off the text was to
// give it a surface that is there whether or not the line has glyphs in that column (ratified call
// 2). A style with no background has no such surface, so it renders exactly what it rendered before
// this rule existed — byte-identical, which is what keeps every non-diff wrap in the transcript out
// of this change.
//
// The pad is measured in the width authority (squareLine over th.measure, ADR 0030), and the line
// is padded BEFORE the style is past it: the escapes a styled line would carry cost nothing in the
// count, and a wide glyph costs the two cells the painter will actually spend on it. Padding after
// the style would put the spaces outside the SGR run — bare cells showing the terminal's own
// background through the very band they were added to fill.
func renderToRail(th theme, style lipgloss.Style, line string, rail int) string {
	if style.GetBackground() == (lipgloss.NoColor{}) {
		return style.Render(line)
	}
	return style.Render(squareLine(th.measure, line, rail))
}

// hangPrefixWidth is the column cost of the prefix hangingPrefixes puts on every row it returns:
// the marker's own width, or 0 in the narrow case where the marker is shed whole and the text wraps
// flat (hangCollapses). It asks the same oracle on the same two arguments the prefixing asked, so
// the split below can never claim a column the prefix does not occupy.
func hangPrefixWidth(th theme, marker string, width int) int {
	mw := th.measure.Width(marker)
	if hangCollapses(width, mw) {
		return 0
	}
	return mw
}

// renderHangingRow renders ONE pw-prefixed row from hangingPrefixes, on style, held to a
// width-column rail.
//
// A plain style paints the row whole, exactly as it always did. A BANDED style splits the hanging
// prefix off first — the marker on the first row, the blank indent under it on every continuation —
// paints that prefix with the band's background CLEARED, and fills only the text out to the rail
// left of it. It is the division gutteredWrap already draws between its gutter and its text, and it
// is what ratified call 3 of docs/plans/"2026-08-19 - 05" asks of every frame: the band is the
// TEXT's field, so the ┝/┕ branch glyph and the blank column beneath it stay chrome rather than
// reading as part of the change. Prefix and band tile the row exactly once between them, so a banded
// row still measures the full width.
//
// The prefix is cut in the width authority's measure (ADR 0030) — the measure hangingPrefixes laid
// it down in — rather than by byte count, so a marker glyph wider than one cell is cut at the column
// it actually occupies. A row the clip already re-cut into its own prefix (fitClipTail at a width
// barely wider than the marker) keeps whatever is left as the prefix and bands the remainder: the
// split is a measure, not a parse, and it cannot address a column the row does not have.
func renderHangingRow(th theme, style lipgloss.Style, row string, pw, width int) string {
	if pw == 0 || style.GetBackground() == (lipgloss.NoColor{}) {
		return renderToRail(th, style, row, wrapRail(width))
	}
	prefix := th.measure.Truncate(row, pw, "")
	return style.Background(lipgloss.NoColor{}).Render(prefix) +
		renderToRail(th, style, strings.TrimPrefix(row, prefix), wrapRail(width-pw))
}

// hangCollapses reports whether a block width columns wide is too narrow to hold an mw-column
// hanging marker AND one column of text beside it — the point at which the hang collapses to zero
// (layout.md, the Column contract's narrow case).
//
// A marker is SHED WHOLE there, never squeezed: the alternative is what this package used to do —
// floor the text at one column and prepend the marker anyway, which composes a three-cell line in a
// two-cell block and breaks layout.md's absolute width cap, the one rule no surface may bend. It is
// the same order the pane title spends its width in (layout.md, "Narrowness does not buy silence
// either"): the mark that no longer fits is dropped rather than shrunk, because a half-marker says
// something the layout does not mean.
//
// An mw of 0 — a caller wrapping with no marker at all (clipCells) — collapses only at a width
// below one column, where the wrap's own floor already lands on the same single-column line, so the
// markerless callers are untouched by construction.
func hangCollapses(width, mw int) bool { return width < mw+1 }

// hangingPrefixes word-wraps text to the width left of the marker and prepends the marker to
// the first line and a matching blank indent to the rest, returning the unstyled lines. It is
// shared by the styled hanging wrap and the user block (which then pads each line to a
// full-width background).
//
// A block too narrow to hold the marker plus one text column drops the marker AND the continuation
// indent and wraps the text flat at the block's full width (hangCollapses).
func hangingPrefixes(th theme, marker, text string, width int) []string {
	mw := th.measure.Width(marker)
	if hangCollapses(width, mw) {
		return wrapText(th, text, width)
	}
	indent := strings.Repeat(" ", mw)
	lines := wrapText(th, text, width-mw)
	out := make([]string, len(lines))
	for i, ln := range lines {
		if i == 0 {
			out[i] = marker + ln
		} else {
			out[i] = indent + ln
		}
	}
	return out
}

// clipTail is what a row cut short ends in: one space and one ellipsis, the sketch's own spelling
// (docs/layout/tool-layout.md). It is a CONTINUATION mark, not a marker — it says "this line goes
// on", where the "+N more lines" marker says how much never got a line at all.
const clipTail = " …"

// clipWrap is hangingWrap under a row budget. It wraps and styles exactly as hangingWrap does —
// the same hangingPrefixes path, so the same wrapText, the same expandTabs, the same hanging
// continuation indent and the same banded rail (renderToRail) — and then keeps at most maxRows
// physical rows, ending the last kept row in
// clipTail when it dropped any. Handed text that fits, it returns hangingWrap's own lines and
// clipped false, so a caller can reach for it unconditionally.
//
// It REPORTS the clip rather than leaving the caller to infer one. Whether a collapsed block hides
// anything is width-dependent once a target can be cut, and the indicator, the click target and the
// paint all have to agree about it; asking this once and passing the answer along is what keeps them
// from each re-deriving it — and from drifting apart when only one of them is changed.
//
// The tail is FITTED, not appended: the kept row is re-cut so the row and its tail together measure
// within width in the width authority's measure, which is the measure the frame is painted in
// (ADR 0030). Appending to a row the wrap had already filled to the column would overrun the width
// by the tail, and the viewport would fold the row into the very second row the budget was spending.
func clipWrap(th theme, style lipgloss.Style, marker, text string, width, maxRows int) (lines []string, clipped bool) {
	if maxRows < 1 {
		return nil, true // no row to spend: everything is hidden, and nothing is left to say so
	}
	prefixed := hangingPrefixes(th, marker, text, width)
	if clipped = len(prefixed) > maxRows; clipped {
		prefixed = prefixed[:maxRows]
		prefixed[maxRows-1] = fitClipTail(th, prefixed[maxRows-1], width)
	}
	pw := hangPrefixWidth(th, marker, width)
	out := make([]string, len(prefixed))
	for i, ln := range prefixed {
		out[i] = renderHangingRow(th, style, ln, pw, width)
	}
	return out, clipped
}

// fitClipTail re-cuts one wrapped row so the row plus clipTail measures within width. The row is
// still unstyled here — the cut lands on the text the wrap produced, before any style has been past
// it, which is the same order every other measurement in this package takes.
//
// It trims the trailing spaces the cut leaves behind: a break can hand back the space it fell on,
// and "grep  …" reads as a slip where "grep …" reads as a sentence continuing. A width too narrow
// to seat even the tail leaves the tail alone rather than half of it — a lone "…" one column short
// of the edge is still the honest mark, and no row can be narrower than what it must say.
func fitClipTail(th theme, row string, width int) string {
	room := max(0, width-th.measure.Width(clipTail))
	return strings.TrimRight(th.measure.Truncate(row, room, ""), " ") + clipTail
}

// wrapText word-wraps text to limit columns, hard-breaking any word longer than the limit
// and preserving the text's own newlines. An empty string yields a single empty line so a
// just-opened assistant buffer still renders its marker.
//
// It breaks with the width authority (th.measure.Wrap), so the break is CHOSEN in the same measure
// the cap below is enforced in and the painter draws in — ADR 0030's rule for this package. It used
// to break with the package-level ansi.Wrap, which is hard-wired to ansi.GraphemeWidth whatever the
// painter is doing: on the painter's default WcWidth that measured a VARIATION SELECTOR-16 cluster
// two cells against the one the terminal paints, so every wrapped surface — transcript prose,
// pop-up bodies, table cells — took its break a cell earlier than it needed on such a line. On
// content the two measures agree about (everything without VS16) the two wraps are identical, which
// is why this is a rename rather than a re-layout. A caller that then pads with a lipgloss Width
// style hands the gain straight back — lipgloss folds in GraphemeWidth — which is why the user
// block below squares its own rows and why the pop-up pane still does not (the issue register).
//
// No line it returns is wider than limit in the width authority's measure — layout.md's absolute
// cap, enforced here rather than assumed. The upstream wrap does not hold it on its own, in either
// measure: the breakpoint branch lacks the full-line checks its default branch has, on the wcwidth
// path (x/ansi@v0.11.7/wrap.go:406-419) and on the grapheme path (:352-361) alike, so a run of
// breakpoints keeps growing a word onto an already-full line — a wrap of "| --- | --- | --- |" at
// limit 3 comes back with a five-cell first line, and of "----" with a four-cell one. Every line
// that comes back over the limit is therefore hard-broken down to it, which is also what makes the
// docstring's "hard-breaking any word longer than the limit" true rather than aspirational. The one
// thing no break can divide is a single grapheme wider than the limit — a CJK glyph at limit 1 —
// and that keeps a line to itself.
func wrapText(th theme, text string, limit int) []string {
	if limit < 1 {
		limit = 1
	}
	// Tabs are settled BEFORE the break is chosen: a tab the wrap counts as nothing is four cells
	// once a style has been past the line, and the cap above would then hold in the measure and
	// break in the paint (expandTabs).
	text = expandTabs(text)
	wrapped := strings.Split(th.measure.Wrap(text, limit, ""), "\n")
	out := make([]string, 0, len(wrapped))
	for _, ln := range wrapped {
		if th.measure.Width(ln) <= limit {
			out = append(out, ln)
			continue
		}
		// preserveSpace keeps this pass purely additive — it inserts breaks and drops nothing, so
		// a segment's own leading indentation survives the cap. The break the hard wrap opens
		// ahead of an over-wide leading grapheme would otherwise surface as a blank row.
		segs := strings.Split(th.measure.Hardwrap(ln, limit, true), "\n")
		if len(segs) > 1 && segs[0] == "" {
			segs = segs[1:]
		}
		out = append(out, segs...)
	}
	return out
}

// tabCells is how many spaces one TAB becomes when this package expands it: lipgloss's own
// tabWidthDefault (lipgloss/v2@v2.0.5/style.go:14, maybeConvertTabs), so a line handed to a style
// with its tabs already expanded paints exactly what that style would have made of it anyway.
const tabCells = 4

// expandTabs replaces every TAB in s with the spaces a lipgloss style would otherwise put there.
//
// A TAB is ZERO cells to the width authority, and zero to the painter too — ultraviolet drops the
// control byte rather than advancing to a tab stop — so on that pair alone the two agree and there
// would be nothing to settle. What breaks the agreement is what sits BETWEEN them: every
// lipgloss.Style.Render rewrites "\t" into tabCells spaces on its way past (maybeConvertTabs), after
// the authority measured the line and before the painter ever sees it. A user block therefore
// composed four cells more than it had measured, per tab in the text: the row overran the width the
// block was given, the viewport folded that one row into two painted ones, and the skill accent —
// shaded at cells counted in the authority's measure — landed four columns left of the token it
// names.
//
// Expanding before anything measures is what puts the three back in step: the authority counts the
// spaces, the style finds no tab left to rewrite, and the painter paints the very cells that were
// counted. This is not the content normalisation ADR 0030 rules out — that ruling is about VS16,
// where folding the content would change what the user sees and would overrule the terminal's own
// measure. A tab has no display width for anyone to have an opinion about, and the spaces are what
// the block was already painting; only the counting of them was wrong.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	return strings.ReplaceAll(s, "\t", strings.Repeat(" ", tabCells))
}

// expandTabsInSpans re-bases spans — byte offsets into text — onto expandTabs(text): every TAB
// before an offset grows the text by tabCells-1 bytes, so the offset moves by that much per tab
// preceding it. Call it while text still holds its tabs, on the way to handing the expanded text to
// the wrap and to the accent map, so both address the same string.
//
// Offsets are clamped to text before they are counted against, so a span that never went through
// the transcript boundary's own check (spansWithin) cannot slice out of range here either.
func expandTabsInSpans(text string, spans []skillSpan) []skillSpan {
	if len(spans) == 0 || !strings.Contains(text, "\t") {
		return spans
	}
	shift := func(off int) int {
		off = clampInt(off, 0, len(text))
		return off + strings.Count(text[:off], "\t")*(tabCells-1)
	}
	out := make([]skillSpan, 0, len(spans))
	for _, sp := range spans {
		out = append(out, skillSpan{start: shift(sp.start), end: shift(sp.end)})
	}
	return out
}

// railWidth is the column cost of one sub-agent rail gutter ("│ " — the rail glyph plus one
// space), the amount each nesting level steals from the usable text width (P3.14).
const railWidth = 2

// railedWidth is the usable text width inside a Depth-level block: the full width less one
// rail gutter per level. Depth 0 is the common case and returns width unchanged; deeper
// levels are floored at one column so wrapping never divides by zero.
func railedWidth(width, depth int) int {
	if depth <= 0 {
		return width
	}
	return max(1, width-depth*railWidth)
}

// railSpacer is the one separating line between two adjacent blocks, framed for the run the two
// of them share: depth is the JOIN of their depths (the shallower one), so the rail is drawn only
// as deep as both sides reach. Depth 0 — the flat transcript, and either side of a sub-agent run's
// boundary — is the bare "" the layout has always used, so a top-level transcript renders exactly
// as before; deeper joins draw the gutter alone, which is what makes a run's frame continuous
// through its separators instead of breaking at every block.
//
// The gutter's trailing space is trimmed BEFORE it is styled, so a spacer's visible text is "│"
// at depth 1 and "│ │" at depth 2 — never a styled trailing blank, which would leave an invisible
// SGR run hanging off the right of an otherwise empty row.
func railSpacer(th theme, depth int) string {
	if depth <= 0 {
		return ""
	}
	return th.subRail.Render(strings.TrimRight(strings.Repeat(glyphSubRail+" ", depth), " "))
}

// railJoin is the ONE separator line between two adjacent blocks: the railed spacer (railSpacer) at
// the join — the min — of their depths, or, where the block below RESUMES a sub-agent list that an
// expanded member's span interrupted, the ┊ closing that span
// (docs/layout/tool-layout.md, "Grouped Sub-agents").
//
// closes is that question, and only the caller can answer it. The spec draws the closer for exactly
// one reason — another grouped sub-agent follows the expanded one — and a group's LAST member shows
// none however it ends. Depth cannot tell those apart: every climb out of a span looks identical
// from here whether a sibling row, the parent's own answer, or nothing at all stands below it, so
// [transcript.renderView] passes the answer down rather than have this guess it.
//
// The closer REPLACES the spacer rather than standing beside it, which is what the spec's sketch
// shows: a span's last row, the ┊, and the next member row are three consecutive lines. It is the
// separator's job the closer is doing — saying where one thing ends and the next begins — and a
// blank line beside it would say the run ended twice.
//
// It is railed at the depth of the row it stands before — the level the list resumes at — so the ┊
// closing a depth-1 span sits at column 0 under the ┌ that opened it, and one closing a depth-2 span
// stands inside the parent's still-open rail as "│ ┊". A climb of several levels at once still draws
// exactly ONE, for the same reason the rule exists: one member row follows, at one level, and the
// frames left behind on the way up have no sibling of their own to be parted from.
func railJoin(th theme, prevDepth, depth int, closes bool) string {
	if closes && prevDepth > depth {
		return railLines(th, []string{th.subRail.Render(glyphRailClose)}, depth)[0]
	}
	return railSpacer(th, min(prevDepth, depth))
}

// railLines frames a Depth-level block: it prepends one styled "│ " rail gutter per nesting
// level to each physical line, so a sub-agent's nested block reads as a vertical-ruled
// sub-section (P3.14). Depth 0 is the common case and returns the lines untouched, so the
// flat top-level transcript renders exactly as before. The rail is styled in the subRail role's
// tool-header gold and sits left of any per-line background (e.g. the user block's), matching
// the marker hanging indent.
func railLines(th theme, lines []string, depth int) []string {
	if depth <= 0 {
		return lines
	}
	gutter := th.subRail.Render(strings.Repeat(glyphSubRail+" ", depth))
	out := make([]string, len(lines))
	for i, ln := range lines {
		out[i] = gutter + ln
	}
	return out
}
