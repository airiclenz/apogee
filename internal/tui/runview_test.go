package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The run view's own suite: what opens one, what leaves one, and what the frame says while one is
// open (ADR 0063). The paint itself is render_test.go's — everything here is about the WALK.

// modelWithRun is a ready idle model whose scrollback holds one finished delegation with a nested
// read behind it: the framed run a click or a ⏎ opens as a view. The prompt is entries[0] and the
// delegation heads at entries[1].
func modelWithRun(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("survey the repo", nil)
	subAgentCall(&m.transcript, "s1", "survey", 0)
	readCall(&m.transcript, "r1", "a.go", 1, 5, 1)
	subAgentReport(&m.transcript, "s1", "all clear", 0)
	m.refreshViewport()
	return m
}

// enterOnLastBlock is the keyboard route in: ⌥↑ enters the block cursor on the LAST stop and ⏎ acts
// on what it stands on (blockcursor.go), which is the same reach a click takes.
func enterOnLastBlock(t *testing.T, m Model) Model {
	t.Helper()
	return step(t, step(t, m, keyAltUp()), keyEnter())
}

func TestRunViewOpensOnExpand(t *testing.T) {
	t.Run("a framed run opens as a view, at its tail, with the block cursor gone", func(t *testing.T) {
		m := modelWithRun(t)
		m = step(t, m, keyAltUp())
		if !m.cursor.active {
			t.Fatal("setup: ⌥↑ did not enter the block cursor, so ⏎ acts on nothing")
		}

		m = step(t, m, keyEnter())

		if got := m.viewedRun().spawn; got != "s1" {
			t.Fatalf("⏎ on the delegation opened run %q; want the run it heads", got)
		}
		if m.transcript.root.spawn != "s1" {
			t.Errorf("the transcript is rooted at %q; the view's paint is its whole mechanism", m.transcript.root.spawn)
		}
		if m.cursor.active {
			t.Error("the view opened with the block cursor still on: its highlight stood on a line that is gone")
		}
		if m.detached {
			t.Error("the view opened detached; it must follow the run's latest line")
		}
		if !m.viewport.AtBottom() {
			t.Error("the view opened away from the run's tail")
		}
		if body := strings.Join(m.lines, "\n"); !strings.Contains(body, "a.go") {
			t.Errorf("the view does not show the run's own entries:\n%s", body)
		}
	})

	t.Run("a running head with no entries behind it opens too", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset()
		m.transcript.addUser("survey the repo", nil)
		subAgentCall(&m.transcript, "s1", "survey", 0)
		subAgentStarted(&m.transcript, "s1", 1)
		m.refreshViewport()

		m = enterOnLastBlock(t, m)

		if got := m.viewedRun().spawn; got != "s1" {
			t.Fatalf("⏎ on a working delegation opened run %q; a child is addressable before its first entry lands", got)
		}
	})

	t.Run("a delegation that ran and left nothing keeps the inline toggle", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset()
		m.transcript.addUser("survey the repo", nil)
		subAgentCall(&m.transcript, "s1", "survey", 0)
		subAgentReport(&m.transcript, "s1", "refused at the depth bound", 0)
		m.refreshViewport()

		m = enterOnLastBlock(t, m)

		if m.inRunView() {
			t.Error("a delegation with no run behind it opened a view; a view of nothing is a blank screen")
		}
		if !m.transcript.entries[1].expanded {
			t.Error("the unframed delegation did not take today's inline toggle instead")
		}
	})

	t.Run("the sub-agent umbrella keeps its own click", func(t *testing.T) {
		m := modelWithSubAgentGroup(t)
		header := memberRows(t, m, 1)[0] - 1 // the umbrella sits directly above the first member row
		if got := strip(m.lines[header]); !strings.Contains(got, "Sub-Agent (3)") {
			t.Fatalf("setup: line %d is %q, not the umbrella header", header, got)
		}

		m = clickCell(t, m, 4, screenRow(t, m, header))

		if m.inRunView() {
			t.Error("a click on the umbrella opened a run view; the umbrella lists members, it is not one")
		}
		memberRows(t, m, 3) // the list is still there to click a member of
	})
}

