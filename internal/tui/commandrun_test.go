package tui

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The queued-command drain (ADR 0025 D7 and D10, amended 2026-09-14)
// ----------------------------------------------------------------------------

// queuedRun opens an Exchange on a scripted engine and stages each line in order — messages and
// commands alike, exactly as the human would type them mid-run.
func queuedRun(t *testing.T, lines ...string) (Model, *fakeEngine) {
	t.Helper()
	eng := &fakeEngine{stepFn: scriptedSteps()}
	m := newTestModelEng(t, eng, testOpts)
	m.input.SetValue("open the exchange")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("precondition: state = %v, want running", m.state)
	}
	for _, line := range lines {
		m = stageRow(t, m, line)
	}
	return m, eng
}

// A /clear queued behind a queued message runs FIRST at the completion: the session is reset
// before the message is flushed, so the message opens the next Exchange in the fresh session rather
// than being cleared away with the old one.
func TestQueuedClearRunsBeforeTheQueuedMessage(t *testing.T) {
	t.Parallel()
	m, eng := queuedRun(t, "a message", "/clear")

	next, cmd := stepCmd(t, m, exchangeDoneMsg{})

	if eng.clearCalls != 1 {
		t.Fatalf("ClearContext calls = %d, want 1 — the queued /clear ran at idle", eng.clearCalls)
	}
	if n := len(next.deferredCommands); n != 0 {
		t.Errorf("queued commands = %d; want the queue drained", n)
	}
	if next.state != stateRunning {
		t.Fatalf("state = %v; want running — the message opened the next Exchange after the clear", next.state)
	}
	if n := len(next.pendingInterjections); n != 0 {
		t.Errorf("staged messages = %d; want the message flushed", n)
	}
	if e := lastEntry(t, next); e.kind != entryUser || e.text != "a message" {
		t.Errorf("tail entry = %+v; want the flushed message as a user block in the fresh transcript", e)
	}
	drainCmd(t, next, cmd)
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != "a message" {
		t.Errorf("submitted = %+v; want the message sent once, after the clear", eng.submitted)
	}
}

// The drain stops at the first verb that opens a worker: a /compact queued ahead of a /clear runs
// alone at the completion, and the /clear waits for the compaction's own fold — never two workers
// on one Agent, and still FIFO.
func TestQueuedDrainStopsAtCompactAndResumesAfterItsFold(t *testing.T) {
	t.Parallel()
	m, eng := queuedRun(t, "/compact", "/clear")

	next, cmd := stepCmd(t, m, exchangeDoneMsg{})

	if next.state != stateRunning || next.worker.box != nil {
		t.Fatalf("state = %v, box = %v; want the compaction worker running with no mailbox", next.state, next.worker.box)
	}
	if got := commandLines(next); !reflect.DeepEqual(got, []string{"/clear"}) {
		t.Fatalf("queued commands after the compaction started = %v; want /clear still waiting", got)
	}
	if eng.clearCalls != 0 {
		t.Fatalf("ClearContext calls = %d, want 0 while the compaction runs", eng.clearCalls)
	}
	drainCmd(t, next, cmd)
	if eng.compactCalls != 1 {
		t.Errorf("Compact calls = %d, want 1", eng.compactCalls)
	}

	after, _ := stepCmd(t, next, compactDoneMsg{})

	if after.state != stateIdle {
		t.Fatalf("state = %v; want idle once the compaction landed and the /clear ran", after.state)
	}
	if eng.clearCalls != 1 {
		t.Errorf("ClearContext calls = %d, want 1 — the /clear ran after foldCompactDone", eng.clearCalls)
	}
	if n := len(after.deferredCommands); n != 0 {
		t.Errorf("queued commands = %d; want the queue drained", n)
	}
}

