package tui

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
	"github.com/rivo/uniseg"
)

// ----------------------------------------------------------------------------
// The textarea geometry mirror — the string math behind the line editor
// ----------------------------------------------------------------------------
//
// [lineEditor] (lineeditor.go) drives a bubbles textarea it cannot see inside: the widget wraps,
// sanitises and seats its caret by its own internal math, and exposes only rows, columns and
// LineInfo. Everything here is the string geometry that bridges the two — free functions over a
// value or a rune run, no Model and no widget in reach — and it is owned by the line editor: its
// caret family (caretTo, caretToOffset, caretByteOffset) and the prompt box's row count read it, and
// the gestures in mouse.go, the accent pass in inputaccent.go and the popup fields call it through
// the same names.
//
// Two groups live side by side, and the rule that separates them is the oracle, not the file:
//
//   - The WIDGET MIRRORS — [wrapRowStarts] with its [growingWidth] and [runesWidth] ruler,
//     [inputContentRows], [sanitizeInputLine] with [inputTabCells] and [sanitizerDropsRune], and
//     [cellToRuneOffset] — answer to the textarea alone (ADR 0030 §6): they measure with uniseg
//     because the widget does, whatever the painter's width authority (width.go) is on.
//   - The OFFSET ARITHMETIC — [visualSubline], [caretOffset], [offsetToLineCol], [runeOffsetOf],
//     [byteOffsetOf], [selectionText] — counts runes and bytes, never cells, so no width ruler has
//     a vote in it at all.
//
// [cellToRuneOffsetIn] is the one painter-measured sibling: the same inversion as cellToRuneOffset,
// but in a STATED [widthAuthority], for cells this package's own painter drew. It sits beside the
// mirror it shadows so the choice between the two is made in one place, not because it is one.

// wrapRowStarts reports the rune offset each visual (soft-wrapped) row of ONE logical line begins
// at, at the textarea's text width — so len(starts) is that line's row count and starts[k] anchors
// its k-th row. It mirrors the bubbles textarea's own `wrap`, rune for rune, because the accents are
// painted onto cells that widget already drew: a mirror that merely wrapped "correctly" would
// misplace them wherever the two disagreed. TestWrapRowStartsMirrorsTheWidget pins it against a real
// textarea's LineInfo at every column, so a change to the widget's wrap fails here rather than
// silently sliding the accents sideways.
//
// Its ORACLE is therefore the widget, never the width authority (width.go): the authority follows
// the painter, and the painter has no vote in where a third-party widget decided to break a line.
// So every measure here is runesWidth — uniseg, the widget's own — and it stays that way whatever
// the authority is on. The one exception is the last-rune term of the hard-break test below, where
// the widget itself reaches for go-runewidth; mirroring that means spelling it the same way.
//
// The widget's rule, in its own terms: text accumulates as WORD + trailing SPACES groups, and a
// group that would overflow the row opens a new one carrying the whole group (which is why a run of
// spaces can land alone on a row). A word too wide for any row is broken where it stands — and the
// widget compares its width against the row counting the word's last rune twice, an off-by-one
// mirrored here deliberately, since matching the split is the whole point. Finally a line whose
// content REACHES the width gains one trailing row, the seat the widget keeps for a caret past a
// full line.
//
// Its row COUNT — len(starts) — is what sizes the prompt box: [inputContentRows] below is the
// sum of this over the value's logical lines, which is the widget's own decomposition
// (totalVisualLines). So the box's height and the rows an accent lands on come off one ruler.
//
// The row and the pending word are measured as whole runs, never summed per rune, which is not a
// detail: a grapheme cluster measures as a whole, so summing its runes one at a time would
// under-count exactly the sequences (an emoji carrying VARIATION SELECTOR-16) the widget counts as
// two cells. The two runs weighed are the widget's own operands — the runes already on the row, and
// the pending word — so the mirror weighs the same text at the same moments it does. Each run's
// width is carried forward as it grows ([growingWidth]) and only its trailing grapheme cluster is
// re-measured with what joins it, so the line costs one pass over its runes whatever the width —
// re-measuring the whole row at every placement made a long line cost its length times the width.
//
// The line is sanitised before any of that runs (sanitizeInputLine), because the widget sanitises on
// the way IN: every write path passes its runes through one sanitizer, so a tab arrives as four
// spaces and a control rune or a utf8.RuneError arrives not at all. Measuring the raw rune instead
// weighs a tab as a single space-like column, and a rune the widget dropped as a column it never
// drew, and wraps such a line where the widget does not. The offsets returned are therefore offsets
// into the line AS THE WIDGET HOLDS IT — post-sanitising — which for every caller in the package is
// the line handed in, since what they hand over is the widget's own already-sanitised value
// (runesWidth, cellToRuneOffset below).
func wrapRowStarts(line []rune, width int) []int {
	if width < 1 {
		width = 1
	}
	line = sanitizeInputLine(line)
	starts := []int{0}
	consumed := 0 // runes of line already placed on a row
	wordLen := 0  // the pending word: a run of non-space runes
	spaces := 0   // the whitespace run trailing that word
	// The widget's `lines[row]` and `word`, measured as they grow.
	var row, word growingWidth
	for _, r := range line {
		if unicode.IsSpace(r) {
			spaces++
		} else {
			wordLen++
			word.extend(line, consumed+wordLen) // the widget's `word`, r included
		}
		switch {
		case spaces > 0: // the group is finished: place it, on a new row if it does not fit
			end := consumed + wordLen + spaces
			if row.cells+word.cells+spaces > width {
				starts = append(starts, consumed)
				row = growingWidth{last: consumed}
			}
			row.extend(line, end)
			consumed = end
			wordLen, spaces = 0, 0
			word = growingWidth{last: consumed}
		case word.cells+runewidth.RuneWidth(r) > width: // a word wider than a row: break it here
			if consumed > starts[len(starts)-1] { // the current row already holds something
				starts = append(starts, consumed)
			}
			// Either way the row now holds exactly the word: it opened a fresh row, or the row it
			// joined held nothing.
			consumed += wordLen
			wordLen = 0
			row, word = word, growingWidth{last: consumed}
		}
	}
	if row.cells+word.cells+spaces >= width {
		starts = append(starts, consumed) // the trailing row a width-filling line keeps for the caret
	}
	return starts
}

