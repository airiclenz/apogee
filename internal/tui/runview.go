package tui

import (
	tea "charm.land/bubbletea/v2"
)

// The run view: the stack of open views, the two moves between levels, and the one key that walks
// back up (ADR 0063).
//
// A delegation has two shapes and no third: the collapsed row it wears in the conversation, and
// THIS — the whole transcript slot given over to that one run, painted rooted at it
// ([transcript.setRoot], render.go) under the breadcrumb naming the way back. Expanding a framed
// run opens the view instead of flipping a rail open in place ([Model.openRunAt] is the redirect
// both reaches funnel through, mouse.go), and esc or a click on the breadcrumb leaves it.
//
// The stack is what makes "one level up" mean something inside a nested delegation: each level
// remembers where the level BELOW it was parked, so backing out of a child lands the reader on the
// row they opened it from rather than at the tail of a conversation they had scrolled up in.

// runView is one open level of that stack: the run it is rooted at, and the scroll position of the
// level BELOW it — the offset and follow-the-tail flag the transcript had when this view opened,
// handed back when it closes.
//
// Three plain fields, riding the value-copied Model (ADR 0011).
type runView struct {
	ref      runRef // the run this level paints
	yOffset  int    // where the level below was scrolled to when this one opened
	detached bool   // …and whether it was following the tail there
}

// viewedRun is the run the human is LOOKING at: the delegation whose view is open, or the zero
// runRef for the top-level conversation. It is the status line's subject (statusLeft) as much as
// the paint's root, so the row speaks for the run on screen rather than for the session.
func (m Model) viewedRun() runRef {
	if len(m.viewStack) == 0 {
		return runRef{}
	}
	return m.viewStack[len(m.viewStack)-1].ref
}

// inRunView reports whether any run view is open.
func (m Model) inRunView() bool { return len(m.viewStack) > 0 }

// openRunAt opens the run view for the delegation headed by entries[index], reporting whether that
// entry was a run to open at all. It is the REDIRECT: every reach that used to flip a delegation's
// rail open asks here first ([Model.toggleBlockAt], and the block cursor's ⏎ through it), so the
// keyboard and the mouse cannot come to disagree about what expanding a delegation means.
//
// The predicate is the framing rule evaluated as if the head were already open: a run with entries
// behind it, or one that has not reported yet. The second half is what lets a child be opened
// BEFORE its first entry lands — a delegation announces itself a beat before it says anything, and
// a reader who clicked it then must not get an inline rail that the view would replace a moment
// later. A delegation that is over and left nothing behind it (refused at the depth bound, faulted
// before its first event) is not a run: it keeps the ordinary block's inline toggle, as does the
// ✦ Sub-Agent umbrella, whose click is its own kind (targetUmbrella).
func (m Model) openRunAt(index int) (Model, bool) {
	if index < 0 || index >= len(m.transcript.entries) {
		return m, false
	}
	head := m.transcript.entries[index]
	if !head.headsRun() {
		return m, false
	}
	if subAgentSpan(m.transcript.entries, index) == 0 && head.done {
		return m, false
	}
	return m.openRun(runRef{depth: head.depth + 1, spawn: head.callID}), true
}

// openRun pushes ref onto the view stack and repaints the transcript rooted at it.
//
// The view opens CLEAN and at the bottom: the block cursor is dropped, because the highlight was
// standing on a line of the conversation that is no longer on screen, and following is re-armed so
// the view lands on the run's latest output — a reader who opened a working child is asking what it
// is doing now (ADR 0063, D5). What the level below was looking at is not lost: it rides the stack
// entry and comes back at [Model.upRun].
//
// The run's own depth is read off its head rather than off the caller — the two agree by
// construction ([transcript.closeRun] builds its ref from exactly this) — so the ref the paint is
// keyed by is the ref the entries carry.
func (m Model) openRun(ref runRef) Model {
	m.viewStack = append(m.viewStack, runView{ref: ref, yOffset: m.viewport.YOffset(), detached: m.detached})
	m.cursor = blockCursor{}
	m.transcript.setRoot(ref)
	m.detached = false
	m.refreshViewport()
	return m
}