func TestRunViewEscGoesOneLevelUp(t *testing.T) {
	t.Run("esc returns to the level below, at the offset and follow it was left at", func(t *testing.T) {
		// A scrollback taller than the viewport with the delegation part-way UP it, so "where the
		// human left it" is a real offset rather than a short transcript's only one — and so the
		// click that opens the view is a click that moved nothing.
		m := newTestModel(t)
		m.transcript.reset()
		for range 20 {
			m.transcript.addUser("survey the repo", nil)
		}
		subAgentCall(&m.transcript, "s1", "survey", 0)
		readCall(&m.transcript, "r1", "a.go", 1, 5, 1)
		subAgentReport(&m.transcript, "s1", "all clear", 0)
		for range 20 {
			m.transcript.addUser("and again", nil)
		}
		m.refreshViewport()
		header := markedLine(t, m, targetHeader)
		m.viewport.SetYOffset(header - 2)
		m.detached = true
		wantOffset := m.viewport.YOffset()
		if wantOffset == 0 || m.viewport.AtBottom() {
			t.Fatalf("setup: the conversation is parked at offset %d, at its bottom", wantOffset)
		}

		m = clickCell(t, m, 2, screenRow(t, m, header))
		if !m.inRunView() {
			t.Fatal("setup: the delegation did not open as a view")
		}

		m = step(t, m, keyEsc())

		if m.inRunView() {
			t.Fatal("esc did not leave the view")
		}
		if m.transcript.root != (runRef{}) {
			t.Errorf("the transcript is still rooted at %q after esc", m.transcript.root.spawn)
		}
		if !m.detached {
			t.Error("esc re-armed following the tail; the level below was scrolled away from it")
		}
		if got := m.viewport.YOffset(); got != wantOffset {
			t.Errorf("esc parked the conversation at offset %d; want %d, where it was left", got, wantOffset)
		}
		if m.cursor.active {
			t.Error("esc left the block cursor standing on a line of the view it closed")
		}
	})

	t.Run("a view taller than the level below still hands that level's offset back", func(t *testing.T) {
		// A run whose own paint is LONGER than the conversation holding it: the offset still
		// standing when the repaint lands is the view's, past the level below's last line, so the
		// repaint clamps it to the bottom and clears the follow flag on the way (refreshViewport).
		// The row the reader parked on has to survive that.
		m := newTestModel(t)
		m.transcript.reset()
		for range 12 {
			m.transcript.addUser("survey the repo", nil)
		}
		subAgentCall(&m.transcript, "s1", "survey", 0)
		for i := range 60 {
			readCall(&m.transcript, "r"+strconv.Itoa(i), "a.go", 1, 5, 1)
		}
		subAgentReport(&m.transcript, "s1", "all clear", 0)
		for range 12 {
			m.transcript.addUser("and again", nil)
		}
		m.refreshViewport()
		below := len(m.lines)
		header := markedLine(t, m, targetHeader)
		m.viewport.SetYOffset(header - 2)
		m.detached = true
		wantOffset := m.viewport.YOffset()
		if wantOffset == 0 || m.viewport.AtBottom() {
			t.Fatalf("setup: the conversation is parked at offset %d, at its bottom", wantOffset)
		}

		m = clickCell(t, m, 2, screenRow(t, m, header))
		if !m.inRunView() {
			t.Fatal("setup: the delegation did not open as a view")
		}
		if len(m.lines) <= below {
			t.Fatalf("setup: the view paints %d lines over a level of %d; this case needs the TALLER one", len(m.lines), below)
		}

		m = step(t, m, keyEsc())

		if got := m.viewport.YOffset(); got != wantOffset {
			t.Errorf("esc parked the conversation at offset %d; want %d, where it was left", got, wantOffset)
		}
		if !m.detached {
			t.Error("esc re-armed following the tail; the level below was scrolled away from it")
		}
	})

	t.Run("a nested run opens a second level and two esc unwind it", func(t *testing.T) {
		m := newTestModel(t)
		m.transcript.reset()
		m.transcript.addUser("survey the repo", nil)
		subAgentCall(&m.transcript, "s1", "survey", 0)
		subAgentCall(&m.transcript, "s2", "read the tests", 1)
		readCall(&m.transcript, "r1", "a.go", 1, 5, 2)
		subAgentReport(&m.transcript, "s2", "all clear", 1)
		subAgentReport(&m.transcript, "s1", "all clear", 0)
		m.refreshViewport()

		m = enterOnLastBlock(t, m)
		if got := m.viewedRun().spawn; got != "s1" {
			t.Fatalf("the outer delegation opened run %q", got)
		}

		m = enterOnLastBlock(t, m)
		if got := m.viewedRun().spawn; got != "s2" {
			t.Fatalf("the nested delegation opened run %q from inside its parent's view", got)
		}
		if len(m.viewStack) != 2 {
			t.Fatalf("the stack holds %d levels; a run inside a run is two", len(m.viewStack))
		}

		if m = step(t, m, keyEsc()); m.viewedRun().spawn != "s1" {
			t.Fatalf("one esc landed on run %q; it goes ONE level up", m.viewedRun().spawn)
		}
		if m = step(t, m, keyEsc()); m.inRunView() {
			t.Fatalf("the second esc left the view stack at %q", m.viewedRun().spawn)
		}
	})

	t.Run("esc inside a view never arms the stop", func(t *testing.T) {
		m := modelWithRun(t)
		cancelled := false
		m.cancel = func() { cancelled = true }
		m.state = stateRunning
		m = enterOnLastBlock(t, m)

		m = step(t, m, keyEsc())

		if !m.lastEsc.IsZero() {
			t.Error("esc inside a view armed the stop gesture; the claimant swallows it")
		}
		if cancelled {
			t.Error("esc inside a view cancelled the worker")
		}
		if m.inRunView() {
			t.Error("esc inside a view did not go up a level")
		}
	})

	t.Run("a pane waiting for an answer keeps its own esc", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			state uiState
		}{
			{"an ask question", stateAwaitingAsk},
			{"an approval request", stateAwaitingApproval},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := modelWithRun(t)
				m.cancel = func() {}
				m = enterOnLastBlock(t, m)
				m.state = tc.state

				m = step(t, m, keyEsc())

				if !m.inRunView() {
					t.Error("esc under a live pane left the run view; the pane's own cancel owns that key")
				}
			})
		}
	})
}