// growingWidth is the uniseg display width ([runesWidth]'s ruler) of a rune run that only ever grows
// at its end — the row and the pending word of [wrapRowStarts]. Appending to a run can only merge
// runes into its LAST grapheme cluster (a combining mark after a space, a second regional indicator,
// a VARIATION SELECTOR-16 after an emoji); every boundary before that cluster's start is already
// decided by the runes on both sides of it. So the width is kept, and a growth re-measures only
// from that cluster's start to the new end: the result is exactly runesWidth of the whole run, at
// the cost of the runes added plus one cluster.
//
// The zero value is the empty run starting at offset 0; an empty run starting elsewhere is
// growingWidth{last: offset}.
type growingWidth struct {
	cells     int // the run's width
	last      int // the offset in the line its final grapheme cluster starts at (its start while empty)
	lastCells int // that final cluster's width
}

// extend grows the run to end at offset end of line, re-measuring from its final cluster only.
func (g *growingWidth) extend(line []rune, end int) {
	if end <= g.last {
		return
	}
	g.cells -= g.lastCells
	s, at, state := string(line[g.last:end]), g.last, -1
	for len(s) > 0 {
		var cluster string
		var w int
		cluster, s, w, state = uniseg.FirstGraphemeClusterInString(s, state)
		g.cells += w
		g.last, g.lastCells = at, w
		at += utf8.RuneCountInString(cluster)
	}
}

