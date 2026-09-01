package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
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

// TestRunViewOwnHeadDoesNotReopenItself pins the one head the redirect must refuse: the view's own.
// A rooted paint spends its root's head on the breadcrumb and on the task row beneath it, and marks
// that row for the head (render.go) — so a run handed a task too tall to fit carries, inside itself,
// a click surface naming itself. Activating it must leave the stack alone: a second level of the run
// already on screen is a view esc has to be pressed twice to leave. The row's kind is what settles
// it — targetTask folds the task and asks the redirect nothing — and the guard inside the redirect
// stands behind that, for any reach that asks it about the run it is already showing.
func TestRunViewOwnHeadDoesNotReopenItself(t *testing.T) {
	// Long enough to fold at 80 columns, so the task row is a click surface at all (promptCollapsedRows).
	const tallTask = "survey the repository from top to bottom and write down every package it holds, " +
		"what each one is for, which of them the TUI reaches into, and which of them reach back — " +
		"then say which of those edges look like the ones a newcomer would trip over first, and why"

	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("survey the repo", nil)
	subAgentCall(&m.transcript, "s1", tallTask, 0)
	readCall(&m.transcript, "r1", "a.go", 1, 5, 1)
	subAgentReport(&m.transcript, "s1", "all clear", 0)
	m.refreshViewport()

	m = enterOnLastBlock(t, m)
	if got := m.viewedRun().spawn; got != "s1" {
		t.Fatalf("setup: ⏎ on the delegation opened run %q; want the run it heads", got)
	}

	task := markedLine(t, m, targetTask)
	if got := m.lineTargets[task].entry; got != 1 {
		t.Fatalf("setup: the first foldable row inside the view is marked for entry %d; want the head at 1, whose task it paints", got)
	}

	m = clickCell(t, m, 2, screenRow(t, m, task))

	if n := len(m.viewStack); n != 1 {
		t.Fatalf("a click on the view's own task row left %d levels open; want the one view that was already there", n)
	}
	if got := m.viewedRun().spawn; got != "s1" {
		t.Errorf("the click re-rooted the paint at %q; it stands on the run it was already showing", got)
	}

	m = step(t, m, keyEsc())

	if m.inRunView() {
		t.Errorf("one esc left %d level(s) open; the click stacked a duplicate of the run on itself", len(m.viewStack))
	}
}

// seeMoreCount reads the number a collapsed prompt's see-more marker advertises off the row carrying
// it — the claim the fold makes to the reader, taken from the marker's own wording (promptSeeMore)
// rather than from a literal spelled twice.
func seeMoreCount(t *testing.T, row string) int {
	t.Helper()
	open := strings.SplitN(promptSeeMoreFormat, "%s", 2)[0]
	i := strings.Index(row, open)
	if i < 0 {
		t.Fatalf("row %q carries no see-more marker, so it advertises nothing to open", row)
	}
	count := row[i+len(open):]
	if j := strings.IndexByte(count, ' '); j >= 0 {
		count = count[:j]
	}
	n, err := strconv.Atoi(count)
	if err != nil {
		t.Fatalf("the see-more marker in %q counts %q, which is not a number", row, count)
	}
	return n
}