func TestRunViewStatusSlotOffersTheWayBack(t *testing.T) {
	m := modelWithRun(t)
	m.state = stateRunning
	if got := plainSlot(m.statusRight()); got != "esc×2 stop" {
		t.Fatalf("setup: the top level's right slot is %q; want the stop gesture", got)
	}

	m = enterOnLastBlock(t, m)

	if got := plainSlot(m.statusRight()); got != breadcrumbHint {
		t.Errorf("the right slot inside a view is %q; want %q — the key it actually has", got, breadcrumbHint)
	}
}

// plainSlot is a status-line slot with its styling taken back off, for a claim about the exact words
// it spends its columns on.
func plainSlot(slot string) string {
	return strings.TrimSpace(plain(tea.NewView(slot)))
}

func TestRunViewStackFollowsTheEntriesItNames(t *testing.T) {
	t.Run("a reset that drops the run closes its view", func(t *testing.T) {
		m := modelWithRun(t)
		m = enterOnLastBlock(t, m)

		m.transcript.reset()
		m.reseatViewStack()

		if m.inRunView() {
			t.Error("a view outlived the entries it painted")
		}
		if m.transcript.root != (runRef{}) {
			t.Errorf("the paint is still rooted at %q with nothing to root at", m.transcript.root.spawn)
		}
	})

	t.Run("a replay of the same run keeps it", func(t *testing.T) {
		m := modelWithRun(t)
		m = enterOnLastBlock(t, m)

		m.transcript.reset()
		m.transcript.addUser("survey the repo", nil)
		subAgentCall(&m.transcript, "s1", "survey", 0)
		readCall(&m.transcript, "r1", "a.go", 1, 5, 1)
		subAgentReport(&m.transcript, "s1", "all clear", 0)
		m.reseatViewStack()

		if got := m.viewedRun().spawn; got != "s1" {
			t.Errorf("the restored session left the view at %q; the same run reopens on the same view", got)
		}
		if m.transcript.root.spawn != "s1" {
			t.Errorf("the paint is rooted at %q; want the view that survived", m.transcript.root.spawn)
		}
	})
}
