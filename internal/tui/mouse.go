package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
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

// flashClearMsg clears the transient status-line note (m.flash) once flashDuration elapses.
type flashClearMsg struct{}

// flashDuration is how long a mouse-copy confirmation lingers in the status line.
const flashDuration = 2 * time.Second

// inputEditable reports whether the prompt is live for the human to edit — the only states in
// which a mouse click positions the caret. While a worker runs (or an approval is pending) the
// input is refused, so clicks there are ignored.
func (m Model) inputEditable() bool {
	return (m.state == stateIdle || m.state == stateAwaitingAsk) && m.input.Focused()
}

// inputContentRect returns the textarea text area's on-screen rectangle: the top-left cell
// (x0,y0) and its width and height in cells. The input box is bottom-anchored above the
// three-row footer (View stacks the flexible viewport above it), so the rectangle follows from
// the window height and the box's own height without tracking the overlays that float above it.
func (m Model) inputContentRect() (x0, y0, w, h int) {
	h = m.input.Height()
	boxTop := m.height - footerHeight - (h + 1) // the box's top border row (the box has no bottom edge)
	y0 = boxTop + 1                             // the first text row sits below that border
	x0 = borderFrame/2 + inputPadding/2         // one border column + one padding column = 2 in from the left
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
// screen origin (View stacks it first) and its content spans the width left of the scroll-bar
// gutter. ok is false when the point falls outside the viewport rows or past the last rendered
// line (the blank pad the sticky-to-top pin leaves below short content), so a click there selects
// nothing. viewport.YOffset folds in the scroll, so the mapping holds at any scroll position.
func (m Model) pointTranscriptRow(x, y int) (line, col int, ok bool) {
	w, h := m.viewport.Width(), m.viewport.Height()
	if h < 1 || y < 0 || y >= h {
		return 0, 0, false
	}
	line = m.viewport.YOffset() + y
	if line < 0 || line >= len(m.lines) {
		return 0, 0, false
	}
	col = clampInt(x, 0, w)
	return line, col, true
}

// reseatCaret drives the textarea caret to an absolute visual (soft-wrapped) row through the
// widget's own primitives: MoveToBegin resets to the top — which "unscrolls" its internal
// viewport to offset 0 — and each CursorDown steps down one visual row, clamping at the end.
// Walking down from the top re-clamps a scroll offset the widget left stale (its SetHeight only
// repositions when the caret falls outside the view, never when the box grows), so the caret
// lands on its real visual row with the least scroll that keeps it visible. It re-derives none
// of the textarea's wrap, so the geometry holds across bubbles releases. Shared by caretTo (a
// mouse click's target row) and reseatInput (the caret's own row, after a height change).
func (m *Model) reseatCaret(visRow int) {
	m.input.MoveToBegin()
	for i := 0; i < visRow; i++ {
		m.input.CursorDown()
	}
}

// caretTo positions the textarea caret at the given absolute visual cell and returns the
// caret's rune offset into the value. It re-seats to the target visual row through reseatCaret
// (the widget's own wrap-aware walk), then LineInfo locates the landed visual line — so the
// result matches what the textarea actually draws without re-deriving its wrap.
func (m *Model) caretTo(visRow, visCol int) int {
	m.reseatCaret(visRow)
	li := m.input.LineInfo()
	// visCol is a display-cell offset from the row's start, but SetCursorColumn indexes runes
	// into the logical line — the two diverge on any CJK/emoji row. Walk the landed visual
	// sub-line's runes, accumulating display width, to convert the cell column to a rune offset;
	// StartColumn (a rune offset) then anchors it back into the logical line. Feeding the raw
	// cell column would drop the caret on the wrong rune, and a drag-copy would then put
	// different text on the clipboard than the highlight showed.
	sub := visualSubline(m.input.Value(), m.input.Line(), li.StartColumn, li.Width)
	m.input.SetCursorColumn(li.StartColumn + cellToRuneOffset(sub, visCol))
	return caretOffset(m.input.Value(), m.input.Line(), m.input.Column())
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
// column, accumulating each rune's width with the same runewidth the textarea's own cursor math
// uses (so the mapping inverts the widget's rendering). A column that lands inside a wide rune
// resolves to that rune's left edge; a column past the run's end returns the full rune count —
// the clamp the caller relies on, expressed in runes rather than cells.
func cellToRuneOffset(runes []rune, cells int) int {
	acc := 0
	for i, r := range runes {
		w := runewidth.RuneWidth(r)
		if acc+w > cells {
			return i
		}
		acc += w
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
// region: a click in the prompt's editable text area positions the caret and arms a prompt
// selection there; otherwise a click in the transcript viewport arms a transcript selection at
// that rendered cell; a click in neither region clears both. Starting one selection clears the
// other, so the two never coexist. Non-left buttons are ignored.
func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.inputEditable() {
		if visRow, visCol, ok := m.pointInputRow(msg.X, msg.Y); ok {
			m.transcriptSel = transcriptSel{} // the prompt claims it: drop any transcript selection
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
// the next click, edit, or transcript change so the human sees what was taken. A bare click
// (anchor == head) is not a selection and just leaves the caret/anchor where it landed. The
// prompt copies the exact typed runes; the transcript copies the rendered text under the span.
func (m Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	switch {
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
			m.transcriptSel.active = false
			return m, nil
		}
		text := transcriptSelectionText(m.lines, m.transcriptSel.anchor, m.transcriptSel.head)
		if strings.TrimSpace(text) == "" {
			m.transcriptSel.active = false // a drag over blank pad copies nothing
			return m, nil
		}
		return m.copyFlash(text)
	}
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
		c0, c1 := 0, lipgloss.Width(lines[r])
		if absRow == top.row {
			c0 = top.col
		}
		if absRow == bot.row {
			c1 = bot.col
		}
		if c1 <= c0 {
			continue
		}
		lines[r] = shadeCells(lines[r], c0, c1, m.th.selection)
	}
	return strings.Join(lines, "\n")
}

// shadeCells re-renders the display columns [c0,c1) of an ANSI line under style. The flanking
// parts keep their original styling (ansi.Cut slices by display cell without breaking escape
// codes); the selected span is stripped and re-rendered so the selection colours win. The
// prompt text is single-styled, so the only thing lost under the span is the cursor block — and
// the caret sits at the selection head anyway.
func shadeCells(line string, c0, c1 int, style lipgloss.Style) string {
	w := lipgloss.Width(line)
	left := ansi.Cut(line, 0, c0)
	mid := ansi.Cut(line, c0, c1)
	right := ansi.Cut(line, c1, w)
	return left + style.Render(ansi.Strip(mid)) + right
}

// transcriptSelectionText extracts the plain text under a content-coordinate span from the
// cached rendered lines — the "copy what you see" slice. It normalises the span to reading order,
// then for each spanned line cuts the display-cell range [c0,c1) with ansi.Cut (cell-accurate and
// escape-safe), strips the styling, and trims the block's trailing pad; the lines join with '\n'.
// The first and last lines cut to the span's own columns; the lines between take the whole width.
// Markers, rail gutters, and soft-wrap breaks are copied verbatim — the accepted terminal-native
// semantics of a screen-space selection (D4).
func transcriptSelectionText(lines []string, a, b contentCell) string {
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
		c0, c1 := 0, lipgloss.Width(line)
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
		out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(line, c0, c1)), " "))
	}
	return strings.Join(out, "\n")
}