// TestRunViewTaskFoldOpensWhatItAdvertises is the marker's promise kept. A rooted paint folds the
// task its run was handed by the ordinary prompt rule (render.go), and that rule paints a see-more
// marker counting the rows it holds back — so activating that row must show exactly those rows.
// While the fold borrowed the HEAD's block state it could not: setExpanded refuses that flag on a
// framed run, which is what keeps a delegation to the two shapes ADR 0063 gives it, and the view was
// left advertising a click nothing would answer. The fold has a state of its own now
// (entry.taskExpanded), and the flag it must NOT have started writing is pinned here beside it —
// that one is the inline rail this plan deleted.
func TestRunViewTaskFoldOpensWhatItAdvertises(t *testing.T) {
	// Tall enough at 80 columns to hide several rows behind the marker, and ending in a word that
	// appears nowhere else, so "did the hidden rows come out?" is one lookup (promptCollapsedRows).
	const lastWord = "loosestrife"
	const tallTask = "survey the repository from top to bottom and write down every package it holds, " +
		"what each one is for, which of them the TUI reaches into, and which of them reach back — " +
		"then say which of those edges look like the ones a newcomer would trip over first, and why; " +
		"finish by naming the three files you would read first tomorrow morning, in order, and give " +
		"each of them one sentence saying what it would tell you, ending on the word " + lastWord

	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("survey the repo", nil)
	subAgentCall(&m.transcript, "s1", tallTask, 0)
	readCall(&m.transcript, "r1", "a.go", 1, 5, 1)
	subAgentReport(&m.transcript, "s1", "all clear", 0)
	m.refreshViewport()

	m = enterOnLastBlock(t, m)
	if got := m.viewedRun().spawn; got != "s1" {
		t.Fatalf("setup: ⏎ on the delegation opened run %q; want the run it heads", got)
	}
	painted := func() string { return strip(strings.Join(m.lines, "\n")) }

	folded := markedRows(t, m, targetTask)
	if len(folded) != promptCollapsedRows {
		t.Fatalf("the folded task paints %d marked row(s), want the collapsed cap of %d", len(folded), promptCollapsedRows)
	}
	marker := folded[len(folded)-1]
	hidden := seeMoreCount(t, strip(m.lines[marker]))
	if hidden < 1 {
		t.Fatalf("setup: the folded task advertises %d hidden row(s); the fixture's task is not tall enough", hidden)
	}
	if strings.Contains(painted(), lastWord) {
		t.Fatalf("setup: the folded task already shows %q, so the marker is hiding nothing", lastWord)
	}

	m = clickCell(t, m, 2, screenRow(t, m, marker))

	opened := markedRows(t, m, targetTask)
	if want := promptCollapsedRows + hidden + 1; len(opened) != want {
		t.Errorf("the marker advertised %d more row(s) and opening it painted %d marked row(s); want %d — the %d it already showed, the %d it counted, and the see-less row closing them",
			hidden, len(opened), want, promptCollapsedRows, hidden)
	}
	if !strings.Contains(painted(), lastWord) {
		t.Errorf("the opened task still hides %q, which its own marker counted:\n%s", lastWord, painted())
	}
	if last := strip(m.lines[opened[len(opened)-1]]); !strings.Contains(last, promptSeeLess) {
		t.Errorf("the opened task ends on %q, want the %q row that folds it again", last, promptSeeLess)
	}

	// The fold is the view's own, and the rail ADR 0063 deleted stays deleted: the head's block
	// state is untouched, and it still refuses the flag that used to open that rail in place.
	if strings.Contains(painted(), subAgentOpenMarker) {
		t.Errorf("opening the task drew %q — the inline sub-agent rail, back from the dead:\n%s", subAgentOpenMarker, painted())
	}
	if m.transcript.entries[1].expanded {
		t.Error("opening the task wrote the head's expanded flag; that flag is the deleted inline rail's, not the view's")
	}
	if m.transcript.setExpanded(1, true) {
		t.Error("setExpanded(1, true) = true: the framed head takes the rail's flag again, and a replayed record could reopen the shape ADR 0063 removed")
	}

	m = clickCell(t, m, 2, screenRow(t, m, markedRows(t, m, targetTask)[0]))

	if again := markedRows(t, m, targetTask); len(again) != promptCollapsedRows {
		t.Errorf("the reopened task folds back to %d marked row(s), want the collapsed cap of %d", len(again), promptCollapsedRows)
	}
	if strings.Contains(painted(), lastWord) {
		t.Errorf("the task folded back but still shows %q:\n%s", lastWord, painted())
	}
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

// ----------------------------------------------------------------------------
// The prompt box inside a view — what it invites, and what ⏎ does (ADR 0063)
// ----------------------------------------------------------------------------

// viewOn folds one delegation named "repo-scout" (spawn id "s1") onto m's scrollback in the given
// lifecycle and opens its run view through the same reach a click takes. It is the frame every test
// below types into: the box on screen belongs to that child.
func viewOn(t *testing.T, m Model, phase childPhase) Model {
	t.Helper()
	subAgentCall(&m.transcript, "s1", "repo-scout", 0)
	if phase != childScheduled {
		subAgentStarted(&m.transcript, "s1", 1)
		readCall(&m.transcript, "r1", "a.go", 1, 5, 1)
	}
	if phase == childOver {
		subAgentReport(&m.transcript, "s1", "all clear", 0)
	}
	m.refreshViewport()
	m = enterOnLastBlock(t, m)
	if !m.inRunView() {
		t.Fatal("setup: ⏎ on the delegation opened no run view, so the box addresses nobody")
	}
	return m
}

// modelViewingChild is a model left in exactly the state a human steers a delegate from: a real
// submit has opened an Exchange (stateRunning, a live mailbox), a delegation is on the scrollback
// in the given lifecycle, and its view is open. Recall is wired, so what the box does with a sent
// line is observable here too.
func modelViewingChild(t *testing.T, eng *fakeEngine, phase childPhase) Model {
	t.Helper()
	m := newTestModelEng(t, eng, recallOpts(&fakeRecallHost{}))
	m.input.SetValue("survey the repo")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("setup: state = %v, want running", m.state)
	}
	return viewOn(t, m, phase)
}

