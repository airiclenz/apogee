package tui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ----------------------------------------------------------------------------
// Mouse support for the prompt: click-to-position + drag-to-select (layout.md)
// ----------------------------------------------------------------------------
//
// The terminal does its own click-drag text selection only while no application captures the
// mouse. apogee captures it (MouseModeCellMotion, set in View) so the wheel can scroll the
// transcript and a click can position the caret — which is exactly why the terminal's own
// selection is off. So the prompt's selection is implemented here, on top of the textarea's
// public API: no copy of the widget's internal word-wrap is needed, which keeps the geometry
// correct across bubbles releases.
//
// Scope (the user's choice): the prompt/input box only. Selecting in the transcript is a later
// rung (ISSUES.md).

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

// caretTo positions the textarea caret at the given absolute visual cell and returns the
// caret's rune offset into the value. It drives the caret with the widget's own wrap-aware
// primitives — MoveToBegin resets to the top (and unscrolls), CursorDown steps by visual row
// and clamps at the end, LineInfo locates the landed visual line — so the result matches what
// the textarea actually draws without re-deriving its wrap.
func (m *Model) caretTo(visRow, visCol int) int {
	m.input.MoveToBegin()
	for i := 0; i < visRow; i++ {
		m.input.CursorDown()
	}
	li := m.input.LineInfo()
	m.input.SetCursorColumn(li.StartColumn + min(visCol, li.CharWidth))
	return caretOffset(m.input.Value(), m.input.Line(), m.input.Column())
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

// handleMouseClick positions the caret at a left-click inside the prompt's text area and starts
// a fresh, collapsed selection there. A left-click off the field clears any selection. Only the
// editable states act; other buttons and other states are ignored (transcript clicks are a
// later rung).
func (m Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || !m.inputEditable() {
		return m, nil
	}
	visRow, visCol, ok := m.pointInputRow(msg.X, msg.Y)
	if !ok {
		m.sel = promptSel{} // a click off the field deselects
		return m, nil
	}
	off := m.caretTo(visRow, visCol)
	m.sel = promptSel{
		active:    true,
		anchorOff: off, headOff: off,
		anchorVis: cell{visRow, visCol}, headVis: cell{visRow, visCol},
	}
	return m, nil
}

// handleMouseMotion extends the selection as the mouse drags with the left button held: the
// caret tracks the drag head (as in a GUI editor) and head advances while the click-set anchor
// stays put. Motion outside the text rows is ignored so a vertical stray does not collapse the
// selection. CellMotion reports motion only while a button is down, so this fires only mid-drag.
func (m Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	if !m.sel.active || msg.Button != tea.MouseLeft || !m.inputEditable() {
		return m, nil
	}
	visRow, visCol, ok := m.pointInputRow(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	off := m.caretTo(visRow, visCol)
	m.sel.headOff = off
	m.sel.headVis = cell{visRow, visCol}
	return m, nil
}

// handleMouseRelease finalises a drag. A non-empty selection is copied to the system clipboard
// over OSC52 (tea.SetClipboard — cross-terminal and SSH-safe, no pbcopy dependency) and a
// transient note confirms it; the highlight stays until the next click or edit so the human
// sees what was taken. A bare click (anchor == head) is not a selection and just leaves the
// caret where it landed.
func (m Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if !m.sel.active {
		return m, nil
	}
	if m.sel.anchorOff == m.sel.headOff {
		m.sel.active = false
		return m, nil
	}
	text := selectionText(m.input.Value(), m.sel.anchorOff, m.sel.headOff)
	if text == "" {
		m.sel.active = false
		return m, nil
	}
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
