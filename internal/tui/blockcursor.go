package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The transcript's keyboard block cursor (docs/layout/tool-layout.md, design call 7)
// ----------------------------------------------------------------------------
//
// A tool block folds and unfolds under the pointer (mouse.go), and this is the same reach for a
// human whose hands are on the keys: a MODAL highlight that walks the transcript's click surfaces
// and opens the one it is standing on. Modal, because the transcript's keys are already spoken for
// — ↑/↓ walk prompt recall and move the caret, ⏎ sends — so the walk has to be entered before it
// can claim any of them, and it is entered by a chord nothing else binds: ⌥↑ / ⌥↓.
//
// What it walks is not a second accounting of what is foldable. It is the paint's OWN click map
// (blocktarget.go, [lineTarget]) — the very slice the mouse resolves a click against — so a row a
// pointer can open is a row this cursor stops on, at whatever level the painter marked it: an
// umbrella header, a type row, a group member, a single block. "Deepest visible level" needs no
// rule of its own here, because the painter already marked each line for its deepest owner and a
// level that is folded away paints no lines at all.
//
// The state is two plain fields on the value-copied Model (ADR 0011): whether the mode is on, and
// which CONTENT line the highlight is on. A content line rather than a screen row because the
// scrollback is the stable coordinate — the same choice the mouse's press anchor makes — so a
// scroll, a stream of new output or a repaint moves the highlight with the text it is pointing at
// instead of leaving it on a row that now shows something else.
type blockCursor struct {
	active bool // the mode is on: ↑/↓/⏎/esc are the transcript's until a printable key hands them back
	line   int  // the index into Model.lines / Model.lineTargets the highlight is standing on
}

// cursorStops is the ordered list of content lines the cursor may stand on: the FIRST line of every
// run of adjacent lines that share one click surface.
//
// The collapsing is the whole difference between a pointer and a cursor. A block marks every line
// it paints for the entry a click there flips — a wrapped header, the branch row, each body row —
// which is right for a pointer, since a human clicks where they are already looking; walking those
// lines one at a time would instead make ⌥↑ crawl through a forty-line body to reach the block
// above it. One stop per surface, and the surface's first line is the one that names it.
//
// [lineTarget] is compared whole, so two adjacent blocks (different entries) and a member row
// beside its neighbour inside one group (different entries, same kind) stay separate stops — both
// are separate things to a click, and this is exactly the mouse's own map read back.
func cursorStops(targets []lineTarget) []int {
	var stops []int
	prev := lineTarget{}
	for i, target := range targets {
		if target.kind != targetNone && target != prev {
			stops = append(stops, i)
		}
		prev = target
	}
	return stops
}

// clamp keeps an active cursor standing on a stop of the paint that is now on the screen, and ends
// the mode when that paint offers none.
//
// It runs from [Model.refreshViewport] — the one place the click map is restashed — because every
// way the transcript can change goes through there: a toggle rewrites the block under the cursor, a
// streamed result appends below it, /clear takes the whole scrollback away. A cursor left pointing
// at a line whose meaning moved is a ⏎ that opens some other block, which is the mouse's stale-anchor
// hazard with a keypress behind it instead of a pointer.
//
// A line that is no longer a stop falls back to the stop BEFORE it, which is where the reader was
// looking: opening a member of a group moves every stop below it down the body that appeared, and
// the stop before is the surface the human was standing on when they pressed ⏎.
func (c blockCursor) clamp(targets []lineTarget) blockCursor {
	if !c.active {
		return blockCursor{}
	}
	stops := cursorStops(targets)
	if len(stops) == 0 {
		return blockCursor{} // nothing on the screen can be opened: the mode has nothing to be
	}
	i, exact := slices.BinarySearch(stops, c.line)
	if exact {
		return c
	}
	c.line = stops[max(i-1, 0)]
	return c
}