// upRun leaves the topmost view and repaints one level down — the meaning of esc inside a view
// (runViewClaimant) and of a click on the breadcrumb header (targetBreadcrumb). At the top level it
// does nothing at all, which is what lets both reaches ask unconditionally.
//
// The level below is restored to where it was, by the rule the scroll flag already states
// everywhere else: a reader who had scrolled AWAY from the tail gets their offset back, and the
// invariant "detached ⇔ off the bottom" is re-derived from where that landed
// (refreshViewportAnchored takes the same care). A reader who was FOLLOWING gets the tail — which
// is where the conversation has grown to while they were away, not the row the tail stood on when
// they left.
//
// Which of the two it is, is read off the STACK ENTRY rather than off the flag the repaint just
// ran under: the offset still standing when the repaint lands is the VIEW's, and a view taller
// than the level below leaves it past that level's bottom, where refreshViewport clamps it and
// clears the flag — taking the reader's parked offset with it if the restore asked the field.
//
// The block cursor is dropped on the way out for the reason it is dropped on the way in: the
// highlight was standing on a line of the view, and the view is gone.
func (m Model) upRun() Model {
	if len(m.viewStack) == 0 {
		return m
	}
	left := m.viewStack[len(m.viewStack)-1]
	m.viewStack = m.viewStack[:len(m.viewStack)-1]
	m.cursor = blockCursor{}
	m.transcript.setRoot(m.viewedRun())
	m.detached = left.detached
	m.refreshViewport()
	if left.detached {
		m.viewport.SetYOffset(left.yOffset)
		m.detached = !m.viewport.AtBottom()
	}
	return m
}

// reseatViewStack drops every open view whose run the transcript no longer holds, and re-roots the
// paint at whatever level survives. It is the ONE rule the stack lives by across a session
// boundary: a view is a way of looking at entries, so it stands exactly as long as the entries it
// names do.
//
// That makes /clear and /new close every view (their reset empties the list) while /continue and a
// restored session KEEP one whose spawn id the replayed scrollback brings back — the same run, the
// same view, no rule of its own. It is called after the transcript has been re-filled, never
// between the reset and the replay, since a stack judged against an empty list would pop a view the
// replay was about to restore.
func (m *Model) reseatViewStack() {
	for len(m.viewStack) > 0 {
		if _, ok := runHeadAt(m.transcript.entries, m.viewedRun().spawn); ok {
			break
		}
		m.viewStack = m.viewStack[:len(m.viewStack)-1]
	}
	m.transcript.setRoot(m.viewedRun())
}

// runViewOwnsEsc reports whether esc means "one level up" in this frame — the claimant's own gate,
// and the status line's ([Model.statusRight]), so the key the row advertises is the key the walk
// will actually hand over.
//
// A view must be open, and no pane may be waiting for an answer: the ask box advertises `esc cancel`
// and the approval pane a `Cancel  esc` row, so esc there keeps the meaning those panes promise
// (ask.go, approval.go) and the way out of the view is to answer the question first.
func (m Model) runViewOwnsEsc() bool {
	if !m.inRunView() {
		return false
	}
	return m.state != stateAwaitingAsk && m.state != stateAwaitingApproval
}

// runViewKey is the view's whole key contract: esc goes one level up, and every other key falls
// through untouched — to the block cursor below it in the claim order, and to the prompt box below
// that. Claiming esc is also what keeps the stop gesture out of a view: m.lastEsc never arms while
// the claimant is open, so esc×2 cannot stop the run from inside it (ADR 0063 — back out first).
func (m Model) runViewKey(msg tea.KeyPressMsg) (bool, tea.Model, tea.Cmd) {
	if msg.String() != "esc" {
		return false, m, nil
	}
	return true, m.upRun(), nil
}