// noteInTranscript reports whether the scrollback carries exactly this note.
func noteInTranscript(m Model, text string) bool {
	for _, e := range m.transcript.entries {
		if e.kind == entryNote && e.text == text {
			return true
		}
	}
	return false
}

// TestRunViewPlaceholderNamesTheChild pins the box's third invitation: inside a view it addresses
// the run on screen, so it names it — and it names esc as the way back, the meaning the view's
// claimant gave that key. Only a running child is invited to; the other two lifecycles say why they
// are not, because a legend may only advertise a key that does something.
func TestRunViewPlaceholderNamesTheChild(t *testing.T) {
	for _, tc := range []struct {
		name  string
		phase childPhase
		want  string
	}{
		{name: "running", phase: childRunning, want: "Message repo-scout…  ⏎ send · ↑ recall · esc back"},
		{name: "finished", phase: childOver, want: "repo-scout has finished · esc back"},
		{name: "scheduled", phase: childScheduled, want: "repo-scout has not started · esc back"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := modelViewingChild(t, &fakeEngine{}, tc.phase)

			if got := m.input.Placeholder; got != tc.want {
				t.Errorf("placeholder = %q; want %q", got, tc.want)
			}
		})
	}

	t.Run("backing out gives the conversation its own legend back", func(t *testing.T) {
		m := modelViewingChild(t, &fakeEngine{}, childRunning)

		m = step(t, m, keyEsc())

		if m.inRunView() {
			t.Fatal("setup: esc did not leave the view")
		}
		if got := m.input.Placeholder; got != runningPlaceholder {
			t.Errorf("placeholder = %q; want the running legend back at the top level", got)
		}
	})

	t.Run("the viewed child finishing re-words the box in place", func(t *testing.T) {
		m := modelViewingChild(t, &fakeEngine{}, childRunning)

		m = step(t, m, eventMsg{Event: domain.SubAgentPhaseEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
			Phase:     domain.SubAgentFinished,
		}})

		want := "repo-scout has finished · esc back"
		if got := m.input.Placeholder; got != want {
			t.Errorf("placeholder = %q; want %q — the invitation must not outlive the run it names", got, want)
		}
	})

	t.Run("a completing Exchange below does not re-label a box addressing a child", func(t *testing.T) {
		m := modelViewingChild(t, &fakeEngine{}, childRunning)

		m = step(t, m, exchangeDoneMsg{})

		if got := m.input.Placeholder; got == idlePlaceholder || got == idleShiftPlaceholder {
			t.Errorf("placeholder = %q; the conversation's own legend took a box that addresses a child", got)
		}
	})
}

