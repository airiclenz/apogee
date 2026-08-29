package tui

import (
	"strconv"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// Re-asserting mouse tracking after a tool child touched the terminal
// ----------------------------------------------------------------------------
//
// Bubble Tea writes the mouse-tracking escapes only when the frame's MouseMode CHANGES
// (cursed_renderer.go's `view.MouseMode != s.lastView.MouseMode` diff). View sets
// tea.MouseModeCellMotion on every laid-out frame, so from the renderer's side the mode never
// moves and the sequence is written exactly once, at the first laid-out frame.
//
// That is a problem apogee cannot see: a tool child runs on the SAME terminal, and one that
// resets mouse tracking on its way out — an editor, a pager, anything that drives the tty and
// restores its own idea of the mode — turns the terminal's reporting off behind apogee's back.
// The renderer has nothing to re-send (its lastView still says CellMotion), so block toggles,
// drag-select and wheel scrolling stay dead for the rest of the session.
//
// The fix is to say it again ourselves at the two moments the terminal may have been taken over
// or reset: after every tool result, and on every resize. The sequence is idempotent, so
// re-asserting a mode that was never lost costs two escapes nobody sees.

const (
	// mouseTrackingSeq is exactly the byte sequence Bubble Tea's renderer emits for
	// tea.MouseModeCellMotion (ansi.SetModeMouseButtonEvent + ansi.SetModeMouseExtSgr,
	// cursed_renderer.go): button-event tracking plus SGR extended coordinates. Written
	// literally rather than composed from the ansi constants so what apogee re-asserts is
	// visible at the seam that has to match the renderer byte for byte.
	mouseTrackingSeq = "\x1b[?1002h\x1b[?1006h"

	// diagMouseReassert is the --tui-diag key for the re-assert counter (diagnostics.go's
	// change-suppressed log): the value is the running count, so every re-assert is a new line
	// and a bug report shows how many went out and when.
	diagMouseReassert = "mouse-reassert"
)

// reassertMouse re-sends the cell-motion mouse-tracking escapes to the terminal, and returns the
// model carrying the incremented count with the Cmd that writes them. It answers with a nil Cmd
// before the first WindowSizeMsg: the pre-ready frame paints no mouse mode at all (View), so
// asserting one there would enable reporting the renderer never turned on.
//
// The count is a plain int on the value-copied Model (ADR 0011) and exists for the diagnostic: a
// log line per re-assert is what tells a bug report whether the escapes went out at all.
func (m Model) reassertMouse() (Model, tea.Cmd) {
	if !m.ready {
		return m, nil
	}
	m.mouseReasserts++
	m.diag.record(diagMouseReassert, strconv.Itoa(m.mouseReasserts))
	return m, tea.Raw(mouseTrackingSeq)
}