// Commands queued before a loop error wait through the errored state and drain at the ⏎ that
// dismisses it — after the error is cleared, before any held message is sent, which stays the
// NEXT ⏎'s.
func TestCommandsQueuedBeforeALoopErrorDrainAtTheDismissal(t *testing.T) {
	t.Parallel()
	m, eng := queuedRun(t, "still worth sending", "/clear")

	m, _ = stepCmd(t, m, errMsg{Err: errors.New("upstream fell over")})

	if m.state != stateErrored {
		t.Fatalf("state = %v; want errored", m.state)
	}
	if got := commandLines(m); !reflect.DeepEqual(got, []string{"/clear"}) {
		t.Fatalf("queued commands after the fault = %v; want /clear kept for the dismissal", got)
	}
	if eng.clearCalls != 0 {
		t.Fatalf("ClearContext calls = %d, want 0 — nothing runs at the fault itself", eng.clearCalls)
	}

	m, cmd := stepCmd(t, m, keyEnter()) // dismiss the error: the queued command runs here

	if m.state != stateIdle {
		t.Fatalf("state = %v; want idle once the error is dismissed", m.state)
	}
	if eng.clearCalls != 1 {
		t.Errorf("ClearContext calls = %d, want 1 — the queued /clear ran at the dismissal", eng.clearCalls)
	}
	if n := len(m.deferredCommands); n != 0 {
		t.Errorf("queued commands = %d; want the queue drained", n)
	}
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged messages = %d; want the message still held — the dismissal sends nothing", n)
	}
	drainCmd(t, m, cmd)
	if n := len(eng.submitted); n != 0 {
		t.Errorf("submitted = %+v; want nothing sent by the dismissal", eng.submitted)
	}

	m, cmd = stepCmd(t, m, keyEnter()) // the next ⏎ sends the held message
	if m.state != stateRunning {
		t.Fatalf("state = %v; want running — the second ⏎ sends the held queue", m.state)
	}
	drainCmd(t, m, cmd)
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != "still worth sending" {
		t.Errorf("submitted = %+v; want the held message sent by the second press", eng.submitted)
	}
}

// ----------------------------------------------------------------------------
// The boundary confirm: /clear, a switch and /fork with workflows running (ADR 0089 D5)
// ----------------------------------------------------------------------------

// runningWorkflowModel is a ready, idle model with one background workflow running, as its started
// phase folded.
func runningWorkflowModel(t *testing.T, eng *fakeEngine) Model {
	t.Helper()
	m := newTestModelEng(t, eng, testOpts)
	return foldEvents(t, m, bgPhase(domain.WorkflowStarted))
}

// assertBoundaryConfirm fails the test unless the stop-or-keep confirm is up with its question.
func assertBoundaryConfirm(t *testing.T, m Model) {
	t.Helper()
	if !m.boundaryConfirmOpen() {
		t.Fatalf("picker = %+v; want the stop-or-keep confirm open", m.picker)
	}
	if got := m.pickerTitle(); got != boundaryConfirmTitle {
		t.Errorf("confirm title = %q, want %q", got, boundaryConfirmTitle)
	}
}

// /clear with a workflow running asks before it clears; y clears without keeping them, n clears
// keeping them and says so, and esc clears nothing.
func TestClearWithAWorkflowRunningAsksToStopIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		key       tea.KeyPressMsg
		wantKept  []bool
		wantNoted bool
	}{
		{"y stops them", keyRune('y'), []bool{false}, false},
		{"n keeps them", keyRune('n'), []bool{true}, true},
		{"esc cancels the clear", keyEsc(), nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			eng := &fakeEngine{}
			m := runningWorkflowModel(t, eng)

			m, _ = typeCommand(t, m, "/clear")
			assertBoundaryConfirm(t, m)
			if eng.clearCalls != 0 {
				t.Fatalf("ClearContext calls = %d before the answer, want 0", eng.clearCalls)
			}
			m = step(t, m, tc.key)

			clears, _, _ := eng.boundaryKeeps()
			if !reflect.DeepEqual(clears, tc.wantKept) {
				t.Errorf("clears (kept per call) = %v, want %v", clears, tc.wantKept)
			}
			if m.picker.open {
				t.Error("the confirm stayed open after its answer")
			}
			if noted := slices.Contains(noteTexts(m), workflowsKeptNote); noted != tc.wantNoted {
				t.Errorf("kept note present = %v, want %v (notes %q)", noted, tc.wantNoted, noteTexts(m))
			}
		})
	}
}

// With no workflow running /clear asks nothing and clears at once, not kept.
func TestClearWithNoWorkflowRunningAsksNothing(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)

	m, _ = typeCommand(t, m, "/clear")

	if m.picker.open {
		t.Errorf("picker = %+v; want no confirm with nothing running", m.picker)
	}
	if clears, _, _ := eng.boundaryKeeps(); !reflect.DeepEqual(clears, []bool{false}) {
		t.Errorf("clears (kept per call) = %v, want one unkept clear", clears)
	}
}