// inputContentRows reports how many visual rows the input value occupies at innerWidth, mirroring
// the textarea's own wrap so the box sizes to exactly what the widget draws. It is the sum over
// logical lines of [wrapRowStarts]' row count — the widget's own decomposition, which
// wraps each logical line independently and adds the counts (its totalVisualLines,
// bubbles/v2@v2.1.0/textarea/textarea.go:1666-1674). Delegating means the box's HEIGHT and the rows
// the accent pass paints on are read off one ruler; they used to be two separate derivations of the
// widget's wrap, and this one was an approximation (ansi.Wordwrap + ansi.Hardwrap) that disagreed
// with the widget on roughly 41% of prompt-shaped drafts, mostly by under-counting — "hello world"
// at width 5 is four widget rows and the old count said three.
//
// The trailing row a width-filling line keeps for a caret past its last cell comes with the mirror.
// Under-counting it leaves the box one row too short at a width-fill boundary — the source of the
// prompt-box scroll artifact the layout re-seat then can no longer reach (fixed in a7afbf1; its
// regression is [TestPromptScrollClampedWhileGrowing]). An empty value is one row.
//
// The count is deliberately unclamped: [promptEditor.rows] holds it to [minInputRows, maxInputRows],
// and past that cap the widget scrolls internally rather than the box growing further.
//
// TABs are the widget's four spaces here too: wrapRowStarts sanitises each line the way the
// textarea's own sanitizer did before it measures (sanitizeInputLine — which drops utf8.RuneError
// and the other control runes with the same authority), so this count inherits that with the rest of
// the wrap.
//
// A bare '\r' is a row boundary here for the same reason: the widget's sanitizer rewrites EVERY '\r'
// AND every '\n' as one newline before it splits into logical rows
// (bubbles/v2@v2.1.0/internal/runeutil/runeutil.go:68-76, textarea.go:504, :519-529), so a CR the
// widget received is a boundary it drew, and splitting on '\n' alone sized the box a row short for
// such a value. That per-rune rewrite is also why "\r\n" is mirrored as TWO boundaries rather than
// one: the widget's own answer for "a\r\nb" is three rows, and a mirror answers to the widget rather
// than to what a line ending ought to mean (ADR 0030 §6). No draft reaches this with a CR today —
// every caller hands over the widget's already-sanitised value — so the fold is fidelity for a value
// arriving from anywhere else, not a live fix.
//
// WIDGET MIRROR — deliberately NOT the width authority. This is one of the package's mirrors of a
// third-party widget's internal math, and a mirror's oracle is the widget, never apogee's
// painter-facing measure (width.go): the textarea wraps with uniseg.StringWidth
// (bubbles/v2@v2.1.0/textarea/textarea.go:1805-1852), which is what wrapRowStarts measures with
// (runesWidth) and is grapheme-clustered, unlike ansi.WcWidth. Sizing the box in the painter's
// measure would size it to something the widget never draws. The same rule governs the caret
// mirrors beside it in this file.
func inputContentRows(value string, innerWidth int) int {
	if innerWidth < 1 {
		innerWidth = 1
	}
	total := 0
	// ReplaceAll returns value untouched when it holds no CR, so the frame path pays nothing.
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r", "\n"), "\n") {
		total += len(wrapRowStarts([]rune(line), innerWidth))
	}
	return total
}

// runesWidth is the display width of a rune run measured the way the textarea measures its own
// content — uniseg.StringWidth over the whole run, so a grapheme cluster counts once, as the cells
// the widget believes it drew. It is the forward half of the cell↔rune mapping [cellToRuneOffset]
// inverts, so a token's start cell and the caret's column are read off the same ruler, and it is
// that widget's ruler rather than the painter's: both are mirrors of its internal math.
//
// It measures no TABs and needs none of the tab arithmetic the transcript side carries: everything
// weighed here comes from the textarea's own value, which the widget sanitises tabs out of on the
// way in — see [cellToRuneOffset] below for why that holds on every write path — and a line
// reaching [wrapRowStarts] from anywhere else has been through the same sanitising first
// (sanitizeInputLine).
func runesWidth(rs []rune) int {
	return uniseg.StringWidth(string(rs))
}

// inputTabCells is how many spaces one TAB becomes on its way into the textarea: the widget
// sanitises every write with runeutil.NewSanitizer's defaults, and that sanitizer rewrites '\t' as
// four spaces flat — not to the next tab stop (bubbles/v2@v2.1.0/internal/runeutil/runeutil.go:26,
// textarea.san). It is deliberately its own constant rather than [tabCells] (wrap.go), which is the
// same number today for an unrelated reason: that one mirrors lipgloss's tab width because the
// PAINTER will apply it, this one mirrors the widget's sanitizer because the WIDGET applied it. A
// mirror answers to the widget alone (ADR 0030 §6), so if the two ever diverge each must follow its
// own oracle.
const inputTabCells = 4