// TestRunViewEnterRefusesANonRunningChild pins the two lifecycles with no mailbox behind them: ⏎ is
// a no-op that says so and leaves the draft standing, so the same line can be carried back up. The
// note lands at depth 0 as it always has AND the same sentence flashes on the status line, because
// a reader inside the view would otherwise see nothing at all until they backed out.
func TestRunViewEnterRefusesANonRunningChild(t *testing.T) {
	for _, phase := range []childPhase{childOver, childScheduled} {
		eng := &fakeEngine{}
		m := modelViewingChild(t, eng, phase)

		m.input.SetValue("check the tests too")
		m, cmd := stepCmd(t, m, keyEnter())

		if got := eng.childInterjections(); len(got) != 0 {
			t.Errorf("phase %v: InterjectChild calls = %+v; want none — there is no child to read one", phase, got)
		}
		if got := m.input.Value(); got != "check the tests too" {
			t.Errorf("phase %v: input = %q; a refused message must stay in the box", phase, got)
		}
		if n := len(m.pendingInterjections); n != 0 {
			t.Errorf("phase %v: staged rows = %d; want none", phase, n)
		}
		want := "repo-scout is not running — go back to send a message"
		if !noteInTranscript(m, want) {
			t.Errorf("phase %v: the refusal note %q is not in the scrollback", phase, want)
		}
		if m.flash != want {
			t.Errorf("phase %v: flash = %q; want the refusal sentence %q on the status line", phase, m.flash, want)
		}
		if !clearsTheFlash(t, cmd) {
			t.Errorf("phase %v: the refusal returned no command that clears the flash", phase)
		}
	}
}

// clearsTheFlash reports whether cmd — or any command batched inside it — eventually yields the
// flashClearMsg that takes the sentence back off the status line. The tick is real, so the message
// is drained on a goroutine and waited for rather than run inline.
func clearsTheFlash(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	if cmd == nil {
		return false
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch msg := msg.(type) {
		case flashClearMsg:
			return true
		case tea.BatchMsg:
			for _, c := range msg {
				if clearsTheFlash(t, c) {
					return true
				}
			}
		}
		return false
	case <-time.After(5 * time.Second):
		t.Fatal("the flash-clearing command never produced a message")
		return false
	}
}

// TestRefuseChildMessageFlashesOnlyInsideAView is the guard on the flash's one condition: at depth 0
// the note is already on screen, so the slot stays with the context gauge and nothing is flashed.
func TestRefuseChildMessageFlashesOnlyInsideAView(t *testing.T) {
	for _, note := range []string{
		childNotRunningNote("repo-scout"),
		childGoneNote("repo-scout"),
	} {
		m := newTestModel(t)
		if m.inRunView() {
			t.Fatal("setup: the model is not at depth 0")
		}

		next, cmd := m.refuseChildMessage(note)
		got := next.(Model)

		if got.flash != "" {
			t.Errorf("note %q: flash = %q; at depth 0 the gauge keeps the slot", note, got.flash)
		}
		if cmd != nil {
			t.Errorf("note %q: a depth-0 refusal returned a command; want none", note)
		}
		if !noteInTranscript(got, note) {
			t.Errorf("note %q: the depth-0 note is not in the scrollback", note)
		}
	}
}

// TestRunViewDecisionPanesKeepEnter is the regression guard: a child's ask and its approval render
// through the very pane the view is painted under, so ⏎ there must still answer the question in
// front of the human rather than message the run behind it.
func TestRunViewDecisionPanesKeepEnter(t *testing.T) {
	t.Run("an ask is answered, not sent to the child", func(t *testing.T) {
		eng := &fakeEngine{}
		m := modelViewingChild(t, eng, childRunning)
		reply := make(chan domain.AskAnswer, 1)

		m = step(t, m, askReqMsg{Request: domain.AskRequest{Question: "which file?"}, Reply: reply})
		for _, r := range "42" {
			m = step(t, m, keyRune(r))
		}
		m = step(t, m, keyEnter())

		select {
		case got := <-reply:
			if got.Text != "42" {
				t.Fatalf("answer = %q; want the typed text", got.Text)
			}
		default:
			t.Fatal("⏎ under the ask pane did not answer it")
		}
		if got := eng.childInterjections(); len(got) != 0 {
			t.Errorf("InterjectChild calls = %+v; an answer is not a message to the run", got)
		}
	})

	t.Run("an approval is decided, not sent to the child", func(t *testing.T) {
		eng := &fakeEngine{}
		m := modelViewingChild(t, eng, childRunning)
		reply := make(chan domain.ApprovalDecision, 1)

		m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "terminal"}, Reply: reply})
		m = step(t, m, approvalArmedMsg{seq: m.approvalSeq})
		m = step(t, m, keyEnter())

		select {
		case <-reply:
		default:
			t.Fatal("⏎ under the approval pane did not take the highlighted row")
		}
		if got := eng.childInterjections(); len(got) != 0 {
			t.Errorf("InterjectChild calls = %+v; a decision is not a message to the run", got)
		}
	})
}