// A queued /clear that asks holds the queue behind it: the staged message is neither flushed nor
// dropped while the confirm is up, and the answer's fold clears and then flushes it into the fresh
// session — the order the human typed them in.
func TestQueuedClearWithAWorkflowRunningHoldsTheFlushUntilAnswered(t *testing.T) {
	t.Parallel()
	m, eng := queuedRun(t, "/clear", "a message")
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))

	m = step(t, m, exchangeDoneMsg{})

	assertBoundaryConfirm(t, m)
	if eng.clearCalls != 0 || m.state != stateIdle {
		t.Fatalf("ClearContext calls %d, state %v while the confirm is up; want none, idle", eng.clearCalls, m.state)
	}
	if n := len(m.pendingInterjections); n != 1 {
		t.Fatalf("staged messages = %d while the confirm is up; want the message held", n)
	}
	next, cmd := stepCmd(t, m, keyRune('n'))

	if clears, _, _ := eng.boundaryKeeps(); !reflect.DeepEqual(clears, []bool{true}) {
		t.Errorf("clears (kept per call) = %v, want one kept clear", clears)
	}
	if next.state != stateRunning || len(next.pendingInterjections) != 0 {
		t.Fatalf("state %v, staged %d after the answer; want the message flushed into a new Exchange",
			next.state, len(next.pendingInterjections))
	}
	drainCmd(t, next, cmd)
	if n := len(eng.submitted); n != 1 || eng.submitted[0].Text != "a message" {
		t.Errorf("submitted = %+v; want the message sent after the clear", eng.submitted)
	}
}

// A /sessions switch with a workflow running asks the same question, and the answer rides to the
// restore: n marks it kept, and the restored session's own workflows are resumed once its Activate
// has landed.
func TestSessionSwitchWithAWorkflowRunningAsksAndResumesTheRestoredSet(t *testing.T) {
	t.Parallel()
	host := &fakeSessionHost{}
	storeMeta(host, "sess-1", "stored", "/ws/a", time.Now(), 0, nil)
	eng := &fakeEngine{}
	m := newBrowserModel(t, eng, host, "/ws/a")
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))
	m = openBrowser(t, m)

	m, cmd := stepCmd(t, m, keyEnter())
	assertBoundaryConfirm(t, m)
	if cmd != nil {
		t.Fatal("the switch loaded the record before the confirm was answered")
	}
	m, cmd = stepCmd(t, m, keyRune('n'))
	m = foldResume(t, m, cmd)

	_, restores, resumes := eng.boundaryKeeps()
	if !reflect.DeepEqual(restores, []bool{true}) {
		t.Errorf("restores (kept per call) = %v, want one kept restore", restores)
	}
	if resumes != 1 || m.resumePending {
		t.Errorf("ResumeWorkflows calls %d, pending %v after the Activate landed; want one, none pending", resumes, m.resumePending)
	}
}

// /fork with a workflow running asks the same question, and y lets the switch to the child stop
// them (an unkept restore).
func TestForkWithAWorkflowRunningAsksToStopIt(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{}
	host := &fakeSessionHost{}
	m := newForkModel(t, eng, host)
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))
	m = openForkPicker(t, m)

	m = step(t, m, keyEnter())
	assertBoundaryConfirm(t, m)
	if got := eng.cutCalls; len(got) != 0 {
		t.Fatalf("CutSnapshot ran %v before the confirm was answered", got)
	}
	m, cmd := stepCmd(t, m, keyRune('y'))
	runWrites(t, m, cmd)

	if _, restores, _ := eng.boundaryKeeps(); !reflect.DeepEqual(restores, []bool{false}) {
		t.Errorf("restores (kept per call) = %v, want one unkept restore to the child", restores)
	}
}

// A session whose only work is a /bg workflow still running is saved at quit — its snapshot is what
// a resume starts the workflow again from — and a --resume start resumes the stored set once, at
// the first fold that finds the engine bound and idle.
func TestABgOnlySessionIsSavedAtQuitAndResumedOnStart(t *testing.T) {
	t.Parallel()
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	m = foldEvents(t, m, bgPhase(domain.WorkflowStarted))

	next, cmd := m.quit()
	runWrites(t, next.(Model), cmd)

	if saves := host.savedCalls(); len(saves) != 1 {
		t.Fatalf("saves at quit = %d, want the /bg-only session saved", len(saves))
	}

	eng := &fakeEngine{}
	resumed := newModel(context.Background(), eng, Options{Sessions: host, UI: testUIPrefs,
		Resumed: &ResumedSession{Title: "bg only"}}, nil)
	resumed = step(t, resumed, tea.WindowSizeMsg{Width: 80, Height: 24})
	resumed = step(t, resumed, tea.WindowSizeMsg{Width: 81, Height: 24})

	if _, _, resumes := eng.boundaryKeeps(); resumes != 1 {
		t.Errorf("ResumeWorkflows calls = %d after a --resume start, want exactly one", resumes)
	}
}

// A workflow that cannot resume is noted, not swallowed.
func TestAFailedWorkflowResumeIsNoted(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{resumeErr: errors.New("apogee: resume workflow w: no folder")}
	m := newModel(context.Background(), eng, Options{UI: testUIPrefs, Resumed: &ResumedSession{Title: "t"}}, nil)

	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	want := "could not resume background workflows: apogee: resume workflow w: no folder"
	if !slices.Contains(noteTexts(m), want) {
		t.Errorf("notes = %q, want %q", noteTexts(m), want)
	}
}
