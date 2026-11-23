package tui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// Navigation mode's walk (ADR 0111 D1–3) is one rule over two regions: a region is a list of stops
// the cursor stands on, and the keys — ⌥↑/⌥↓ to enter, ↑/↓ to move, → and ← between the regions, ⏎
// to act, `^x` to stop a run — mean the same thing in either. What differs is only what a stop IS
// and what acting on it does, and that is all a region adapter answers ([navRegion]): the
// transcript's stops are the paint's own click map ([transcriptRegion]), the side panel's are the
// column's stops ([panelRegion]). Exactly two adapters implement it, one per region; ADR 0111 D12
// declines a third.
//
// A key resolves the cursor once ([Model.navigation]): a panel-standing cursor is re-judged against
// the column as it stands now, and the column's frame is built at most once for the key, from one
// row bound ([Model.panelFrameAs]) — and only when the key reaches the panel at all, so a walk in the
// transcript composes no frame. The result is a plain value built on a per-key copy of the Model and
// never held on it (ADR 0011).

// navRegion is one of navigation mode's two regions as the walk sees it: how many stops it offers,
// which one the cursor's place there resolves to, how the cursor comes to stand on one, and what ⏎
// and `^x` do on the one it stands on. The cursor's place in each region lives on [blockCursor], so
// a region the cursor is not standing in still remembers it.
type navRegion interface {
	// stopCount is how many stops the region offers this key.
	stopCount() int
	// place is the index of the stop the cursor's place in this region resolves to — or, the cursor
	// having none here yet, the stop nearest screen row row (−1 for the first). It is false only
	// when the region has no stop.
	place(cursor blockCursor, row int) (int, bool)
	// stand is m with its cursor standing in this region on the stop at, brought into view.
	stand(m Model, at int) Model
	// activate is ⏎ on the stop the cursor stands on in this region.
	activate(m Model) (Model, tea.Cmd)
	// runHead is the delegation the stop the cursor stands on in this region opens, when it opens one.
	runHead(m Model) (entry, bool)
	// isOverlay reports whether the region is painted only while the cursor stands in it — the side
	// panel's narrow-window overlay — so that only → may open it (ADR 0111 D7).
	isOverlay() bool
}

// navigation is navigation mode resolved for one key ([Model.navigation]): the Model with its cursor
// re-judged, and the two regions it walks. The panel region is nil until a way into the panel needs
// it ([navigation.withPanel]), so a key that never reaches the column builds no frame.
type navigation struct {
	m          Model
	transcript navRegion
	panel      navRegion
}

// navigation resolves navigation mode for one key: the transcript's stops read off the paint, and a
// panel-standing cursor re-judged against the column as it stands now — the column re-lays out
// under every Event and a deadline (the sub-agents linger) can empty it with no key pressed, so its
// stop is found again by identity, or by the nearest row; a column with no stops left hands the
// cursor back to the transcript's place, or ends the mode where the transcript has no stop either.
// It is pure: the re-judged cursor is on the returned value's Model, never written back to m.
func (m Model) navigation() navigation {
	return navigation{m: m, transcript: transcriptRegion{stops: cursorStops(m.lineTargets, m.pads)}}.reseat()
}

// withPanel is n with its panel region built, when it is not already: the column's frame at the
// geometry the panel region is painted at — the overlay asked for where the keys are the mode's own,
// as [Model.panelGeometry] asks it — over the rows of the frame m composes. A column that is not
// painted is a region with no stops.
func (n navigation) withPanel() navigation {
	if n.panel == nil {
		n.panel = newPanelRegion(n.m.panelFrameAs(n.m.blockCursorOwnsKeys()))
	}
	return n
}

// reseat is n with a panel-standing cursor re-judged against the column ([Model.navigation]).
func (n navigation) reseat() navigation {
	if !n.m.cursor.inPanel() {
		return n
	}
	n = n.withPanel()
	if n.panel.stopCount() > 0 {
		n.m = n.cross(n.panel, -1)
		return n
	}
	if n.transcript.stopCount() == 0 {
		n.m.cursor = blockCursor{}
		return n
	}
	n.m = n.cross(n.transcript, -1)
	return n
}

// standingOn is the region the cursor stands in, and false while the mode is off.
func (n navigation) standingOn() (cursorRegion, bool) {
	return n.m.cursor.region, n.m.cursor.active
}

// current is the adapter of the region the cursor stands in — the transcript while the mode is off,
// whose adapter answers nothing for an inactive cursor.
func (n navigation) current() navRegion {
	if n.m.cursor.inPanel() {
		return n.withPanel().panel
	}
	return n.transcript
}

// runHead is the delegation the cursor stands on, when it stands on one: a transcript row the paint
// marked as a run's header, or a side-panel sub-agent row (ADR 0111 D3).
func (n navigation) runHead() (entry, bool) {
	return n.current().runHead(n.m)
}

// navEntryDir is the direction ⌥↑ (−1) and ⌥↓ (+1) enter and move the walk in, and false for every
// other key: the two keys that are the mode's whether or not it is on.
func navEntryDir(msg tea.KeyPressMsg) (int, bool) {
	switch msg.String() {
	case "alt+up":
		return -1, true
	case "alt+down":
		return 1, true
	}
	return 0, false
}