// TestRunViewDecisionPaneOwnsTheLegend is the other half of that guard, on the row the human READS:
// a pane borrows the box below it, so while an ask or an approval stands the box must not go on
// inviting a message to the child — the child legend names esc as the way back, and esc under a
// pane cancels the question instead (runViewOwnsEsc steps aside for both states). The view is still
// open behind the pane, so the child's own invitation comes back the moment the question is away.
func TestRunViewDecisionPaneOwnsTheLegend(t *testing.T) {
	const childLegendText = "Message repo-scout…  ⏎ send · ↑ recall · esc back"

	t.Run("an ask hands the box the answering legend", func(t *testing.T) {
		m := modelViewingChild(t, &fakeEngine{}, childRunning)
		if got := m.input.Placeholder; got != childLegendText {
			t.Fatalf("setup: placeholder = %q; want the child legend", got)
		}
		reply := make(chan domain.AskAnswer, 1)

		m = step(t, m, askReqMsg{Request: domain.AskRequest{Question: "which file?"}, Reply: reply})

		if got := m.input.Placeholder; got != m.idleLegend() {
			t.Errorf("placeholder = %q; want the answering legend %q — the box belongs to the question", got, m.idleLegend())
		}
		if strings.Contains(m.input.Placeholder, "esc back") {
			t.Errorf("placeholder = %q; esc cancels the question here, so the box must not advertise it as the way back", m.input.Placeholder)
		}

		// An event arriving under the open pane must not put the child's invitation back either.
		m = step(t, m, eventMsg{Event: domain.SubAgentPhaseEvent{
			EventBase: domain.EventBase{Depth: 1, CallID: "s1"},
			Phase:     domain.SubAgentFinished,
		}})
		if got := m.input.Placeholder; got != m.idleLegend() {
			t.Errorf("placeholder = %q after an event under the pane; want the answering legend to stand", got)
		}

		for _, r := range "42" {
			m = step(t, m, keyRune(r))
		}
		m = step(t, m, keyEnter())

		if got := m.input.Placeholder; got != "repo-scout has finished · esc back" {
			t.Errorf("placeholder = %q; the answered question gives the box back to the run on screen", got)
		}
	})

	t.Run("an approval keeps the conversation's own legend", func(t *testing.T) {
		m := modelViewingChild(t, &fakeEngine{}, childRunning)
		reply := make(chan domain.ApprovalDecision, 1)

		m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "terminal"}, Reply: reply})

		if got := m.input.Placeholder; got != runningPlaceholder {
			t.Errorf("placeholder = %q; want %q — the pane's Cancel row owns esc, not the view", got, runningPlaceholder)
		}
		if strings.Contains(m.input.Placeholder, "esc back") {
			t.Errorf("placeholder = %q; esc cancels the approval here, so the box must not advertise it as the way back", m.input.Placeholder)
		}

		m = step(t, m, approvalArmedMsg{seq: m.approvalSeq})
		m = step(t, m, keyEnter())

		if got := m.input.Placeholder; got != childLegendText {
			t.Errorf("placeholder = %q; want the child legend back once the decision is away", got)
		}
	})

	// A pane does not always end in an answer: a stop or a fault takes the whole Exchange, question
	// and all, and the box it borrowed goes back to the run STILL OPEN behind it rather than to the
	// conversation below. finishWorker resolves the legend after its own state flip for exactly
	// this — while the state still named the pane, legendFor would yield and the top level's
	// invitation would land on a box that is addressing a child.
	for _, tc := range []struct {
		name string
		open tea.Msg
	}{
		{name: "an ask", open: askReqMsg{Request: domain.AskRequest{Question: "which file?"}, Reply: make(chan domain.AskAnswer, 1)}},
		{name: "an approval", open: approvalReqMsg{Request: domain.ApprovalRequest{Tool: "terminal"}, Reply: make(chan domain.ApprovalDecision, 1)}},
	} {
		t.Run("a stop under "+tc.name+" hands the box back to the view", func(t *testing.T) {
			m := modelViewingChild(t, &fakeEngine{}, childRunning)
			m = step(t, m, tc.open)

			m = step(t, m, cancelledMsg{})

			if got := m.input.Placeholder; got != childLegendText {
				t.Errorf("placeholder = %q; want the child legend %q — the view outlived the question", got, childLegendText)
			}
		})
	}

	t.Run("a fault under a pane hands it back too", func(t *testing.T) {
		m := modelViewingChild(t, &fakeEngine{}, childRunning)
		m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "terminal"}, Reply: make(chan domain.ApprovalDecision, 1)})

		m = step(t, m, errMsg{Err: errors.New("upstream fell over")})

		// Errored is not a borrowed box: esc still walks one level up there (runViewOwnsEsc), so
		// the legend goes on naming it.
		if got := m.input.Placeholder; got != childLegendText {
			t.Errorf("placeholder = %q; want the child legend %q — esc still leaves the view at errored", got, childLegendText)
		}
	})
}