// highlightTranscript overlays the transcript drag-selection's background on the viewport's
// visible block, mirroring highlightInput: it shades the display cells between the selection's
// two content-anchored ends on each visible line. viewport.YOffset maps the stored absolute
// content rows onto the visible rows, so the highlight tracks the selection through a mid-drag
// wheel-scroll. It is applied after the sticky header so a header row highlights as a plain
// viewport row. With no active (non-empty) selection the view is returned unchanged.
func (m Model) highlightTranscript(view string) string {
	if !m.transcriptSel.active || m.transcriptSel.anchor == m.transcriptSel.head {
		return view
	}
	top, bot := m.transcriptSel.anchor, m.transcriptSel.head
	if bot.line < top.line || (bot.line == top.line && bot.col < top.col) {
		top, bot = bot, top // normalise to reading order
	}
	off := m.viewport.YOffset()
	lines := strings.Split(view, "\n")
	for r := range lines {
		absRow := off + r
		if absRow < top.line || absRow > bot.line {
			continue
		}
		c0, c1 := 0, lipgloss.Width(lines[r])
		if absRow == top.line {
			c0 = top.col
		}
		if absRow == bot.line {
			c1 = bot.col
		}
		if c1 <= c0 {
			continue
		}
		lines[r] = shadeCells(lines[r], c0, c1, m.th.selection)
	}
	return strings.Join(lines, "\n")
}
