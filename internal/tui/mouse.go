package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Mouse support: click-to-position + drag-to-select in the prompt AND transcript (layout.md)
// ----------------------------------------------------------------------------
//
// The terminal does its own click-drag text selection only while no application captures the
// mouse. apogee captures it (MouseModeCellMotion, set in View) so the wheel can scroll the
// transcript and a click can position the caret — which is exactly why the terminal's own
// selection is off. So selection is implemented here, on top of the widgets' public API: no
// copy of the widgets' internal wrapping is needed, which keeps the geometry correct across
// releases.
//
// Two rectangles, two selection models. The prompt (textarea) selection carries rune offsets
// into the source Value, so it copies the exact typed text. The transcript (viewport) selection
// is screen-space — "copy what you see": it anchors in content coordinates (rendered-line index
// + display cell) and slices the cached rendered lines on release, so markers, rail gutters, and
// soft-wraps are copied verbatim (the accepted terminal-native semantics, D4). The mouse
// handlers arbitrate by region — a point in the input rect drives the editor, a point in the
// viewport drives the transcript — so the two selections never coexist.
//
// A transcript selection SURVIVES a repaint by the keep-if-unchanged rule (transcriptSel.
// spanUnchanged, applied in refreshViewport): it lives on exactly while every rendered line it
// spans is identical before and after. The transcript is append-only at a fixed width, so a drag
// over settled text keeps extending while the model streams new lines beneath it — and the moment
// the text under the span does move (the streaming tail growing, a rewrap on resize, a tool call
// joining its group) the selection drops rather than pointing at something else. That is what
// makes copy equal sight: the release slices the very lines the rule protected. A COLLAPSED span is
// exempt and survives every repaint — it shades nothing, so it is a click in progress rather than a
// selection, and the release still needs the line the press named to toggle a block (toggleBlockAt);
// without the exemption a live block's blinking star rewrote its own header line out from under the
// press and the toggle was lost.
//
// Screen rows become content lines through ONE mapping, Model.contentLineAt (model.go), which the
// mouse and the highlight both read. It is overlay-aware at BOTH ends of the transcript, and that
// is what keeps copy equal to sight rather than merely close to it:
//
//   - At the top, the sticky header draws the owning prompt over the viewport's first rows, so
//     those rows name the header's own lines, not the reply lines the scroll offset hides beneath
//     them. A drag that starts on the header and runs down into the reply therefore spans the
//     content between the two — the lines the header covers included, since a screen-space span
//     stays a contiguous run of content lines.
//   - At the bottom, the approval and ask popups, the /sessions browser, the picker, the
//     autocomplete dropdown and the staged-interjection strip take their rows OFF the transcript
//     (Model.transcriptRows composes the frame from what is left). Those rows map to no content
//     line at all, so a click on a popup border or a session row arms nothing — the alternative,
//     bounding by the height layout() stored, addressed reply lines that were not on screen.

// cell is an absolute visual position inside the textarea content: row counts wrapped (visual)
// lines from the top of the value; col is the display column within that row.
type cell struct{ row, col int }

// promptSel is the prompt's drag-selection. It carries the same span two ways: rune offsets
// into the textarea Value (anchorOff/headOff — what gets copied, so real newlines survive and
// soft-wraps do not) and absolute visual cells (anchorVis/headVis — what gets highlighted,
// derived straight from the mouse so no wrap math is needed to draw it). anchor is the drag's
// fixed end (set on press); head is the moving end (updated on drag). The zero value is none.
type promptSel struct {
	active             bool
	anchorOff, headOff int
	anchorVis, headVis cell
}

// contentCell is an absolute position in the rendered transcript: line indexes into the cached
// m.lines and col is the display column within that line. The transcript selection anchors in
// these content coordinates (not screen coordinates) so it survives a wheel-scroll mid-drag —
// the scroll moves what is on screen, not the line the anchor names.
type contentCell struct{ line, col int }

// transcriptSel is the transcript's drag-selection in content coordinates. anchor is the drag's
// fixed end (set on press); head is the moving end (updated on drag). The zero value is none.
// Unlike promptSel it stores no rune offsets: the copied text is sliced from the rendered lines
// (screen-space), not from a source string.
type transcriptSel struct {
	active       bool
	anchor, head contentCell
}

// spanUnchanged reports whether the selection's ground held still across a re-render: every line
// it spans is identical in the outgoing lines and the incoming ones. It is the keep-if-unchanged
// rule refreshViewport decides a selection's fate by — true keeps it, false drops it.
//
// An inactive selection has nothing to keep, so it reports false. An active COLLAPSED one
// (anchor == head) is kept whatever moved beneath it, and that exemption is this rule's own purpose
// read literally: the drop exists so a highlight never stands over text that has since been
// rewritten, and a collapsed span paints no highlight at all — shadeCells returns on exactly that
// test. Collapsed, it is not a selection but a CLICK IN PROGRESS: the press's record of the line
// the human pressed on, which the release consumes to toggle a block (handleMouseRelease →
// toggleBlockAt). Dropping it threw that answer away — a live block's header alternates ✦ with a
// bare cell on the spinner phase (blockState.star), rewriting its own line twice a second, so a
// press on a running tool's header was zeroed before the button came up and the toggle never
// fired. A kept
// anchor may outlive the paint it was taken in — a collapse elsewhere can leave it past the last
// marked line — and that costs nothing: toggleBlockAt's bounds check makes a stale line a no-op.
//
// A DRAGGED span is judged exactly as before. One reaching past either slice reports false: the
// lines it named are no longer all there, which is a change by any reading. Only the LINE range is
// normalised (anchor above head or below it names the same rows), and the columns need no re-check
// of their own — identical lines have identical widths, so a column that was valid still is.
func (s transcriptSel) spanUnchanged(oldLines, newLines []string) bool {
	if !s.active {
		return false
	}
	if s.anchor == s.head {
		return true // a click in progress: it shades nothing, so nothing under it can go stale
	}
	top, bot := s.anchor.line, s.head.line
	if bot < top {
		top, bot = bot, top
	}
	if top < 0 || bot >= len(oldLines) || bot >= len(newLines) {
		return false
	}
	for i := top; i <= bot; i++ {
		if oldLines[i] != newLines[i] {
			return false
		}
	}
	return true
}