// key is the mode's key contract over the resolved cursor ([Model.blockCursorKey] documents it). It
// returns the next Model whether or not it claimed the key, because a printable key ENDS the mode
// and is then left to fall through to the prompt.
func (n navigation) key(msg tea.KeyPressMsg) (Model, tea.Cmd, bool) {
	if dir, isEntry := navEntryDir(msg); isEntry {
		return n.move(dir), nil, true
	}
	m := n.m
	if !m.cursor.active {
		return m, nil, false
	}
	switch msg.String() {
	case "up":
		return n.move(-1), nil, true
	case "down":
		return n.move(1), nil, true
	case "right":
		return n.right(), nil, true
	case "left":
		return n.left(), nil, true
	case "esc":
		m.cursor = blockCursor{}
		return m, nil, true
	case "enter":
		next, cmd := n.current().activate(m)
		return next, cmd, true
	case "ctrl+x":
		// `^x` on a delegation's row stops that run alone — a member of a ✦ Sub-agents umbrella, a
		// grandchild's row inside a view (ADR 0086 D5), or a side-panel sub-agent row (ADR 0111 D3) —
		// and leaves the mode standing: the row is still there, and says how the run ended once it
		// has. Anywhere else the key is not the cursor's, and inside a view it goes on to stop
		// nothing (the view's claimant declined it).
		head, ok := n.runHead()
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
	}
	return m, nil, false
}

// move moves the cursor one stop in dir within the region it stands in, clamped at either end rather
// than wrapping — a walk that fell off the top and reappeared at the bottom would lose the reader's
// place in a scrollback that is often screens long — or, the mode off, enters it ([navigation.enter]).
// A region left with no stop for the cursor ends the mode.
func (n navigation) move(dir int) Model {
	if !n.m.cursor.active {
		return n.enter(dir)
	}
	region := n.current()
	at, ok := region.place(n.m.cursor, -1)
	if !ok {
		m := n.m
		m.cursor = blockCursor{}
		return m
	}
	return region.stand(n.m, stepIndex(at, dir, region.stopCount()))
}

// enter turns the mode on from the prompt, at the end of the transcript's stops that dir points away
// from: ⌥↑ at the LAST stop, the most recent block and the one a reader looking away from the prompt
// wants first, and ⌥↓ at the first, the oldest thing the scrollback still holds. Where the transcript
// has no stop it enters at that end of the side panel's (ADR 0111 D3) — the docked column's only,
// since only → opens the overlay (ADR 0111 D7). With no stop in either the mode stays off.
//
// A live drag-selection is dropped on the way in. The transcript has one highlight and one meaning
// for it — this is the thing you are pointing at — and the two would otherwise paint at once, which
// is the same region arbitration the prompt's own selection is cleared for on every keypress
// (handleKey).
func (n navigation) enter(dir int) Model {
	region := n.transcript
	if region.stopCount() == 0 {
		n = n.withPanel()
		region = n.panel
		if region.isOverlay() || region.stopCount() == 0 {
			m := n.m
			m.cursor = blockCursor{}
			return m
		}
	}
	m := n.m
	m.transcriptSel = transcriptSel{}
	m.cursor = blockCursor{active: true}
	return region.stand(m, entryIndex(dir, region.stopCount()))
}

// right is → : the cursor moves into the side panel, onto the stop it remembers there or, on the
// first visit this mode, the stop nearest the transcript row it leaves. On a window under the
// column's gate the move opens it as the overlay (ADR 0111 D7), so the stops are the overlay's — the
// docked column's laid out over the same rows. A panel with no stop to stand on — a column `off`
// hides, or an overlay while a decision owns the keys, which [navigation.withPanel] never asks for —
// leaves the cursor where it is.
func (n navigation) right() Model {
	if !n.m.cursor.inTranscript() {
		return n.m
	}
	n = n.withPanel()
	if n.panel.stopCount() == 0 {
		return n.m
	}
	return n.cross(n.panel, n.m.blockCursorRow())
}

// left is ← : the cursor moves back to the transcript row it remembers, scrolled into view as any
// move there is. A transcript with no stop leaves the cursor in the docked panel; in the narrow-window
// overlay ← closes it whatever the transcript holds (ADR 0111 D7), so there it ends the mode instead.
func (n navigation) left() Model {
	if !n.m.cursor.inPanel() {
		return n.m
	}
	n = n.withPanel()
	if n.transcript.stopCount() == 0 {
		m := n.m
		if n.panel.isOverlay() {
			m.cursor = blockCursor{}
		}
		return m
	}
	return n.cross(n.transcript, -1)
}

// cross is the Model with its cursor standing in region to, on the place it remembers there or —
// none yet — the stop nearest screen row row. The caller has checked that to has a stop.
func (n navigation) cross(to navRegion, row int) Model {
	at, _ := to.place(n.m.cursor, row)
	return to.stand(n.m, at)
}