// blockCursorOwnsKeys reports whether the transcript's cursor may hold the frame's keys this frame.
// A pending decision — an approval menu, an ask_user question with choices — owns ↑/↓ and ⏎ for its
// own rows, and a modal cursor that swallowed them would leave a human unable to answer the very
// thing that is blocking the run.
//
// The mode is not ENDED by such a prompt, only suspended: the highlight goes with the keys
// (highlightBlockCursor reads this too) and comes back on the line it was standing on once the
// decision is taken, which is where the reader was before the run interrupted them.
func (m Model) blockCursorOwnsKeys() bool {
	return !m.state.decisionPending()
}

// blockCursorKey is the mode's whole key contract. It returns the next Model whether or not it
// claimed the key, because one of its cases does both: a printable key ENDS the mode and is then
// left to fall through to the prompt, so what the human typed lands in the box rather than being
// swallowed by the walk they were in (design call 7).
//
// ⌥↑ / ⌥↓ both enter and move, so the mode costs no separate gesture to open. Inside it plain ↑/↓
// move, ⏎ opens or closes the surface under the highlight, and esc leaves without typing anything.
// Every other key — ⌃c, PgUp, ⇧⇥ — is left alone and leaves the mode standing: they are the frame's
// own verbs, and none of them is a reason to lose your place in the scrollback.
func (m Model) blockCursorKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	if !m.blockCursorOwnsKeys() {
		return m, nil, false
	}
	switch msg.String() {
	case "alt+up":
		return m.moveBlockCursor(-1), nil, true
	case "alt+down":
		return m.moveBlockCursor(1), nil, true
	}
	if !m.cursor.active {
		return m, nil, false
	}
	switch msg.String() {
	case "up":
		return m.moveBlockCursor(-1), nil, true
	case "down":
		return m.moveBlockCursor(1), nil, true
	case "enter":
		next, cmd := m.toggleAtBlockCursor()
		return next, cmd, true
	case "esc":
		m.cursor = blockCursor{}
		return m, nil, true
	}
	if msg.Text != "" {
		// Typing is the way out that needs no key of its own: the human has stopped reading and
		// started writing. The keypress is NOT claimed — it goes on to the prompt box below and
		// opens the message with the letter they meant to type.
		m.cursor = blockCursor{}
		return m, nil, false
	}
	return m, nil, false
}

// moveBlockCursor moves the highlight one stop in dir, entering the mode when it is not already on.
//
// Entering comes from OUTSIDE the transcript — the prompt box, below everything — so each key lands
// on the end it is pointing away from: ⌥↑ enters at the LAST stop, the most recent block and the one
// a reader looking away from the prompt wants first, and ⌥↓ at the first, which is the oldest thing
// the scrollback still holds. Inside, movement is clamped rather than wrapping: a walk that fell off
// the top and reappeared at the bottom would lose the reader's place in a scrollback that is often
// screens long, and the ask_user choice list already takes the same non-wrapping shape.
//
// A live drag-selection is dropped on the way in. The transcript has one highlight and one meaning
// for it — this is the thing you are pointing at — and the two would otherwise paint at once, which
// is the same region arbitration the prompt's own selection is cleared for on every keypress
// (handleKey).
func (m Model) moveBlockCursor(dir int) Model {
	stops := cursorStops(m.lineTargets)
	if len(stops) == 0 {
		m.cursor = blockCursor{}
		return m
	}
	if !m.cursor.active {
		m.transcriptSel = transcriptSel{}
		m.cursor = blockCursor{active: true, line: stops[len(stops)-1]}
		if dir > 0 {
			m.cursor.line = stops[0]
		}
		m.followBlockCursor()
		return m
	}
	at, _ := slices.BinarySearch(stops, m.cursor.line)
	m.cursor.line = stops[min(max(at+dir, 0), len(stops)-1)]
	m.followBlockCursor()
	return m
}