// TestRunViewBreadcrumbHintFollowsTheKey is the same guard on the row ABOVE the transcript. The
// header and the status line's right slot advertise ONE key (statusRight, "the two rows advertise
// one key"), so the breadcrumb may not go on promising `esc back` while a child's ask or approval
// pane holds esc for its own answer. The trail itself stays either way — it says where the reader
// is, which is true under a pane — and the hint comes back the moment the question is away.
func TestRunViewBreadcrumbHintFollowsTheKey(t *testing.T) {
	// header is the row the sticky overlay freezes at the top of the view (render.go), plainly.
	header := func(t *testing.T, m Model) string {
		t.Helper()
		m.refreshViewport()
		if len(m.lines) == 0 {
			t.Fatal("the view painted no lines at all")
		}
		return strip(m.lines[0])
	}

	for _, tc := range []struct {
		name   string
		open   tea.Msg
		answer []tea.Msg
	}{
		{
			name:   "an ask",
			open:   askReqMsg{Request: domain.AskRequest{Question: "which file?"}, Reply: make(chan domain.AskAnswer, 1)},
			answer: []tea.Msg{keyRune('4'), keyEnter()},
		},
		{
			name: "an approval",
			open: approvalReqMsg{Request: domain.ApprovalRequest{Tool: "terminal"}, Reply: make(chan domain.ApprovalDecision, 1)},
		},
	} {
		t.Run(tc.name+" takes the hint off the header", func(t *testing.T) {
			m := modelViewingChild(t, &fakeEngine{}, childRunning)
			before := header(t, m)
			if !strings.Contains(before, breadcrumbHint) {
				t.Fatalf("setup: the header is %q; want it to advertise %q", before, breadcrumbHint)
			}
			trail := strings.TrimSpace(before[:strings.Index(before, breadcrumbHint)])

			m = step(t, m, tc.open)

			under := header(t, m)
			if strings.Contains(under, breadcrumbHint) {
				t.Errorf("the header is %q under %s; esc answers the pane here, so it must not advertise the way back", under, tc.name)
			}
			if !strings.Contains(under, trail) {
				t.Errorf("the header is %q under %s; want it to keep the trail %q — where the reader is is still true", under, tc.name, trail)
			}
			if got := plainSlot(m.statusRight()); strings.Contains(got, breadcrumbHint) {
				t.Errorf("the status slot is %q under %s; the two rows advertise one key and must fall silent together", got, tc.name)
			}

			// The answer, and the header wears the key again — the view stood behind the pane.
			if tc.answer == nil {
				m = step(t, m, approvalArmedMsg{seq: m.approvalSeq})
				m = step(t, m, keyEnter())
			}
			for _, msg := range tc.answer {
				m = step(t, m, msg)
			}
			if after := header(t, m); !strings.Contains(after, breadcrumbHint) {
				t.Errorf("the header is %q once the question is away; want %q back", after, breadcrumbHint)
			}
		})
	}
}