// transcriptRegion is the transcript as navigation mode walks it: the paint's own click map
// collapsed to one stop per surface ([cursorStops]), the cursor's place a content line.
type transcriptRegion struct {
	stops []int
}

func (r transcriptRegion) stopCount() int { return len(r.stops) }

// place is the stop the cursor's line is on — or, the line being no stop, the stop before it, the
// rule [blockCursor.clamp] seats it by. Every transcript place resolves while the region has a stop,
// so row is never read.
func (r transcriptRegion) place(cursor blockCursor, _ int) (int, bool) {
	if len(r.stops) == 0 {
		return 0, false
	}
	at, isExact := slices.BinarySearch(r.stops, cursor.line)
	if isExact {
		return at, true
	}
	return max(at-1, 0), true
}

// stand puts the cursor on the transcript line of stop at and scrolls it into view
// ([Model.followBlockCursor]).
func (r transcriptRegion) stand(m Model, at int) Model {
	m.cursor.region = regionTranscript
	m.cursor.line = r.stops[at]
	m.followBlockCursor()
	return m
}

// activate opens or closes the surface under the highlight, through the mouse's own toggle
// ([Model.toggleAtBlockCursor]).
func (transcriptRegion) activate(m Model) (Model, tea.Cmd) {
	return m.toggleAtBlockCursor()
}

// runHead is the delegation the highlighted transcript row heads: a line the paint marked as a
// header ([targetHeader]) of an entry that heads a run — a delegation's collapsed row at any depth,
// a ✦ Sub-agents umbrella's member rows included. It reads the paint's own accounting
// (m.lineTargets), as [Model.toggleBlockAt] does, so the row `^x` stops is the row the human sees
// highlighted.
func (transcriptRegion) runHead(m Model) (entry, bool) {
	if !m.cursor.inTranscript() || m.cursor.line < 0 || m.cursor.line >= len(m.lineTargets) {
		return entry{}, false
	}
	target := m.lineTargets[m.cursor.line]
	if target.kind != targetHeader {
		return entry{}, false
	}
	return m.runHeadAt(target.entry)
}

func (transcriptRegion) isOverlay() bool { return false }

// panelRegion is the side panel's column as navigation mode walks it: the frame's stops
// ([panelFrame.stops]), the cursor's place a stop identity ([panelStopID]).
type panelRegion struct {
	stops      []panelStop
	isOverlaid bool
}

// newPanelRegion is the panel region over frame: its stops, and whether it is painted as the
// narrow-window overlay.
func newPanelRegion(frame panelFrame) panelRegion {
	return panelRegion{stops: frame.stops, isOverlaid: frame.geometry.isOverlaid}
}

func (r panelRegion) stopCount() int { return len(r.stops) }

// place is the stop the cursor's panel place resolves to ([blockCursor.panelStopIndex]) or, the
// cursor having no panel place yet, the stop nearest row ([nearestPanelStop]).
func (r panelRegion) place(cursor blockCursor, row int) (int, bool) {
	if at, ok := cursor.panelStopIndex(r.stops); ok {
		return at, true
	}
	return nearestPanelStop(r.stops, row, anyPanelStop)
}

// stand puts the cursor on panel stop at, remembering its identity and row.
func (r panelRegion) stand(m Model, at int) Model {
	m.cursor.region = regionPanel
	m.cursor = m.cursor.standOnPanel(r.stops[at])
	return m
}

// activate is ⏎ on the panel stop the cursor stands on (ADR 0111 D4, adr-0111-amend-1109b): exactly
// the click on that stop, through the click's own [Model.followPanelTarget], so the two ways in are
// one gesture — under a modal pane or on a watched pin it acts as the click does there. A header's
// two stops keep the mode, the cursor still on the stop it acted on: the pin stop toggles pinned ↔
// auto, the title stop folds ↔ opens the section, and the title's identity outlives the re-layout the
// fold makes ([blockCursor.panelStopIndex]). A stop that opens a detail — a Run view, a tool block,
// /usage, /mcp — ends it, as ⏎ on a delegation's transcript row does: the reader's attention has
// moved to what opened.
func (r panelRegion) activate(m Model) (Model, tea.Cmd) {
	at, ok := m.cursor.panelStopIndex(r.stops)
	if !ok {
		return m, nil
	}
	target := r.stops[at].target
	isOpening := !r.stops[at].id.isHeader && m.panelTargetLive(target)
	next, cmd := m.followPanelTarget(target)
	m = next.(Model)
	if isOpening {
		m.cursor = blockCursor{}
	}
	return m, cmd
}

// runHead is the delegation the panel stop the cursor stands on opens, when it is a sub-agent row
// (ADR 0111 D3). Any other stop — a header, a task card, a changed file, the usage or the mcp report
// — heads no run.
func (r panelRegion) runHead(m Model) (entry, bool) {
	if !m.cursor.inPanel() {
		return entry{}, false
	}
	at, ok := m.cursor.panelStopIndex(r.stops)
	if !ok || r.stops[at].target.kind != panelTargetRun {
		return entry{}, false
	}
	return m.runHeadAt(r.stops[at].target.entry)
}

func (r panelRegion) isOverlay() bool { return r.isOverlaid }