// sanitizeInputLine rewrites line the way the textarea rewrote everything ever written into it, so a
// line is measured as the widget HOLDS it rather than as it was handed over. The widget's rule is
// runeutil.NewSanitizer's default (bubbles/v2@v2.1.0/internal/runeutil/runeutil.go:26-29, :56-95),
// and this is the whole of it per line: a utf8.RuneError is dropped, a TAB becomes [inputTabCells]
// spaces, every other control rune is dropped, and anything else is kept. Mirroring only the tab
// leaves the mirror one rune out of step with the widget for every rune it drops: the offsets
// returned index the value the widget HOLDS, so an accent past such a rune is seated on the wrong
// run of cells — and a utf8.RuneError, one cell wide to the ruler and absent from the widget, moves
// the wrap itself.
//
// '\r' and '\n' are the sanitizer's remaining case (each becomes one '\n') and are deliberately not
// handled here, because neither can reach a LINE: the widget sanitises BEFORE it splits its input
// into logical rows (bubbles/v2@v2.1.0/textarea/textarea.go:504, :519-529), so a '\r' has already
// become a row boundary rather than a rune inside a row, and the callers split the value on that
// boundary before they get here — [inputCellSpans] on '\n', which is all the widget's own value can
// carry, and [inputContentRows] on either, since a value handed to it need not have
// come from the widget at all. That the widget's value is
// sanitised on every write path at all is argued once, from the caret's side, at [cellToRuneOffset]
// below.
//
// A line the sanitizer would leave alone is returned as-is, unallocated: that is every line the
// package itself measures — the widget's value has already been through this — so the frame path
// pays nothing for a case only an outside caller can reach.
func sanitizeInputLine(line []rune) []rune {
	tabs, dropped := 0, 0
	for _, r := range line {
		switch {
		case r == '\t':
			tabs++
		case sanitizerDropsRune(r):
			dropped++
		}
	}
	if tabs == 0 && dropped == 0 {
		return line
	}
	out := make([]rune, 0, len(line)+tabs*(inputTabCells-1)-dropped)
	for _, r := range line {
		switch {
		case r == '\t':
			for i := 0; i < inputTabCells; i++ {
				out = append(out, ' ')
			}
		case sanitizerDropsRune(r):
			// Kept by neither the widget nor the mirror.
		default:
			out = append(out, r)
		}
	}
	return out
}

// sanitizerDropsRune reports whether the textarea's sanitizer drops r outright instead of keeping or
// rewriting it: utf8.RuneError, and every control rune it has no replacement for — which is all of
// them but '\t' (four spaces) and '\r'/'\n' (a row boundary, never a rune within a line — see
// [sanitizeInputLine]).
func sanitizerDropsRune(r rune) bool {
	return r == utf8.RuneError || (r != '\t' && unicode.IsControl(r))
}

// visualSubline returns the runes of one visual (soft-wrapped) sub-line: the [start, start+width)
// rune slice of the row-th logical line of value. LineInfo supplies start (the sub-line's rune
// offset into its logical line) and width (its rune count), so the slice is exactly the runes the
// textarea drew on that visual row — bounded so a click near the wrap point never reads into the
// next row's runes.
func visualSubline(value string, row, start, width int) []rune {
	lines := strings.Split(value, "\n")
	if row < 0 || row >= len(lines) {
		return nil
	}
	runes := []rune(lines[row])
	lo := clampInt(start, 0, len(runes))
	hi := clampInt(start+width, lo, len(runes))
	return runes[lo:hi]
}

// cellToRuneOffset maps a display-cell column within a run of runes to the rune offset at that
// column: the last offset whose text still fits inside cells ([runesWidth] above). A column
// that lands inside a wide grapheme resolves to that grapheme's left edge; a column past the run's
// end returns the full rune count — the clamp the caller relies on, expressed in runes rather than
// cells.
//
// Its ORACLE is the textarea widget, not the width authority (width.go), and the two genuinely
// differ: the authority follows the PAINTER, while this inverts the widget's own cursor math, which
// measures with uniseg whatever the painter is doing (bubbles/v2@v2.1.0 textarea.LineInfo's
// CharOffset, and textarea.Cursor's x, are both uniseg.StringWidth of the row prefix). The caret
// this feeds is drawn at that CharOffset, so measuring here in any other ruler would seat the caret
// at a column the widget then draws it somewhere else from. Where a click's own column has to be
// read as PAINTED cells — the selection highlight, the accent overlay — the authority is the right
// ruler and is used instead; this one conversion lives in the widget's space because its answer is
// consumed by the widget.
//
// A TAB never reaches here, which is why nothing on this path expands one (expandTabs, render.go)
// before measuring it. The textarea sanitises everything written into it — runeutil.NewSanitizer,
// whose default rewrites each '\t' as four spaces and drops utf8.RuneError and every other control
// rune — and every write path funnels through that one sanitiser: SetValue and InsertString,
// InsertRune, both paste messages, and the key-press default. A draft therefore cannot HOLD a tab —
// nor a control rune, nor a utf8.RuneError — so the value this reads (and the value the accent
// overlay reads, inputaccent.go) is tab-free by construction. A line arriving from OUTSIDE the
// widget carries no such guarantee, which is why the wrap mirror re-applies that whole per-rune rule
// itself ([sanitizeInputLine] above, where it is stated in full). The tab-measurement
// defects fixed elsewhere in the package — where text arriving from the model or the disk still
// carried its tabs past a ruler that counts them as nothing — have no instance on the prompt box's
// side of the line.
func cellToRuneOffset(runes []rune, cells int) int {
	for i := range runes {
		if runesWidth(runes[:i+1]) > cells {
			return i
		}
	}
	return len(runes)
}

