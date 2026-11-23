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
	active bool         // the mode is on: ↑/↓/←/→/esc are the mode's until a printable key hands them back
	region cursorRegion // which region the cursor stands in; the other one's place is only remembered
	line   int          // the index into Model.lines / Model.lineTargets the transcript place is on

	panel    panelStopID // the panel stop the panel place is on, while hasPanel
	panelY   int         // the row that stop was last laid out on: the fallback when its identity is gone
	hasPanel bool        // the panel has a place: the region was entered at least once this mode
}

// cursorRegion is which of navigation mode's two regions the cursor stands in; the walk over each is
// its region adapter ([navRegion], navmode.go).
type cursorRegion int

const (
	regionTranscript cursorRegion = iota // the transcript's click surfaces (the zero value: the mode began there)
	regionPanel                          // the side panel's stops ([Model.panelStops])
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
// stands in was the transcript and it lost every stop — the cursor handed to the panel region's
// remembered or first stop ([panelRegion]), the mode ending only where the panel has none either.
// The panel region needs no seating here: the column is re-judged at the next key
// ([Model.navigation]) and at paint.
func (m Model) seatBlockCursor() blockCursor {
	c := m.cursor.clamp(m.lineTargets, m.pads)
	if c.active || !m.cursor.inTranscript() {
		return c
	}
	panel := panelRegion{stops: m.navPanelStops()}
	at, ok := panel.place(m.cursor, -1)
	if !ok {
		return c
	}
	return panel.stand(m, at).cursor
}

// navPanelStops is the side panel's stops as navigation mode may walk them: the column's stops
// over the rows the frame gives it ([Model.panelStops]), none while the column is not painted. A
// column docked beside the transcript offers them to every way in; one hidden under the window's
// gate offers them only to a cursor already standing in the panel region — which is what paints it
// as the overlay ([Model.panelOverlaid]) — so ⌥↑/⌥↓ and a transcript that loses its stops never
// open the overlay: only → does ([navigation.right], ADR 0111 D7).
func (m Model) navPanelStops() []panelStop {
	if !m.panelShown() && !m.panelOverlaid() {
		return nil
	}
	return m.panelStops(m.frameSpans().slotBottom)
}

// reseatPanelCursor is m with a panel-standing cursor re-judged against the column as it stands now,
// as a key resolves it ([Model.navigation]).
func (m Model) reseatPanelCursor() Model {
	return m.navigation().m
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
// row it stands on; in the panel ⏎ is the click on the stop it stands on ([panelRegion.activate],
// ADR 0111 D4), and `^x` on a sub-agent row stops that run (ADR 0111 D3) — off one it is not the
// mode's and goes on to the frame.
// Every other key — ⌃c, PgUp, ⇧⇥ — is left alone and leaves the mode standing: they are the frame's
// own verbs, and none of them is a reason to lose your place in the scrollback.
//
// The key resolves the cursor once ([Model.navigation]): a panel-standing cursor is re-judged
// against the column first, so a key never acts on a stop the column no longer has, and the walk over
// either region is its adapter's ([navRegion], navmode.go). A mode that is off resolves nothing for
// a key that does not open it, so the keys typed into the prompt pay for no walk.
func (m Model) blockCursorKey(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	if !m.blockCursorOwnsKeys() {
		return m, nil, false
	}
	if _, isEntry := navEntryDir(msg); !isEntry && !m.cursor.active {
		return m, nil, false
	}
	return m.navigation().key(msg)
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
// marker column ([Model.highlightBlockCursor]), a side-panel row from its gutter's first cell
// ([Model.sidePanelColumn]). The mark replaces a cell rather than adding one, so the row keeps the
// width it was painted to. Like [shadeCells] it slices through the width authority, the measure the
// row was painted in.
func markCursorRow(measure widthAuthority, line string, style lipgloss.Style) string {
	rest := measure.Cut(line, 1, measure.Width(line))
	return style.Render(glyphCursorMark + ansi.Strip(rest))
}

// markPanelCursorRow paints line — a column row past its separator: the gutter cells, then
// the content — as the panel cursor standing on the stop id wears it. A body stop's row is marked
// whole ([markCursorRow]). A header row carries two stops (adr-0111-amend-1109b), so the mark tells
// them apart by where the selection tone falls: the pin stop shades the lead pin cell alone, the
// title stop the rest of the row past it, the pin cell left as painted — the ▌ over the gutter's
// first cell either way, so a terminal painting no colour still shows the row.
func markPanelCursorRow(measure widthAuthority, line string, gutter int, id panelStopID, style lipgloss.Style) string {
	if !id.isHeader {
		return markCursorRow(measure, line, style)
	}
	width := measure.Width(line)
	pin := gutter // the pin glyph's cell: the content's first
	c0, c1 := pin, pin+1
	if !id.isPin {
		c0, c1 = pin+1, width
	}
	rest := measure.Cut(line, 1, width)
	return style.Render(glyphCursorMark) + shadeCells(measure, rest, c0-1, c1-1, style)
}

// panelCursorRow is the column row the panel-standing cursor is painted on over rows rows, as the
// frame at this geometry lays them out ([Model.panelCursorRowIn]).
func (m Model) panelCursorRow(rows int) (int, panelStopID) {
	return m.panelCursorRowIn(m.newPanelFrame(m.panelGeometry(), rows))
}

// panelCursorRowIn is the column row the panel-standing cursor is painted on — its stop's first row
// among frame's stops — and that stop's identity, which says how the row is marked
// ([markPanelCursorRow]); the row is −1 where nothing is: the cursor stands in the transcript, a
// decision holds the keys ([Model.blockCursorOwnsKeys], which takes the highlight with them), or the
// column has no stop.
func (m Model) panelCursorRowIn(frame panelFrame) (int, panelStopID) {
	if !m.cursor.inPanel() || !m.blockCursorOwnsKeys() {
		return -1, panelStopID{}
	}
	at, ok := m.cursor.panelStopIndex(frame.stops)
	if !ok {
		return -1, panelStopID{}
	}
	return frame.stops[at].y, frame.stops[at].id
}
