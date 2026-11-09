package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
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
// The state is plain fields on the value-copied Model (ADR 0011): whether the mode is on, and
// which CONTENT line the highlight is on. A content line rather than a screen row because the
// scrollback is the stable coordinate — the same choice the mouse's press anchor makes — so a
// scroll, a stream of new output or a repaint moves the highlight with the text it is pointing at
// instead of leaving it on a row that now shows something else.
//
// Navigation mode (ADR 0111 D1–3) spans two regions — the transcript and the side panel's column —
// and the cursor stands in one of them at a time while REMEMBERING its place in the other, so → and
// ← move between the two and come back to where the reader left each. The panel's place is a stop
// IDENTITY ([panelStopID]) rather than a row, because the column re-lays out under every Event: a
// row appearing or vanishing above it leaves the cursor on its own row. The row it was last seen on
// is kept beside it as the hint a vanished identity falls back to ([blockCursor.panelStopIndex]).
type blockCursor struct {
	active bool      // the mode is on: ↑/↓/←/→/esc are the mode's until a printable key hands them back
	region navRegion // which region the cursor stands in; the other one's place is only remembered
	line   int       // the index into Model.lines / Model.lineTargets the transcript place is on

	panel    panelStopID // the panel stop the panel place is on, while hasPanel
	panelY   int         // the row that stop was last laid out on: the fallback when its identity is gone
	hasPanel bool        // the panel has a place: the region was entered at least once this mode
}

// navRegion is which of navigation mode's two regions the cursor stands in.
type navRegion int

const (
	regionTranscript navRegion = iota // the transcript's click surfaces (the zero value: the mode began there)
	regionPanel                       // the side panel's stops ([Model.panelStops])
)

// inTranscript reports whether the cursor is on and standing in the transcript — the gate every
// reader of the transcript place answers to, so a place only remembered while the cursor stands in
// the panel is never painted, toggled, stopped or scrolled to.
func (c blockCursor) inTranscript() bool {
	return c.active && c.region == regionTranscript
}

// inPanel reports whether the cursor is on and standing in the side panel.
func (c blockCursor) inPanel() bool {
	return c.active && c.region == regionPanel
}

// standOnPanel is c with its panel place moved to stop.
func (c blockCursor) standOnPanel(stop panelStop) blockCursor {
	c.panel, c.panelY, c.hasPanel = stop.id, stop.y, true
	return c
}

// panelStopIndex is the index in stops of the cursor's panel place (ADR 0111 D5): the stop with its
// identity; or — that row gone from the column — the stop of the same section nearest the row it was
// last on, since a reader whose sub-agent row ended is still reading that section; or, the whole
// section gone, the column's first stop. It is false when the cursor has no panel place or the
// column has no stops.
func (c blockCursor) panelStopIndex(stops []panelStop) (int, bool) {
	if !c.hasPanel || len(stops) == 0 {
		return 0, false
	}
	if at := slices.IndexFunc(stops, func(s panelStop) bool { return s.id == c.panel }); at >= 0 {
		return at, true
	}
	inSection := func(s panelStop) bool { return s.id.section == c.panel.section }
	if at, ok := nearestPanelStop(stops, c.panelY, inSection); ok {
		return at, true
	}
	return 0, true
}

// nearestPanelStop is the index of the stop in stops, among those isCandidate keeps, whose first row
// is closest to row, the upper one on a tie. It is false when isCandidate keeps none.
func nearestPanelStop(stops []panelStop, row int, isCandidate func(panelStop) bool) (int, bool) {
	best := -1
	for i, stop := range stops {
		if !isCandidate(stop) {
			continue
		}
		if best < 0 || abs(stop.y-row) < abs(stops[best].y-row) {
			best = i
		}
	}
	return best, best >= 0
}

// anyPanelStop keeps every stop: [nearestPanelStop] over the whole column.
func anyPanelStop(panelStop) bool { return true }