// blockCursorRow is the screen row the cursor's line is DRAWN on, or −1 when the transcript is not
// showing it. It reads [Model.drawnLineAt] — the mapping the mouse and the selection highlight both
// go through — rather than subtracting the scroll offset itself, so the sticky header's frozen rows
// are answered for correctly: those rows draw the owning prompt's lines and not the offset's, and a
// cursor "at" one of them is a cursor the human cannot see.
func (m Model) blockCursorRow() int {
	if !m.cursor.active {
		return -1
	}
	for row := range m.transcriptRows() {
		if m.drawnLineAt(row) == m.cursor.line {
			return row
		}
	}
	return -1
}

// followBlockCursor scrolls the transcript to wherever the cursor has walked to, and does nothing at
// all while the cursor is already on the screen — a walk down a long body must not yank the view
// every step.
//
// The scroll goes through [Model.refreshViewportAnchored], the same anchoring a click uses, so
// following and toggling park the view by one rule: the cursor's line goes on the row named, and
// detachment is re-derived from where that left the view. The row named is the near EDGE — the top
// when the cursor walked above the view, the bottom when it walked below — so the next step in the
// same direction has a screenful to walk through before the view moves again.
//
// The retry is the sticky header's doing: parking a line on row 0 puts it UNDER the frozen prompt
// rows overlaid there, where the human would see the highlight nowhere at all. Asking again after
// the park — the span is a function of the offset that park just set — moves it to the first row the
// header does not cover.
func (m *Model) followBlockCursor() {
	if !m.cursor.active || m.blockCursorRow() >= 0 {
		return
	}
	rows := m.transcriptRows()
	if rows <= 0 {
		return
	}
	row := rows - 1
	if m.cursor.line < m.viewport.YOffset() {
		row = 0
	}
	m.refreshViewportAnchored(m.cursor.line, row)
	if m.blockCursorRow() >= 0 {
		return
	}
	if _, header := m.stickyHeaderSpan(); header > 0 && header < rows {
		m.refreshViewportAnchored(m.cursor.line, header)
	}
}

// toggleAtBlockCursor opens or closes the surface the highlight is standing on, through the mouse's
// own [Model.toggleBlockAt]: the kinds, what each of them flips, and the anchoring that holds the
// row still while the body appears are one rule for both ways in, and a second copy of it here
// would be the drift the paint's single click map exists to prevent.
//
// The anchor row is where the cursor is DRAWN, so ⏎ holds the highlighted row exactly where the eye
// already is. A cursor the view is not showing cannot be toggled — followBlockCursor keeps that
// unreachable after any move, and refusing beats guessing a row for a line nobody can see.
func (m Model) toggleAtBlockCursor() (Model, tea.Cmd) {
	row := m.blockCursorRow()
	if row < 0 {
		return m, nil
	}
	return m.toggleBlockAt(m.cursor.line, row)
}

// highlightBlockCursor overlays the cursor's bar on the composed transcript block, mirroring
// highlightTranscript (mouse.go): it is handed the transcript's own rows, so it maps its content
// line through the same [Model.drawnLineAt] and paints the row that is actually showing it.
//
// The bar is the theme's SELECTION field — the one tone the frame already spends on "this is the
// thing you are pointing at" (design call 7). It can never be confused with a drag-selection,
// because the two never coexist: entering the mode drops the selection, and a mouse press ends the
// paint of this one by moving the reader's attention back to the pointer.
func (m Model) highlightBlockCursor(view string) string {
	if !m.cursor.active || !m.blockCursorOwnsKeys() {
		return view
	}
	row := m.blockCursorRow()
	if row < 0 {
		return view
	}
	lines := strings.Split(view, "\n")
	if row >= len(lines) {
		return view
	}
	lines[row] = shadeCells(m.th.measure, lines[row], 0, m.th.measure.Width(lines[row]), m.th.selection)
	return strings.Join(lines, "\n")
}
