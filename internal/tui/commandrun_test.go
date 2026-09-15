package tui

import (
	"errors"
	"reflect"
	"testing"
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
	m, eng := queuedRun(t, "/compact", "/clear")

	next, cmd := stepCmd(t, m, exchangeDoneMsg{})

	if next.state != stateRunning || next.box != nil {
		t.Fatalf("state = %v, box = %v; want the compaction worker running with no mailbox", next.state, next.box)
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
