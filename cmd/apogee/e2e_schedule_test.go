package main

// T-07 of the v0.17.1 release checklist — "an abandoned final Turn faults" — as tests, for the two
// surfaces a human had to read by eye: the TUI's firing block and the daemon's verb column.
//
// It was manual because a real daemon firing costs a cycle of wall clock and the TUI firing block
// had no driver. Both halves are now driven: the cycle is a fake Clock (tuiScheduleClock,
// daemonClock — the Driver-level seams ADR 0062 allows, the engine untouched), and the upstream is
// testdata/stubllm/empty-reply.yaml, which answers every request with nothing at all. That is the
// whole provocation: a final Turn that neither spoke nor acted is a Turn the engine abandons, and
// what the checklist asks is whether the two surfaces then SAY so.
//
// The headless third of T-07 is cmd/apogee/headless_test.go's TestHeadlessExitCodes and stays there.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// TestE2EFiringMarksAnAbandonedFinalTurn is T-07 steps 8–10 in the TUI: a Schedule is created, the
// clock is advanced, the Firing runs against a server that says nothing, and the block it leaves
// behind reads as a fault rather than as an answer.
//
// The cell and the reason are read after the block is EXPANDED, because collapsed is the default
// (layout.md; TestFiringBlockCollapsesToItsRemainderCount) and a collapsed firing block is its
// header and its branch alone. Step 10 of the checklist opens it with a click for exactly this
// reason; a driver opens it with the keyboard route to the same toggle (⌥↑ then ⏎).
func TestE2EFiringMarksAnAbandonedFinalTurn(t *testing.T) {
	clock := useFakeScheduleClock(t)
	stub := stubllm.New(t, loadScript(t, "empty-reply"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	// Step 8 — the Schedule goes on the clock. The library's own EventCreated is the confirmation,
	// and it states the cycle back, so waiting for it is waiting for a Schedule that really is live.
	submit(drv, "/schedule 30s say hi")
	drv.WaitText("every 30s")
	drv.WaitQuiet(settled)

	// One tick makes every live Schedule due at once — the half-minute floor spent in a microsecond,
	// which is the only reason this test fits in the package's budget.
	clock.tick()

	// The Firing announced itself as a block, ran, and came back. "finished — no answer" is the
	// summary slot a run that produced no text earns, and it is the frame's signal that the block
	// has been enriched: everything below is about the enriched block.
	drv.WaitText("Schedule")
	drv.WaitText(scheduleNoAnswer)
	drv.WaitQuiet(settled)

	expandLastBlock(drv)

	// Step 9 — the stats line's LAST cell says faulted, and it is on the stats line rather than in
	// the summary slot above it: the summary belongs to whatever the run did say.
	stats := rowContaining(t, drv.Frame(), scheduleFaulted)
	if !strings.HasSuffix(strings.TrimSpace(stats), scheduleFaulted) {
		t.Errorf("the stats line is %q; %q must be its last cell", strings.TrimSpace(stats), scheduleFaulted)
	}
	if !strings.Contains(stats, " · "+scheduleFaulted) {
		t.Errorf("the stats line is %q; the faulted cell must sit beside the turn and time cells", stats)
	}

	// Step 10 — the expanded body says WHY, in the upstream's own words, and points at the record.
	flat := flatten(drv.Frame().String())
	for _, want := range []string{
		"final turn abandoned — upstream returned an empty reply (finish: stop)",
		"saved as",
		"find it in /sessions",
	} {
		if !strings.Contains(flat, flatten(want)) {
			t.Errorf("the expanded firing block does not say %q:\n%s", want, drv.Frame())
		}
	}
	// And the record the pointer promises is really there.
	if len(sess.sessionRecords()) == 0 {
		t.Error("the block points at a saved record, but the session store is empty")
	}

	// Step 7's TUI twin — a faulted Firing is not a broken session: the Schedule comes off the clock
	// on request and the run quits clean.
	submit(drv, "/schedule-stop")
	drv.WaitText("stopped")
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit after a faulted firing", err)
	}
}

// TestDaemonFaultedVerbColumn is T-07 steps 4–7: `apogee daemon`, driven in process against the same
// silent upstream, logs its faulted Firing under the verb column a supervisor's journal is scanned
// by — and then exits 0, because a faulted firing is not a daemon failure.
//
// It runs the REAL runner rather than the daemon harness's stub: the verb is a fact about what
// run.Once reported, and a stubbed runner would only prove that the log formats a Faulted flag the
// test itself set. The formatting is pinned separately and purely (TestDaemonLogLines).
func TestDaemonFaultedVerbColumn(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "empty-reply"))
	h := newDaemonHarness(t)
	// The harness installs a stub runner; this test wants the composition. Its own t.Cleanup puts
	// the production value back, so nothing here has to.
	runOnce = run.Once
	writeConfigHome(t, h.home, "servers:\n"+
		"  - name: stub\n    endpoint: "+stub.URL+"\n    model: "+stub.Model+"\nserver: stub\n")
	ws := t.TempDir()
	h.writeSchedules(t, "schedules:\n"+
		"  - name: fault-probe\n"+
		"    on:\n      cycle: 30s\n"+
		"    run:\n      prompt: say hi\n      workspace: "+ws+"\n      mode: plan\n")

	wait := h.run(t)
	h.awaitLog(t, "1 schedule on the clock")
	h.clock.tick()

	// Step 6 — the verb column, padded to the same nine characters `completed` fills, so a journal
	// stays aligned; and the reason leading, ahead of the counts.
	h.awaitLog(t, "faulted   fault-probe")
	h.awaitLog(t, "final turn abandoned (upstream returned an empty reply (finish: stop))")

	// Step 7 — the daemon stops on request and reports no failure of its own.
	h.stop()
	if err := wait(); err != nil {
		t.Fatalf("the daemon returned %v; a faulted firing is not a daemon failure\n%s", err, h.errOut.String())
	}
	if got := h.out.String(); strings.Contains(got, "completed  fault-probe") {
		t.Errorf("the faulted firing was also logged as completed:\n%s", got)
	}
}

// The two wordings this file asserts on, spelled once. They are internal/tui's own constants
// (scheduleFaultedCell, scheduleNoAnswerSummary) restated here because cmd/apogee cannot import
// them — and that is the point of restating them: a rename over there fails here, which is exactly
// what a release checklist's verbatim oracle is for.
const (
	scheduleFaulted  = "faulted"
	scheduleNoAnswer = "finished — no answer"
)

// useFakeScheduleClock puts the interactive Scheduler on a clock the test drives and gives it back
// at the end of the test. The fake is the daemon tests' own (fakeDaemonClock): one tick makes every
// live Schedule due, which is how a thirty-second floor is crossed inside a test.
//
// Tests that swap it never call t.Parallel — it is a package var, like every other seam in this
// package.
func useFakeScheduleClock(t *testing.T) *fakeDaemonClock {
	t.Helper()

	clock := newFakeDaemonClock()
	prev := tuiScheduleClock
	tuiScheduleClock = clock
	t.Cleanup(func() { tuiScheduleClock = prev })
	return clock
}