// flashClearMsg clears the transient status-line note (m.flash) once flashDuration elapses.
type flashClearMsg struct{}

// flashDuration is how long a mouse-copy confirmation lingers in the status line.
const flashDuration = 2 * time.Second

// inputEditable reports whether the prompt is live for the human to edit — the states in which a
// keypress reaches the textarea and a mouse click positions the caret. Editability IS the rule:
// idle (a message to send), awaitingAsk (the borrowed answer box), and running (a message staged
// as an interjection, ADR 0025). At awaitingApproval and errored the box is inert — a/d/s and
// Enter-dismiss own the keyboard there — so clicks fall through to the transcript arbitration.
func (m Model) inputEditable() bool {
	return (m.state == stateIdle || m.state == stateAwaitingAsk || m.state == stateRunning) &&
		m.input.Focused()
}

// inputContentRect returns the textarea text area's on-screen rectangle: the top-left cell
// (x0,y0) and its width and height in cells. The input box is bottom-anchored above the footer's
// single line and the ▁ hairline under it (View stacks the flexible viewport above them), so the
// rectangle follows from the window height and the box's own height without tracking the overlays
// that float above it.
//
// boxTop is stated in the LAYOUT's own constants rather than in literals, so the mapping cannot
// drift from the arithmetic transcriptBudget spends: the box occupies its content rows plus
// inputBorderRows (it closes its own rounded frame), and what stands below it is the footer line
// and the bottom hairline. Every one of the three is named here — an omitted term is exactly the
// off-by-one that puts the caret on the wrong row.
func (m Model) inputContentRect() (x0, y0, w, h int) {
	h = m.input.Height()
	boxTop := m.height - bottomRuleHeight - footerHeight - (h + inputBorderRows) // the box's top border row
	y0 = boxTop + 1                                                              // the first text row sits below that border
	x0 = borderFrame/2 + inputPadding/2                                          // one border column + one padding column = 2 in from the left
	w = m.inputInnerWidth()
	return x0, y0, w, h
}

// pointInputRow maps a screen point to a visual position inside the textarea content. ok is
// false when the point is above or below the text rows (so a drag that strays out of the box
// vertically is ignored). A point left or right of the text clamps to the row's ends, so a drag
// past the edge still selects to the line boundary. ScrollYOffset folds in the textarea's own
// vertical scroll, so the mapping holds even for a prompt taller than the box.
func (m Model) pointInputRow(x, y int) (visRow, visCol int, ok bool) {
	x0, y0, w, h := m.inputContentRect()
	if h < 1 || y < y0 || y >= y0+h {
		return 0, 0, false
	}
	visRow = m.input.ScrollYOffset() + (y - y0)
	visCol = clampInt(x-x0, 0, w)
	return visRow, visCol, true
}

// pointTranscriptRow maps a screen point to a content coordinate in the rendered transcript: the
// line index into m.lines and the display cell within it. The viewport is top-anchored at the
// screen origin (View stacks it first) and its content spans whatever width layout() gave it: the
// window less the scroll-bar gutter while the bar is shown (ui.show-scrollbar), the whole window
// width once it is hidden and the column goes back to the body. The column bound below is therefore
// ASKED of the viewport rather than restating either arithmetic, so the mapping holds in both
// states. ok is false when the point falls outside the transcript's rows or past the last rendered
// line — a session shorter than the viewport leaves the rows beneath its tail empty, and a click
// there names no content at all, so it selects nothing. contentLineAt (model.go) resolves the row, folding in the scroll offset, the
// sticky-header overlay and the rows this frame's overlays took off the bottom
// ([Model.transcriptRows]) — so a click on a header row names the prompt line drawn there rather
// than the reply line hidden beneath it, a click on a popup row names nothing at all, and the
// mapping holds at any scroll position.
//
// The vertical bound is that same derivation and NOT the stored viewport height, which is the whole
// of what makes the arbitration honest: the height layout() stored is the transcript's only while
// no overlay is open, and the rows between the drawn transcript and the input box are painted by an
// approval popup or the /sessions browser. Bounding by the stored height let a click there arm a
// selection over the reply lines the popup covers — invisible text, copied to the clipboard on
// release.
//
// The column needs no conversion, and that is a CONSEQUENCE, not a coincidence. The terminal
// reports x as a PAINTED cell index — the column the glyph under the pointer occupies on screen —
// while the returned col indexes the rendered line by display cell for the cuts downstream
// (transcriptSelectionText, shadeCells). Those two spaces coincide exactly while the display-width
// authority measures the way the painter paints (width.go), which is the whole of what makes it an
// authority; the transcript body starts at screen column 0, so the identity holds outright. It is
// the CUTS that have to speak the authority — measure the line in one method and slice it in
// another and the pointer names one glyph while the clipboard takes its neighbour.
func (m Model) pointTranscriptRow(x, y int) (line, col int, ok bool) {
	line = m.contentLineAt(y) // −1 off the transcript's own rows, overlay rows included
	if line < 0 || line >= len(m.lines) {
		return 0, 0, false
	}
	col = clampInt(x, 0, m.viewport.Width())
	return line, col, true
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
// column: the last offset whose text still fits inside cells (runesWidth, inputaccent.go). A column
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
// whose default rewrites each '\t' as four spaces — and every write path funnels through that one
// sanitiser: SetValue and InsertString, InsertRune, both paste messages, and the key-press default.
// A draft therefore cannot HOLD a tab, so the value this reads (and the value the accent overlay
// reads, inputaccent.go) is tab-free by construction. The tab-measurement defects fixed elsewhere in
// the package — where text arriving from the model or the disk still carried its tabs past a ruler
// that counts them as nothing — have no instance on the prompt box's side of the line.
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

// handleMouseClick starts a fresh, collapsed selection under a left-click. It arbitrates by
// region: a click on the open /settings pane's row list belongs to the pane (selecting a row, or
// seating the caret in the row being typed into); a click in the prompt's editable text area
// positions the caret and arms a prompt selection there; otherwise a click in the transcript viewport
// arms a transcript selection at that rendered cell; a click in none of them clears all three. Starting
// one selection clears the others, so no two coexist. Non-left buttons are ignored.
//
// The pane is asked FIRST because it is drawn over the transcript, and only for its own rows: it takes
// no more of the frame than its list needs, so the transcript above it keeps its pointer.
func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if next, claimed := m.handleSettingsClick(msg); claimed {
		return next, nil
	}
	m.settings.sel = promptSel{} // the pane did not claim this click: its highlight goes, as the other two would
	if m.inputEditable() {
		if visRow, visCol, ok := m.pointInputRow(msg.X, msg.Y); ok {
			m.transcriptSel = transcriptSel{} // the prompt claims it: drop any transcript selection
			m.dropRecall()                    // a click IN the box is acting in it: the arrows go back to the caret
			off := m.caretTo(visRow, visCol)
			m.sel = promptSel{
				active:    true,
				anchorOff: off, headOff: off,
				anchorVis: cell{visRow, visCol}, headVis: cell{visRow, visCol},
			}
			return m, nil
		}
	}
	if line, col, ok := m.pointTranscriptRow(msg.X, msg.Y); ok {
		m.sel = promptSel{} // the transcript claims it: drop any prompt selection
		m.transcriptSel = transcriptSel{
			active: true,
			anchor: contentCell{line, col}, head: contentCell{line, col},
		}
		return m, nil
	}
	m.sel = promptSel{} // a click off both fields deselects
	m.transcriptSel = transcriptSel{}
	return m, nil
}