// abs is n's magnitude.
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
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
//
// The run view's breadcrumb band is the one surface whose first line does NOT name it: the band
// opens on a blank row, and the trail stands on the row below (breadcrumbTrailRow). Its stop is
// that trail row, so the bar lands on the words that say where ⏎ goes, and a screen too short to
// freeze the whole band — which freezes the trail alone (Model.stickyHeaderSpan) — still shows the
// line the cursor stands on.
//
// A padded prompt block is the other: it opens on its ▄ half row (pads, renderedTranscript.pads),
// which carries the block's mark but none of its words, so its stop is the first row past the pad.
func cursorStops(targets []lineTarget, pads []bool) []int {
	var stops []int
	prev := lineTarget{}
	for i, target := range targets {
		if target.kind != targetNone && target != prev {
			stops = append(stops, surfaceStop(targets, pads, i))
		}
		prev = target
	}
	return stops
}

// surfaceStop is the line the cursor stands on for the surface whose first line is first: that line
// itself, except on the breadcrumb band, whose trail sits breadcrumbTrailRow below its opening pad
// row, and on a padded prompt block, whose first row past its ▄ half row is the one with words on
// it. A band shorter than that (a hand-built map), or a pad with nothing of its surface after it,
// keeps its first line.
func surfaceStop(targets []lineTarget, pads []bool, first int) int {
	if first < len(pads) && pads[first] {
		if next := first + 1; next < len(targets) && targets[next] == targets[first] {
			return next
		}
		return first
	}
	if targets[first].kind != targetBreadcrumb {
		return first
	}
	trail := first + breadcrumbTrailRow
	if trail < len(targets) && targets[trail] == targets[first] {
		return trail
	}
	return first
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
//
// The transcript place is kept honest while the cursor stands in the panel too, so ← returns to a
// real stop; a transcript with no stops at all leaves a panel-standing cursor alone. A cursor
// standing in a transcript that lost every stop comes back inactive here, and
// [Model.seatBlockCursor] hands it to the panel or lets the mode end.
func (c blockCursor) clamp(targets []lineTarget, pads []bool) blockCursor {
	if !c.active {
		return blockCursor{}
	}
	stops := cursorStops(targets, pads)
	if len(stops) == 0 {
		if c.region == regionPanel {
			return c
		}
		return blockCursor{} // nothing in the transcript can be opened: this region has nothing to be
	}
	i, exact := slices.BinarySearch(stops, c.line)
	if exact {
		return c
	}
	c.line = stops[max(i-1, 0)]
	return c
}

// seatBlockCursor is the cursor re-seated against the paint that just landed ([Model.refreshViewport]):
// the transcript place clamped onto a stop ([blockCursor.clamp]), and — when the region the cursor
// stands in was the transcript and it lost every stop — the cursor handed to the panel's remembered
// or first stop, the mode ending only where the panel has none either. The panel region needs no
// seating here: the column is re-judged at the next key ([Model.reseatPanelCursor]) and at paint.
func (m Model) seatBlockCursor() blockCursor {
	c := m.cursor.clamp(m.lineTargets, m.pads)
	if c.active || !m.cursor.inTranscript() {
		return c
	}
	stops := m.navPanelStops()
	if len(stops) == 0 {
		return c
	}
	handed := m.cursor
	handed.region = regionPanel
	at, ok := handed.panelStopIndex(stops)
	if !ok {
		at = 0
	}
	return handed.standOnPanel(stops[at])
}

// navPanelStops is the side panel's stops as navigation mode may walk them: the column's stops
// over the rows the frame gives it ([Model.panelStops]), none while the column is not painted. A
// column docked beside the transcript offers them to every way in; one hidden under the window's
// gate offers them only to a cursor already standing in the panel region — which is what paints it
// as the overlay ([Model.panelOverlaid]) — so ⌥↑/⌥↓ and a transcript that loses its stops never
// open the overlay: only → does ([Model.enterPanelRegion], ADR 0111 D7).
func (m Model) navPanelStops() []panelStop {
	if !m.panelShown() && !m.panelOverlaid() {
		return nil
	}
	return m.panelStops(m.frameSpans().slotBottom)
}

// reseatPanelCursor re-judges a panel-standing cursor against the column as it stands now, before a
// key is claimed: the column re-lays out under every Event and a deadline (the sub-agents linger)
// can empty it with no key pressed, so its stop is found again by identity — or by the nearest row —
// and a column with no stops left hands the cursor back to the transcript's place, or ends the mode
// where the transcript has no stop either.
func (m Model) reseatPanelCursor() Model {
	if !m.cursor.inPanel() {
		return m
	}
	stops := m.navPanelStops()
	if at, ok := m.cursor.panelStopIndex(stops); ok {
		m.cursor = m.cursor.standOnPanel(stops[at])
		return m
	}
	if len(stops) > 0 {
		m.cursor = m.cursor.standOnPanel(stops[0])
		return m
	}
	if len(cursorStops(m.lineTargets, m.pads)) == 0 {
		m.cursor = blockCursor{}
		return m
	}
	m.cursor.region = regionTranscript
	m.followBlockCursor()
	return m
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
// ⌥↑ / ⌥↓ both enter and move, so the mode costs no separate gesture to open; they enter in the
// transcript, or in the side panel when the transcript has no stop. Inside the mode plain ↑/↓ move
// within the region the cursor stands in, → and ← move it between the transcript and the panel
// (ADR 0111 D1–3) — and are claimed even where there is nowhere to go, so the prompt's caret never
// moves under a reader who is navigating — and esc leaves the mode from either region. In the
// transcript ⏎ opens or closes the surface under the highlight and `^x` stops the delegation whose
// row it stands on; in the panel ⏎ is the click on the stop it stands on ([Model.activatePanelCursor],
// ADR 0111 D4), and `^x` on a sub-agent row stops that run (ADR 0111 D3) — off one it is not the
// mode's and goes on to the frame.
// Every other key — ⌃c, PgUp, ⇧⇥ — is left alone and leaves the mode standing: they are the frame's
// own verbs, and none of them is a reason to lose your place in the scrollback.
//
// A panel-standing cursor is re-judged against the column first ([Model.reseatPanelCursor]), so a
// key never acts on a stop the column no longer has.
func (m Model) blockCursorKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	if !m.blockCursorOwnsKeys() {
		return m, nil, false
	}
	m = m.reseatPanelCursor()
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
	case "right":
		return m.enterPanelRegion(), nil, true
	case "left":
		return m.enterTranscriptRegion(), nil, true
	case "esc":
		m.cursor = blockCursor{}
		return m, nil, true
	case "enter":
		if m.cursor.inPanel() {
			next, cmd := m.activatePanelCursor()
			return next, cmd, true
		}
		if !m.cursor.inTranscript() {
			return m, nil, false
		}
		next, cmd := m.toggleAtBlockCursor()
		return next, cmd, true
	case "ctrl+x":
		// `^x` on a delegation's row stops that run alone — a member of a ✦ Sub-Agent umbrella, a
		// grandchild's row inside a view (ADR 0086 D5), or a side-panel sub-agent row (ADR 0111 D3) —
		// and leaves the mode standing: the row is still there, and says how the run ended once it
		// has. Anywhere else the key is not the cursor's, and inside a view it goes on to stop
		// nothing (the view's claimant declined it).
		head, ok := m.cursorRunHead()
		if !ok {
			return m, nil, false
		}
		if m.watch.isActive {
			// A watched run's rows stop nothing: the watch is read-only (watch.go).
			cmd := m.showFlash(watchRefusedFlash)
			return m, cmd, true
		}
		m.stopRun(head)
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

// enterPanelRegion is → : the cursor moves into the side panel, onto the stop it remembers there or,
// on the first visit this mode, the stop nearest the transcript row it leaves. On a window under the
// column's gate the move opens it as the overlay (ADR 0111 D7), so the stops are judged as the panel
// region would see them — the overlay's, which are the docked column's laid out over the same rows. A
// panel with no stop to stand on — or a column `off` hides — leaves the cursor where it is.
func (m Model) enterPanelRegion() Model {
	if !m.cursor.inTranscript() {
		return m
	}
	entered := m
	entered.cursor.region = regionPanel
	stops := entered.navPanelStops()
	if len(stops) == 0 {
		return m
	}
	at, ok := m.cursor.panelStopIndex(stops)
	if !ok {
		at, _ = nearestPanelStop(stops, m.blockCursorRow(), anyPanelStop)
	}
	entered.cursor = entered.cursor.standOnPanel(stops[at])
	return entered
}

// activatePanelCursor is ⏎ on the panel stop the cursor stands on (ADR 0111 D4, adr-0111-amend-1109b):
// exactly the click on that stop, through the click's own [Model.followPanelTarget], so the two ways
// in are one gesture — under a modal pane or on a watched pin it acts as the click does there. A
// header's two stops keep the mode, the cursor still on the stop it acted on: the pin stop toggles
// pinned ↔ auto, the title stop folds ↔ opens the section, and the title's identity outlives the
// re-layout the fold makes ([blockCursor.panelStopIndex]). A stop that opens a detail — a Run view,
// a tool block, /usage, /mcp — ends it, as ⏎ on a delegation's transcript row does: the reader's
// attention has moved to what opened. The stop is the one [Model.reseatPanelCursor] resolved for
// this key.
func (m Model) activatePanelCursor() (Model, tea.Cmd) {
	stops := m.navPanelStops()
	at, ok := m.cursor.panelStopIndex(stops)
	if !ok {
		return m, nil
	}
	target := stops[at].target
	isOpening := !stops[at].id.isHeader && m.panelTargetLive(target)
	next, cmd := m.followPanelTarget(target)
	m = next.(Model)
	if isOpening {
		m.cursor = blockCursor{}
	}
	return m, cmd
}

// enterTranscriptRegion is ← : the cursor moves back to the transcript row it remembers, scrolled
// into view as any move there is. A transcript with no stop leaves the cursor in the docked panel;
// in the narrow-window overlay ← closes it whatever the transcript holds (ADR 0111 D7), so there it
// ends the mode instead.
func (m Model) enterTranscriptRegion() Model {
	if !m.cursor.inPanel() {
		return m
	}
	if len(cursorStops(m.lineTargets, m.pads)) == 0 {
		if m.panelOverlaid() {
			m.cursor = blockCursor{}
		}
		return m
	}
	m.cursor.region = regionTranscript
	m.followBlockCursor()
	return m
}

// cursorRunHead is the delegation the highlight stands on, when it stands on one: a cursor standing
// in the transcript whose line the paint marked as a header ([targetHeader]) of an entry that heads a run — a
// delegation's collapsed row at any depth, a ✦ Sub-Agent umbrella's member rows included — or a
// cursor standing on a side-panel sub-agent row, whose stop opens that run (ADR 0111 D3). It reads
// the paint's own accounting (m.lineTargets, the column's stops), as [Model.toggleBlockAt] and
// [Model.activatePanelCursor] do, so the row `^x` stops is the row the human sees highlighted.
func (m Model) cursorRunHead() (entry, bool) {
	if m.cursor.inPanel() {
		return m.panelCursorRunHead()
	}
	if !m.cursor.inTranscript() || m.cursor.line < 0 || m.cursor.line >= len(m.lineTargets) {
		return entry{}, false
	}
	target := m.lineTargets[m.cursor.line]
	if target.kind != targetHeader {
		return entry{}, false
	}
	return m.runHeadAt(target.entry)
}

// panelCursorRunHead is the delegation a panel-standing cursor's stop opens: the stop the cursor
// resolves to ([blockCursor.panelStopIndex]) when it is a sub-agent row. Any other stop — a header,
// a task card, a changed file, the usage or the mcp report — heads no run.
func (m Model) panelCursorRunHead() (entry, bool) {
	stops := m.navPanelStops()
	at, ok := m.cursor.panelStopIndex(stops)
	if !ok || stops[at].target.kind != panelTargetRun {
		return entry{}, false
	}
	return m.runHeadAt(stops[at].target.entry)
}

// runHeadAt is entries[index] when it heads a run.
func (m Model) runHeadAt(index int) (entry, bool) {
	if index < 0 || index >= len(m.transcript.entries) {
		return entry{}, false
	}
	head := m.transcript.entries[index]
	if !head.headsRun() {
		return entry{}, false
	}
	return head, true
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
// Standing in the panel, the move walks the column's stops instead ([Model.movePanelCursor]), by the
// same clamped rule.
//
// A live drag-selection is dropped on the way in. The transcript has one highlight and one meaning
// for it — this is the thing you are pointing at — and the two would otherwise paint at once, which
// is the same region arbitration the prompt's own selection is cleared for on every keypress
// (handleKey).
func (m Model) moveBlockCursor(dir int) Model {
	if !m.cursor.active {
		return m.enterNavigationMode(dir)
	}
	if m.cursor.region == regionPanel {
		return m.movePanelCursor(dir)
	}
	stops := cursorStops(m.lineTargets, m.pads)
	if len(stops) == 0 {
		m.cursor = blockCursor{}
		return m
	}
	at, _ := slices.BinarySearch(stops, m.cursor.line)
	m.cursor.line = stops[stepIndex(at, dir, len(stops))]
	m.followBlockCursor()
	return m
}

// enterNavigationMode turns the mode on from the prompt, at the end of the transcript's stops that
// dir points away from — or, where the transcript has no stop, at that end of the side panel's
// (ADR 0111 D3). With no stop in either region the mode stays off.
func (m Model) enterNavigationMode(dir int) Model {
	if stops := cursorStops(m.lineTargets, m.pads); len(stops) > 0 {
		m.transcriptSel = transcriptSel{}
		m.cursor = blockCursor{active: true, line: stops[entryIndex(dir, len(stops))]}
		m.followBlockCursor()
		return m
	}
	stops := m.navPanelStops()
	if len(stops) == 0 {
		m.cursor = blockCursor{}
		return m
	}
	m.transcriptSel = transcriptSel{}
	m.cursor = blockCursor{active: true, region: regionPanel}.standOnPanel(stops[entryIndex(dir, len(stops))])
	return m
}

// movePanelCursor moves a panel-standing cursor one stop in dir down the column, clamped at either
// end; a column with no stops leaves it where it is (the next key re-judges it).
func (m Model) movePanelCursor(dir int) Model {
	stops := m.navPanelStops()
	at, ok := m.cursor.panelStopIndex(stops)
	if !ok {
		return m
	}
	m.cursor = m.cursor.standOnPanel(stops[stepIndex(at, dir, len(stops))])
	return m
}

// entryIndex is the stop a region is entered at from the prompt below it: the last for dir < 0 (⌥↑),
// the first otherwise (⌥↓).
func entryIndex(dir, count int) int {
	if dir > 0 {
		return 0
	}
	return count - 1
}

// stepIndex is at moved dir stops, clamped to a list of count — never wrapping.
func stepIndex(at, dir, count int) int {
	return min(max(at+dir, 0), count-1)
}

// blockCursorRow is the screen row the cursor's line is DRAWN on, or −1 when the transcript is not
// showing it. It reads [Model.drawnLineAt] — the mapping the mouse and the selection highlight both
// go through — rather than subtracting the scroll offset itself, so the sticky header's frozen rows
// are answered for correctly: those rows draw the owning prompt's lines and not the offset's, and a
// cursor "at" one of them is a cursor the human cannot see. A cursor standing in the side panel is
// drawn on no transcript row: its transcript place is only remembered.
func (m Model) blockCursorRow() int {
	if !m.cursor.inTranscript() {
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
	if !m.cursor.inTranscript() || m.blockCursorRow() >= 0 {
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
// "Opens" is not always in place. A framed delegation opens as its RUN VIEW (runview.go, ADR 0063),
// which is the same redirect a click gets and for the same reason: the rule lives where the click
// map is read, so ⏎ on a delegation and a click on one are one gesture with two ways in. A workflow's
// stage row is a stop like any other surface (targetStage) and opens the same way: its item's run
// view, or the stage view listing its items ([Model.openStageAt]).
//
// The anchor row is where the cursor is DRAWN, so ⏎ holds the highlighted row exactly where the eye
// already is. A cursor the view is not showing cannot be toggled — followBlockCursor keeps that
// unreachable after any move, and refusing beats guessing a row for a line nobody can see — as is a
// place the cursor only remembers while it stands in the side panel ([Model.blockCursorRow]).
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
// The bar is two marks ([markCursorRow]): a glyphCursorMark ▌ over column 0 — the marker cell — and
// the theme's SELECTION field across the row, the one tone the frame already spends on "this is the
// thing you are pointing at" (design call 7). The ▌ is what a terminal painting no colour still
// shows (ADR 0111 D6); the row keeps its width, so nothing re-wraps. It can never be confused with a drag-selection,
// because the two never coexist: entering the mode drops the selection, and a mouse press ends the
// paint of this one by moving the reader's attention back to the pointer. Only the region the cursor
// stands in is painted: while it is in the side panel the transcript's remembered row is not
// ([Model.panelCursorRow] paints that one).
func (m Model) highlightBlockCursor(view string) string {
	if !m.cursor.inTranscript() || !m.blockCursorOwnsKeys() {
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
	lines[row] = markCursorRow(m.th.measure, lines[row], m.th.selection)
	return strings.Join(lines, "\n")
}

// markCursorRow paints line as the row navigation mode's cursor stands on: glyphCursorMark over its
// first cell and the rest of it re-rendered under style, so the row reads as the cursor's by shape
// as well as by tone. It is the one paint both regions' cursors wear — a transcript row from its
// marker column ([Model.highlightBlockCursor]), a side-panel row from its gutter cell
// ([Model.sidePanelColumn]). The mark replaces a cell rather than adding one, so the row keeps the
// width it was painted to. Like [shadeCells] it slices through the width authority, the measure the
// row was painted in.
func markCursorRow(measure widthAuthority, line string, style lipgloss.Style) string {
	rest := measure.Cut(line, 1, measure.Width(line))
	return style.Render(glyphCursorMark + ansi.Strip(rest))
}

// markPanelCursorRow paints line — a column row past its separator: the gutter cell, then the
// content — as the panel cursor standing on the stop id wears it. A body stop's row is marked whole
// ([markCursorRow]). A header row carries two stops (adr-0111-amend-1109b), so the mark tells them
// apart by where the selection tone falls: the pin stop shades the lead pin cell alone, the title
// stop the rest of the row past it, the pin cell left as painted — the ▌ over the gutter cell either
// way, so a terminal painting no colour still shows the row.
func markPanelCursorRow(measure widthAuthority, line string, id panelStopID, style lipgloss.Style) string {
	if !id.isHeader {
		return markCursorRow(measure, line, style)
	}
	width := measure.Width(line)
	pin := measure.Width(sidePanelGutter) // the pin glyph's cell: the content's first
	c0, c1 := pin, pin+1
	if !id.isPin {
		c0, c1 = pin+1, width
	}
	rest := measure.Cut(line, 1, width)
	return style.Render(glyphCursorMark) + shadeCells(measure, rest, c0-1, c1-1, style)
}

// panelCursorRow is the column row the panel-standing cursor is painted on — its stop's first row
// among the stops laid out over rows rows — and that stop's identity, which says how the row is
// marked ([markPanelCursorRow]); the row is −1 where nothing is: the cursor stands in the
// transcript, a decision holds the keys ([Model.blockCursorOwnsKeys], which takes the highlight with
// them), or the column has no stop.
func (m Model) panelCursorRow(rows int) (int, panelStopID) {
	if !m.cursor.inPanel() || !m.blockCursorOwnsKeys() {
		return -1, panelStopID{}
	}
	stops := m.panelStops(rows)
	at, ok := m.cursor.panelStopIndex(stops)
	if !ok {
		return -1, panelStopID{}
	}
	return stops[at].y, stops[at].id
}