// cellToRuneOffsetIn is cellToRuneOffset in a STATED measure: the offset of the last rune whose text
// still fits inside cells when measured the way the given authority measures (width.go).
//
// The two exist apart because they invert two different painters. cellToRuneOffset's oracle is the
// textarea WIDGET, which measures its own cursor with uniseg whatever the terminal is doing, and its
// answer is fed straight back to that widget. This one's oracle is THIS package's painter: the cells
// it walks are cells of a line the popup module laid out and drew (a /settings row), so a click's
// column has to be read in the authority the row was measured and cut in, or the pointer names one
// glyph and the caret lands on its neighbour on every terminal the two measures disagree on
// (ADR 0030 — a VARIATION SELECTOR-16 cluster is one cell to the painter's default and two to uniseg).
//
// So it is NOT a widget mirror, and ADR 0030 §6's exemption does not cover it: its oracle is the
// painter's [widthAuthority], handed in rather than assumed. It lives in this file as the
// painter-measured sibling of the mirror it shadows, so the choice between the two rulers is made at
// one site.
func cellToRuneOffsetIn(measure widthAuthority, runes []rune, cells int) int {
	for i := range runes {
		if measure.Width(string(runes[:i+1])) > cells {
			return i
		}
	}
	return len(runes)
}

// caretOffset converts a (logical row, column) cursor position into a rune offset into value,
// counting each '\n' as one rune so the result indexes []rune(value) directly. Soft-wraps are
// not in value, so they contribute nothing — only real newlines do, which is what copied text
// should preserve.
func caretOffset(value string, row, col int) int {
	lines := strings.Split(value, "\n")
	off := 0
	for i := 0; i < row && i < len(lines); i++ {
		off += len([]rune(lines[i])) + 1 // the +1 is the '\n' that split removed
	}
	return off + col
}

// offsetToLineCol is caretOffset's exact inverse: it turns a rune offset into value back into the
// (logical row, column) the textarea positions its cursor by. The mouse needs only the forward
// direction (a click names a cell, the caret follows), but a completion that splices text into the
// MIDDLE of a draft needs this one: the new caret is known as an offset into the new value, and the
// widget can only be driven by row and column ([lineEditor.caretToOffset]).
//
// An offset past the end of a line lands at that line's end rather than wrapping into the next,
// which is what makes the two functions inverses at every position, the line ends included: the
// offset of a row's last column and the offset of the next row's first differ by the '\n' between
// them. Offsets outside the value clamp to its first and last positions.
func offsetToLineCol(value string, off int) (row, col int) {
	if off < 0 {
		return 0, 0
	}
	lines := strings.Split(value, "\n")
	for i, ln := range lines {
		n := len([]rune(ln))
		if off <= n || i == len(lines)-1 {
			return i, clampInt(off, 0, n)
		}
		off -= n + 1 // the +1 is the '\n' that split removed
	}
	return 0, 0 // unreachable: Split always yields at least one line
}

// runeOffsetOf converts a BYTE offset into value to the rune offset of the same position. It is the
// bridge between the two coordinate systems the input cluster lives in: the chat mini-language
// slices the value by byte (command.go, autocomplete.go — its tokens are delimited by ASCII
// whitespace, so byte offsets are the natural currency there), while the textarea counts its cursor
// in runes. A byte offset past the end clamps to the end.
//
// The count is RUNES, not columns: neither end of the bridge is a display width, so the width
// authority has no part in it and converting it to one would be a defect.
func runeOffsetOf(value string, byteOff int) int {
	return utf8.RuneCountInString(value[:clampInt(byteOff, 0, len(value))])
}

// byteOffsetOf is runeOffsetOf's inverse: the byte offset at which the runeOff-th rune of value
// begins. An offset past the last rune yields len(value) — the end position, which is a valid
// caret site and not a rune of its own.
func byteOffsetOf(value string, runeOff int) int {
	if runeOff <= 0 {
		return 0
	}
	n := 0
	for i := range value { // ranging a string visits the byte index of each rune's first byte
		if n == runeOff {
			return i
		}
		n++
	}
	return len(value)
}

// selectionText returns the value runes between two offsets (lo inclusive, hi exclusive),
// clamped to the value — the text a drag copies to the clipboard.
func selectionText(value string, a, b int) string {
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	r := []rune(value)
	lo = clampInt(lo, 0, len(r))
	hi = clampInt(hi, 0, len(r))
	return string(r[lo:hi])
}