// handleMouseMotion extends whichever selection is live as the mouse drags with the left button
// held: head advances while the click-set anchor stays put. A drag never STARTS a selection
// (only a click does), so at most one of the two is active and they cannot both extend. Motion
// outside the owning rectangle is ignored so a stray past the edge does not collapse or hijack
// the selection. CellMotion reports motion only while a button is down, so this fires only
// mid-drag.
func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if next, claimed := m.handleSettingsMotion(msg); claimed {
		return next, nil
	}
	if m.sel.active && m.inputEditable() {
		if visRow, visCol, ok := m.pointInputRow(msg.X, msg.Y); ok {
			off := m.caretTo(visRow, visCol)
			m.sel.headOff = off
			m.sel.headVis = cell{visRow, visCol}
		}
		return m, nil
	}
	if m.transcriptSel.active {
		if line, col, ok := m.pointTranscriptRow(msg.X, msg.Y); ok {
			m.transcriptSel.head = contentCell{line, col}
		}
	}
	return m, nil
}

// handleMouseRelease finalises a drag on whichever selection is live. A non-empty span is copied
// to the system clipboard over OSC52 and a transient note confirms it; the highlight stays until
// the next click or edit — or, for a transcript span, until the lines under it change
// (spanUnchanged) — so the human sees what was taken, even while the model keeps streaming beneath
// it. A bare click (anchor == head) is not a selection and just leaves the caret/anchor where it
// landed. The prompt copies the exact typed runes; the transcript copies the rendered text under
// the span.
//
// A bare click in the TRANSCRIPT has one further meaning: on a block's click surface it toggles
// that block (toggleBlockAt), at the line the PRESS anchored on rather than at whatever the release
// point names by then. MOTION is what arbitrates, exactly as it already separates
// click-to-position from drag in the prompt — a drag that starts on a header line is a
// drag-select like any other, because it never reaches this branch.
func (m Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	switch {
	case m.settings.sel.active:
		// The /settings edit field copies what a release took, exactly as the prompt does: the value is
		// a string the human is typing, so the exact runes are what belongs on the clipboard. A bare
		// click leaves the caret where it landed and copies nothing.
		if m.settings.sel.anchorOff == m.settings.sel.headOff {
			m.settings.sel.active = false
			return m, nil
		}
		text := selectionText(m.settings.editor.value(), m.settings.sel.anchorOff, m.settings.sel.headOff)
		if text == "" {
			m.settings.sel.active = false
			return m, nil
		}
		return m.copyFlash(text)
	case m.sel.active:
		if m.sel.anchorOff == m.sel.headOff {
			m.sel.active = false
			return m, nil
		}
		text := selectionText(m.input.Value(), m.sel.anchorOff, m.sel.headOff)
		if text == "" {
			m.sel.active = false
			return m, nil
		}
		return m.copyFlash(text)
	case m.transcriptSel.active:
		if m.transcriptSel.anchor == m.transcriptSel.head {
			pressed := m.transcriptSel.anchor.line
			m.transcriptSel.active = false
			return m.toggleBlockAt(pressed, msg.Y)
		}
		text := transcriptSelectionText(m.th.measure, m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
		if strings.TrimSpace(text) == "" {
			m.transcriptSel.active = false // a drag that took only blank rows copies nothing
			return m, nil
		}
		return m.copyFlash(text)
	}
	return m, nil
}

// toggleBlockAt changes the state of the block whose click surface a MOTIONLESS click landed on
// (layout.md, "Collapsed and expanded blocks"): a toggle line flips what it names, and a
// `+N more lines` marker expands the block whose body it is counting for — never collapses it,
// because the marker is a line of the collapsed paint alone, so a click there can only mean "show
// me the rest". A line that is neither is left exactly as it was: everywhere else in the transcript
// a click keeps its selection meaning, which is the overwhelmingly common case and the one this
// returns on first.
//
// WHAT a toggle line names is the paint's business and not this function's, which is why one case
// covers a block header and a group member alike: inside a folded run every row of a member — its
// one collapsed row, and, open, its target rows, its body and the see-less row closing it — is
// marked for that member's OWN entry, so this same flip opens the third of ten reads and leaves the
// other nine exactly where they were (render.go, lineMark).
//
// line is the PRESS's own content line, not the release point's, and THAT is what makes the toggle
// land while a reply streams. The press already stored the scroll-immune answer: transcriptSel.
// anchor is in content coordinates, which are append-stable by design, whereas re-resolving the
// release point through pointTranscriptRow reads the LIVE scroll offset — and refreshViewport ends
// every streamed event at GotoBottom, so in the 50–150 ms a human takes to let a button up the
// content under a motionless pointer has already moved, and the release names some other line,
// almost always no target at all. One press, one intent, the same answer at any stream rate.
//
// releaseRow stays a SCREEN row because the anchoring half of the rule is a screen fact: the
// toggled line goes back on the row the pointer is resting on (refreshViewportAnchored), so what
// the human is looking at holds still while the body appears or goes.
//
// The lookup reads the PAINT'S OWN accounting — m.lineTargets, which refreshViewport stashes from
// the marks the painter made as it emitted each line (render.go). One accounting for what is drawn
// where, so the block that flips is the block under the cursor and never its neighbour; the bounds
// check is the same defence the stash's builder states, and it earns its keep twice over here,
// since an anchor OUTLIVES the paint it was taken in — a collapse elsewhere can leave it past the
// last marked line, and a stale index would be a click toggling some other block.
//
// Nothing here re-derives the transcript: an index the entry list has grown past, or one naming a
// kind with no block state, answers false from the transcript's own guard and this changes
// nothing at all.
func (m Model) toggleBlockAt(line, releaseRow int) (tea.Model, tea.Cmd) {
	if line < 0 || line >= len(m.lineTargets) {
		return m, nil
	}
	target := m.lineTargets[line]
	switch target.kind {
	case targetHeader:
		if !m.transcript.toggleExpanded(target.entry) {
			return m, nil
		}
	case targetMarker:
		if !m.transcript.setExpanded(target.entry, true) {
			return m, nil
		}
	default:
		return m, nil
	}
	m.refreshViewportAnchored(line, releaseRow)
	return m, nil
}

// copyFlash copies text to the system clipboard over OSC52 (tea.SetClipboard — cross-terminal and
// SSH-safe, no pbcopy dependency) and shows a transient confirmation counting the runes taken
// (flashClearMsg clears it after flashDuration). Shared by the prompt and transcript drag-release
// paths so both confirm a copy identically.
func (m Model) copyFlash(text string) (tea.Model, tea.Cmd) {
	n := len([]rune(text))
	noun := "chars"
	if n == 1 {
		noun = "char"
	}
	m.flash = fmt.Sprintf("copied %d %s", n, noun)
	return m, tea.Batch(
		tea.SetClipboard(text),
		tea.Tick(flashDuration, func(time.Time) tea.Msg { return flashClearMsg{} }),
	)
}

// highlightInput overlays the drag-selection's background on the textarea's rendered block. It
// works purely in visual-cell space — shading the cells between the selection's two ends on the
// already-wrapped lines — so it needs no copy of the textarea's wrap. ScrollYOffset maps the
// stored absolute rows onto the visible block. With no active (non-empty) selection the view is
// returned unchanged.
func (m Model) highlightInput(view string) string {
	if !m.sel.active || m.sel.anchorOff == m.sel.headOff {
		return view
	}
	top, bot := m.sel.anchorVis, m.sel.headVis
	if bot.row < top.row || (bot.row == top.row && bot.col < top.col) {
		top, bot = bot, top // normalise to reading order
	}
	scroll := m.input.ScrollYOffset()
	lines := strings.Split(view, "\n")
	for r := range lines {
		absRow := scroll + r
		if absRow < top.row || absRow > bot.row {
			continue
		}
		c0, c1 := 0, m.th.measure.Width(lines[r])
		if absRow == top.row {
			c0 = top.col
		}
		if absRow == bot.row {
			c1 = bot.col
		}
		if c1 <= c0 {
			continue
		}
		lines[r] = shadeCells(m.th.measure, lines[r], c0, c1, m.th.selection)
	}
	return strings.Join(lines, "\n")
}

// shadeCells re-renders the display columns [c0,c1) of an ANSI line under style. The flanking
// parts keep their original styling (the cut slices by display cell without breaking escape
// codes); the selected span is stripped and re-rendered so the selection colours win. The
// prompt text is single-styled, so a stripped span loses nothing there — the caret is not part of
// the rendered content at all: the terminal draws the real cursor over the frame (steadyCursor).
//
// It slices through the width authority (width.go) rather than the package-level ansi.Cut, which
// is hard-wired to GraphemeWidth. c0/c1 are painted columns — a mouse report's own space, or a
// token's cells on a row the widget already drew — so a cut in any measure but the painter's
// shades a different run of cells than the one that is on screen there.
func shadeCells(measure widthAuthority, line string, c0, c1 int, style lipgloss.Style) string {
	w := measure.Width(line)
	left := measure.Cut(line, 0, c0)
	mid := measure.Cut(line, c0, c1)
	right := measure.Cut(line, c1, w)
	return left + style.Render(ansi.Strip(mid)) + right
}

// transcriptSelectionText extracts the plain text under a content-coordinate span from the
// cached rendered lines — the "copy what you see" slice. It normalises the span to reading order,
// then for each spanned line cuts the display-cell range [c0,c1) with the width authority
// (cell-accurate and escape-safe), strips the styling, and trims the block's trailing pad; the
// lines join with '\n'. The first and last lines cut to the span's own columns; the lines between
// take the whole width. Markers, rail gutters, and soft-wrap breaks are copied verbatim — the
// accepted terminal-native semantics of a screen-space selection (D4).
//
// The measure is the authority's (width.go) rather than ansi.Cut's hard-wired GraphemeWidth
// because the columns came from a mouse report, which counts PAINTED cells: cutting in the measure
// the terminal paints in is what makes the clipboard hold the glyphs the pointer ran over.
func transcriptSelectionText(measure widthAuthority, lines []string, a, b contentCell) string {
	top, bot := a, b
	if bot.line < top.line || (bot.line == top.line && bot.col < top.col) {
		top, bot = bot, top // normalise to reading order
	}
	out := make([]string, 0, bot.line-top.line+1)
	for row := top.line; row <= bot.line; row++ {
		if row < 0 || row >= len(lines) {
			continue
		}
		line := lines[row]
		c0, c1 := 0, measure.Width(line)
		if row == top.line {
			c0 = top.col
		}
		if row == bot.line {
			c1 = bot.col
		}
		if c1 <= c0 {
			out = append(out, "") // an empty or fully-clipped row is a blank line in the copy
			continue
		}
		out = append(out, strings.TrimRight(ansi.Strip(measure.Cut(line, c0, c1)), " "))
	}
	return strings.Join(out, "\n")
}

// ----------------------------------------------------------------------------
// Mouse in the /settings pane (settings.go, docs/layout/settings-screen-layout.md requirement 7)
// ----------------------------------------------------------------------------
//
// A third rectangle joins the prompt's and the transcript's, and it claims the pointer only where the
// pane is actually DRAWN. /settings is the frame's one full-height pane but it takes only the rows its
// list needs (frameRowPlan), so on a tall window the transcript is still on screen above it and a drag
// up there copies what it always did. Inside the pane the pointer does what the keys do: a click on a
// key row selects it, the wheel walks the list, and on the row being typed into a click seats the
// caret and a drag selects the value's text.
//
// The mapping is the PAINTER's own arithmetic rather than a second copy of it. The pane composes its
// spec once for both readers (settingsKeyListSpec) and renderPopupPlaced reports where the row block
// landed among the pane's painted rows and which window of rows it holds (popupPlacement) — so the row
// under the pointer is the row the frame drew there at every description length, window height and
// scroll position. The one fact stated on this side is where the pane BEGINS on the screen, and that is
// read off the frame's own stacking (View): the transcript's rows, the single gap row above the overlay
// slot, and whatever else that slot is holding.
//
// The MULTI-LINE field is the same three gestures over a surface that is all field: it replaces the key
// list rather than one row of it (renderSettingsText), so a click seats the caret at the glyph under the
// pointer wherever in the prompt that is, a drag selects across its lines, and the wheel walks it a line
// at a time. Its geometry is read the same way and needs one thing more — the lines each of the prompt's
// lines WRAPPED to, which the placement now carries (popupPlacement.blocks) and popupWrapOffsets reads
// backwards into the prompt's own rune offsets.
//
// Two states take no pointer. The value sub-list replaces the key list with a menu of a different shape
// (renderSettingsEnum), and an armed reset is a question waiting on ⏎ or esc; a click in either names
// nothing rather than guessing at the list it covers. And while a value is being typed the selection
// never MOVES: the buffer belongs to the selected row (settingsBufferTarget), so a click that walked
// the highlight would leave the field editing a key it is no longer drawn on.

// settingsPaneRect is where the open /settings pane is drawn: the screen row its top border lands on
// and how many rows the pane takes. ok is false when it is not on the frame at all — closed, or given
// way to a window too short to seat it (settingsGiveWayNote).
//
// The row is derived from the frame's OWN stacking rather than counted up from the bottom the way the
// input box is (inputContentRect): View draws the transcript first, then the single blank gap row, then
// the transcript-side overlay slot this pane shares with the approval prompt, the browser and the
// picker. Every one of those terms is named here — the slot's other tenants included, though none of
// them can be open beside this pane today — because an omitted term is exactly the off-by-one that puts
// the click on the wrong row.
func (m Model) settingsPaneRect() (y0, h int, ok bool) {
	if !m.settings.open {
		// Asked before the frame is composed, because every click and every wheel notch asks: with no
		// pane up there is nothing to place, and composing the frame's overlays to learn that would put
		// a render on the path of a click the pane has no part in.
		return 0, 0, false
	}
	ov := m.frameOverlays()
	if ov.settings == "" {
		return 0, 0, false
	}
	y0 = ov.transcriptRows(m.transcriptBudget()) + gapHeight
	for _, above := range []string{ov.prompt, ov.browser, ov.picker} {
		if above != "" {
			y0 += lipgloss.Height(above)
		}
	}
	return y0, lipgloss.Height(ov.settings), true
}

// settingsPaint is the open key list as it was DRAWN this frame: the display rows the pane composed,
// the screen row its row block starts on, which display row that first line shows, and how many row
// lines there are. It is everything a pointer needs in order to name what is under it.
//
// Mapping a screen row back to a display row is a subtraction because a settings row is exactly one
// painted line — the pane asks the module for neither wrapped rows nor row gaps (settingsKeyListSpec),
// which is the condition popupPlacement states for reading it this way.
type settingsPaint struct {
	display settingsDisplay
	y0      int // screen row of the row block's first line
	start   int // the display row that line shows
	rows    int // how many row lines are drawn
}

// settingsPaint composes the open pane exactly as the frame does and reports where its rows landed. ok
// is false wherever there is nothing to address: the pane closed or given way, or a second step up that
// takes no pointer.
//
// It renders the pane to get the answer, which is the same price the transcript mapping already pays
// (contentLineAt spends a frameOverlays of its own on every click): the painter is the authority on
// where a row is, and asking it costs less than a second arithmetic that can disagree with it.
func (m Model) settingsPaint() (settingsPaint, bool) {
	paneTop, _, ok := m.settingsPaneRect()
	if !ok {
		return settingsPaint{}, false
	}
	rows := m.settingRows()
	if _, sub := m.settingsEnumTarget(rows); sub {
		return settingsPaint{}, false // the value sub-list is a menu of its own; no pointer names it
	}
	if _, text := m.settingsTextTarget(rows); text {
		return settingsPaint{}, false // the multi-line field replaced the list: settingsTextPaint answers there
	}
	spec, display, seated := m.settingsKeyListSpec(rows)
	if !seated {
		return settingsPaint{}, false
	}
	_, place := renderPopupPlaced(m.th, spec, m.width)
	return settingsPaint{
		display: display,
		y0:      paneTop + place.rowsAt,
		start:   place.start,
		rows:    place.end - place.start,
	}, true
}

// rowAt maps a screen row to the DISPLAY row drawn on it — the pane's own list index, its section
// labels and spacers included. ok is false above or below the drawn window, so a click on the pane's
// title, its description header or its legend names no row.
func (p settingsPaint) rowAt(y int) (int, bool) {
	if p.rows < 1 || y < p.y0 || y >= p.y0+p.rows {
		return 0, false
	}
	return p.start + (y - p.y0), true
}

// settingsContentX is the screen column a pane's content lines begin at: the box's left border glyph
// and the padding cell drawn inside it, both asked of the border style the box is drawn with
// (drawTitledBox reads the same two).
func (m Model) settingsContentX() int {
	return m.th.popupBorder.GetBorderLeftSize() + m.th.popupBorder.GetPaddingLeft()
}

// settingsValueX is the screen column a row's VALUE cell starts at: the pane's content origin, the
// marker column every row leads with (popupRowIndent), and the columns laid out before the value
// (popupCellColumn, over the widths the module measured across the whole list). Both readers of the
// edit row spend it — the click that seats a caret and the highlight that shades a span — so the two
// can never land on different columns of the same row.
func (m Model) settingsValueX(display settingsDisplay) int {
	return m.settingsContentX() + popupRowIndent +
		popupCellColumn(m.th, popupColumnWidths(m.th, display.rows), settingsValueColumn)
}

// settingsCaretAt is the rune offset in the edit field's value that a display-cell column of its value
// cell names. The cell is the row's PAINTED text — the caret glyph standing in it included
// (settingsEditText) — so the column is read in the painter's own measure (cellToRuneOffsetIn) and the
// glyph is then taken back out: it occupies a cell at the caret, and every rune after it is painted one
// position further along than it stands in the value.
func (m Model) settingsCaretAt(cells int) int {
	text := []rune(m.settingsEditText())
	off := cellToRuneOffsetIn(m.th.measure, text, cells)
	if off > m.settings.editor.caretRune() {
		off-- // past the caret glyph: the painted position is one ahead of the value's own
	}
	return off
}

// settingsEditCells is the display-cell span a range of the field's value occupies inside the value
// cell — settingsCaretAt read the other way, for the highlight. The offsets are mapped THROUGH the
// painted text, the caret glyph shifting everything after it one position along, and measured in the
// painter's authority, so the shaded run covers exactly the glyphs a release would copy.
func (m Model) settingsEditCells(a, b int) (int, int) {
	text := []rune(m.settingsEditText())
	if len(text) == 0 {
		return 0, 0 // no field open: the caret glyph alone is one rune, so this is "nothing to shade"
	}
	caret := m.settings.editor.caretRune()
	value := len(text) - 1 // the painted text is the value plus the caret glyph
	cell := func(off int) int {
		off = clampInt(off, 0, value)
		if off > caret {
			off++ // step over the caret glyph the painter drew before this rune
		}
		return m.th.measure.Width(string(text[:clampInt(off, 0, len(text))]))
	}
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	return cell(lo), cell(hi)
}

// settingsTextPaint is the open MULTI-LINE field as it was DRAWN this frame: the prompt's lines as they
// were painted (the caret glyph among them), where each line begins as a rune offset into that painted
// text, the lines each of them wrapped to, and which window of them the pane is showing. It is
// settingsPaint's counterpart for the state where the field IS the pane, and it carries one thing more —
// the wrap — because a line of prose can cost several painted rows where a key row never does.
//
// top is the first painted line of the row block, in whichever coordinates the caller asked for: the
// pane's own painted rows for the highlight, the SCREEN's for the pointer. One builder answers both, so
// the row a click names and the row a span is shaded on cannot come apart.
type settingsTextPaint struct {
	starts []int      // where each line begins as a rune offset into the painted text
	blocks [][]string // the lines each of them wrapped to, as the painter broke them
	subs   [][]int    // where each of THOSE begins in its own line (popupWrapOffsets)
	top    int        // the row block's first painted line
	start  int        // the first field line that block shows
	end    int        // one past the last
}

// settingsTextGeometry reads the placement the painter reported into the coordinates a pointer or a
// highlight works in. It re-derives none of the wrap: blocks are the painter's own composition, and the
// only arithmetic here is the running rune offset of each line into the painted text — the one fact the
// module never had, because it is about the VALUE and not about the pane.
func (m Model) settingsTextGeometry(place popupPlacement, top int) settingsTextPaint {
	lines := m.settingsTextLines()
	starts := make([]int, len(lines))
	subs := make([][]int, len(lines))
	off := 0
	for i, line := range lines {
		starts[i] = off
		off += len([]rune(line)) + 1 // the +1 is the '\n' the split removed
		if i < len(place.blocks) {
			subs[i] = popupWrapOffsets(line, place.blocks[i])
		}
	}
	return settingsTextPaint{
		starts: starts,
		blocks: place.blocks,
		subs:   subs,
		top:    top,
		start:  place.start,
		end:    place.end,
	}
}

// settingsTextPaint composes the open field exactly as the frame does and reports where its lines
// landed, in SCREEN rows. ok is false wherever there is nothing to address: the pane closed or given
// way, the field not open, or a frame that cannot seat the pane.
func (m Model) settingsTextPaint() (settingsTextPaint, bool) {
	paneTop, _, ok := m.settingsPaneRect()
	if !ok {
		return settingsTextPaint{}, false
	}
	rows := m.settingRows()
	if _, open := m.settingsTextTarget(rows); !open {
		return settingsTextPaint{}, false
	}
	spec, seated := m.settingsTextSpec(rows)
	if !seated {
		return settingsTextPaint{}, false
	}
	_, place := renderPopupPlaced(m.th, spec, m.width)
	return m.settingsTextGeometry(place, paneTop+place.rowsAt), true
}

// lineAt maps a row to the field LINE drawn on it and to which of that line's wrapped sub-lines the row
// shows. ok is false above or below the drawn window, so a click on the pane's title, its description
// header or its legend names no part of the prompt.
func (p settingsTextPaint) lineAt(y int) (line, sub int, ok bool) {
	row := y - p.top
	if row < 0 {
		return 0, 0, false
	}
	for i := p.start; i < p.end && i < len(p.blocks); i++ {
		if row < len(p.blocks[i]) {
			return i, row, true
		}
		row -= len(p.blocks[i])
	}
	return 0, 0, false
}

// span is the range of PAINTED text one wrapped sub-line covers — its runes, and the offset it begins
// at in the same painted text starts is counted in. It is what both readers of a sub-line need: the
// click converts a column inside it to an offset, and the highlight converts an offset back to a
// column. The bounds check is unreachable defence — every caller walks the window lineAt maps against
// — and it answers with an empty span, which shades nothing and seats no caret.
func (p settingsTextPaint) span(line, sub int) (text []rune, start int) {
	if line < 0 || line >= len(p.blocks) || sub < 0 || sub >= len(p.blocks[line]) {
		return nil, 0
	}
	return []rune(p.blocks[line][sub]), p.starts[line] + p.subs[line][sub]
}

// settingsTextCaretAt is the rune offset in the field's VALUE that a screen point names: the sub-line
// under the pointer, the column read across it in the painter's own measure (cellToRuneOffsetIn), and
// the caret glyph then taken back out — it occupies a cell of its own, so every rune after it is painted
// one position further along than it stands in the value (settingsCaretAt's correction, one field over).
func (m Model) settingsTextCaretAt(p settingsTextPaint, x, y int) (int, bool) {
	line, sub, ok := p.lineAt(y)
	if !ok {
		return 0, false
	}
	text, start := p.span(line, sub)
	cells := max(0, x-m.settingsContentX()-popupRowIndent)
	off := start + cellToRuneOffsetIn(m.th.measure, text, cells)
	if off > m.settings.editor.caretRune() {
		off--
	}
	return off, true
}

// handleSettingsTextClick answers a left-click inside the open multi-line field: it seats the caret at
// the glyph under the pointer and arms a collapsed selection there, exactly as a click in the value
// buffer does. claimed is false off the field's own lines, which leaves the pane's chrome — and the
// transcript above a short pane — to whoever else wants the click.
func (m Model) handleSettingsTextClick(msg tea.MouseClickMsg) (Model, bool) {
	paint, ok := m.settingsTextPaint()
	if !ok {
		return m, false
	}
	off, ok := m.settingsTextCaretAt(paint, msg.X, msg.Y)
	if !ok {
		return m, false
	}
	m.sel, m.transcriptSel = promptSel{}, transcriptSel{}
	m.settings.editor.caretToRune(off)
	m.settings.sel = promptSel{active: true, anchorOff: off, headOff: off}
	return m, true
}

// handleSettingsTextMotion extends the field's selection as the mouse drags with the left button held —
// across its lines, which is the whole difference from the one-row buffer's drag. Motion that strays off
// the field's lines is still the pane's: the span keeps what it had rather than collapsing onto the
// chrome the pointer wandered over.
func (m Model) handleSettingsTextMotion(msg tea.MouseMotionMsg) (Model, bool) {
	paint, ok := m.settingsTextPaint()
	if !ok {
		return m, false
	}
	off, ok := m.settingsTextCaretAt(paint, msg.X, msg.Y)
	if !ok {
		return m, true
	}
	m.settings.editor.caretToRune(off)
	m.settings.sel.headOff = off
	return m, true
}

// highlightSettingsText overlays the multi-line field's drag-selection on the composed pane —
// highlightSettingsEdit's job over a span that can cross lines. Each painted sub-line is shaded for
// exactly the part of the selection it holds, so a span that begins mid-line and ends mid-line three
// lines down lights those three lines and nothing either side of it.
//
// The span is converted from the VALUE's offsets to the PAINTED text's first: the caret glyph stands in
// the cell it is drawn at, so every rune from the caret on is painted one position along (settingsEditCells
// makes the same correction for the value row).
func (m Model) highlightSettingsText(view string, place popupPlacement) string {
	if m.settings.kind != settingsTextEditor || !m.settings.sel.active {
		return view
	}
	lo, hi := m.settings.sel.anchorOff, m.settings.sel.headOff
	if lo > hi {
		lo, hi = hi, lo
	}
	if lo == hi {
		return view // a click in progress shades nothing (promptSel's own rule)
	}
	caret := m.settings.editor.caretRune()
	if lo >= caret {
		lo++ // the span opens at or past the caret: the glyph is painted before it
	}
	if hi > caret {
		hi++ // and the same for its end, which is exclusive
	}
	paint := m.settingsTextGeometry(place, place.rowsAt)
	lines := strings.Split(view, "\n")
	row := paint.top
	x := m.settingsContentX() + popupRowIndent
	for i := paint.start; i < paint.end && i < len(paint.blocks); i++ {
		for sub := range paint.blocks[i] {
			text, start := paint.span(i, sub)
			a, b := max(lo, start), min(hi, start+len(text))
			if b > a && row >= 0 && row < len(lines) {
				c0 := m.th.measure.Width(string(text[:a-start]))
				c1 := m.th.measure.Width(string(text[:b-start]))
				lines[row] = shadeCells(m.th.measure, lines[row], x+c0, x+c1, m.th.selection)
			}
			row++
		}
	}
	return strings.Join(lines, "\n")
}

// handleSettingsClick answers a left-click inside the open /settings pane: on the row being typed into
// it seats the caret at the clicked glyph and arms a selection there, on any other KEY row it moves the
// selection to that row, and on a section label, a spacer or the pane's chrome it does nothing at all.
// ok is false when the point falls outside the pane's row block, which leaves the click to the prompt
// and the transcript exactly as it was.
//
// A click the pane DOES claim drops both of the other selections: starting one selection clears the
// others, the arbitration this file has always made, and here it also says the pointer has moved to
// the surface the keyboard is already on. The converse is the caller's, and it is the half a live
// selection makes silent: a click the pane does NOT claim drops the pane's own span, or the field's
// highlight would keep answering every motion and every release taken elsewhere on the frame.
func (m Model) handleSettingsClick(msg tea.MouseClickMsg) (Model, bool) {
	if m.settings.kind == settingsTextEditor {
		return m.handleSettingsTextClick(msg) // the field IS the pane there: its own geometry answers
	}
	paint, ok := m.settingsPaint()
	if !ok {
		return m, false
	}
	display, ok := paint.rowAt(msg.Y)
	if !ok {
		return m, false
	}
	m.sel, m.transcriptSel = promptSel{}, transcriptSel{}
	switch {
	case m.settings.kind == settingsValueBuffer:
		if display != paint.display.selected {
			break // another row: the buffer stays where it is, and the click is swallowed
		}
		off := m.settingsCaretAt(max(0, msg.X-m.settingsValueX(paint.display)))
		m.settings.editor.caretToRune(off)
		m.settings.sel = promptSel{active: true, anchorOff: off, headOff: off}
	case m.settings.kind == settingsKeyList:
		if key, isKey := paint.display.settingKeyAt(display); isKey {
			m.settings.selected = key
		}
	}
	return m, true
}

// handleSettingsMotion extends the edit field's selection as the mouse drags with the left button held:
// the caret follows the moving end, exactly as it does in the prompt, while the click-set anchor stays
// put. A drag never STARTS one, and motion that strays off the edited row is ignored so a stray past
// its ends neither collapses the span nor hijacks another row.
func (m Model) handleSettingsMotion(msg tea.MouseMotionMsg) (Model, bool) {
	if !m.settings.sel.active {
		return m, false
	}
	if m.settings.kind == settingsTextEditor {
		return m.handleSettingsTextMotion(msg)
	}
	if m.settings.kind != settingsValueBuffer {
		return m, false
	}
	paint, ok := m.settingsPaint()
	if !ok {
		return m, false
	}
	if display, inRows := paint.rowAt(msg.Y); !inRows || display != paint.display.selected {
		return m, true // off the field, but still the pane's drag: the span keeps what it had
	}
	off := m.settingsCaretAt(max(0, msg.X-m.settingsValueX(paint.display)))
	m.settings.editor.caretToRune(off)
	m.settings.sel.headOff = off
	return m, true
}

// settingsWheel walks the /settings key list one row per notch while the pointer is over the pane — the
// keyboard's ↑/↓ under the wheel, with the scroll window following it exactly as it does for them
// (popupRowWindow re-derives around the selection on every frame). handled is false anywhere else,
// which leaves the notch to the transcript scrolling above and behind the pane.
//
// It CLAMPS at the ends where the keys WRAP, and the difference is the gesture rather than an
// inconsistency: ↑/↓ walk a list as a cycle, while a wheel is a scroll — rolling past the last key and
// landing back on the first would move the human somewhere they did not aim. A wheel over a pane that
// is asking something (the value sub-list, an armed reset, an open buffer) moves nothing: the step owns
// the surface until it is answered, as it owns every key.
func (m Model) settingsWheel(msg tea.MouseWheelMsg) (Model, bool) {
	y0, h, ok := m.settingsPaneRect()
	if !ok || msg.Y < y0 || msg.Y >= y0+h {
		return m, false
	}
	if m.settings.kind == settingsTextEditor {
		// The multi-line field scrolls by its CARET, because that is what its window follows
		// (renderSettingsText points selected at the caret's line): one prompt line per notch, the same
		// step ↑/↓ take in it, and the widget's own clamp at the two ends the wheel must not roll past.
		switch msg.Button {
		case tea.MouseWheelUp:
			m.settings.editor.stepLine(-1)
		case tea.MouseWheelDown:
			m.settings.editor.stepLine(1)
		}
		return m, true
	}
	if m.settings.kind != settingsKeyList {
		return m, true
	}
	n := len(m.settingRows())
	sel := m.settingsSelection(n)
	if sel < 0 {
		return m, true // no rows to walk; the pane is showing its own empty-list row
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		m.settings.selected = clampInt(sel-1, 0, n-1)
	case tea.MouseWheelDown:
		m.settings.selected = clampInt(sel+1, 0, n-1)
	}
	return m, true
}

// highlightSettingsEdit overlays the edit row's drag-selection on the composed pane — highlightInput's
// idiom one surface along. It is done on the pane's PAINTED lines rather than in the cell the row is
// composed from because the popup module takes plain, escape-free cells and styles its rows whole
// (doc.go): a pre-shaded cell would hand it the very escapes its contract forbids.
//
// The row is found through the placement the paint reported and the columns through the layout the
// module measured, so a span can never be shaded onto the row above or across the wrong column. With no
// active selection, none of any width, or the edited row scrolled out of the window, the view is
// returned unchanged.
func (m Model) highlightSettingsEdit(view string, display settingsDisplay, place popupPlacement) string {
	if m.settings.kind != settingsValueBuffer || !m.settings.sel.active {
		return view
	}
	if m.settings.sel.anchorOff == m.settings.sel.headOff {
		return view // a click in progress shades nothing (promptSel's own rule)
	}
	if display.selected < place.start || display.selected >= place.end {
		return view
	}
	lines := strings.Split(view, "\n")
	row := place.rowsAt + (display.selected - place.start)
	if row < 0 || row >= len(lines) {
		return view
	}
	x := m.settingsValueX(display)
	c0, c1 := m.settingsEditCells(m.settings.sel.anchorOff, m.settings.sel.headOff)
	if c1 <= c0 {
		return view
	}
	lines[row] = shadeCells(m.th.measure, lines[row], x+c0, x+c1, m.th.selection)
	return strings.Join(lines, "\n")
}

// highlightTranscript overlays the transcript drag-selection's background on the viewport's
// visible block, mirroring highlightInput: it shades the display cells between the selection's
// two content-anchored ends on each visible line. It is handed the transcript block View composed,
// so its rows are the transcript's by construction — it spends the frame's row budget
// ([Model.transcriptRows]) once as a loop bound and maps each row through drawnLineAt, the same
// mapping contentLineAt gives the mouse. So the highlight tracks the selection through a mid-drag
// wheel-scroll, and a header row highlights exactly when the header line under the span is the one
// drawn there. With no active (non-empty) selection the view is returned unchanged.
func (m Model) highlightTranscript(view string) string {
	if !m.transcriptSel.active || m.transcriptSel.anchor == m.transcriptSel.head {
		return view
	}
	top, bot := m.transcriptSel.anchor, m.transcriptSel.head
	if bot.line < top.line || (bot.line == top.line && bot.col < top.col) {
		top, bot = bot, top // normalise to reading order
	}
	drawn := m.transcriptRows()
	lines := strings.Split(view, "\n")
	for r := range lines {
		if r >= drawn {
			break // past the transcript: an overlay owns these rows, nothing of ours is drawn on them
		}
		absRow := m.drawnLineAt(r)
		if absRow < top.line || absRow > bot.line {
			continue
		}
		c0, c1 := 0, m.th.measure.Width(lines[r])
		if absRow == top.line {
			c0 = top.col
		}
		if absRow == bot.line {
			c1 = bot.col
		}
		if c1 <= c0 {
			continue
		}
		lines[r] = shadeCells(m.th.measure, lines[r], c0, c1, m.th.selection)
	}
	return strings.Join(lines, "\n")
}
