package main

// A session boundary over a running background workflow, end to end: `/clear`, and a `/sessions`
// switch, each typed while a background sub_agent (ADR 0094) is held running, answered `y` at the
// stop-or-keep confirm (ADR 0089 D5), must land — the cleared view, the resumed session — inside the
// driver's default timeout.
//
// It guards the 2026-10-05 audit's deadlock: the boundary stopped the workflows by waiting for each
// run to end on the TUI's Update goroutine, while the run's last event waited in tea.Program.Send
// for that same goroutine — and the TUI hung for good. The engine half is pinned in internal/agent
// (TestBackground_AClearDoesNotWaitOnABlockedSink); this file drives the real composition, where
// the sink really is the program.

import (
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The fixture's own words, restated here so an assertion reads as the claim it is making
// (testdata/stubllm/background-boundary.yaml).
const (
	bgBoundaryHello      = "Say hello before the scout."
	bgBoundaryHelloReply = "Hello before the scout."
	bgBoundaryPrompt     = "Scout the repository in the background."
	bgBoundaryAnswered   = "The scout is running in the background."
	// bgBoundaryGate holds the child's answer, so the workflow is running when the boundary comes.
	bgBoundaryGate = "scouted"
	// bgBoundaryConfirm is the boundary confirm's title (internal/tui's boundaryConfirmTitle).
	bgBoundaryConfirm = "stop running workflows? (y/n)"
	// bgBoundaryResumed opens the note a /sessions switch leaves once the restore has gone through
	// (internal/tui's resumeLoaded).
	bgBoundaryResumed = "resumed:"
)

// TestE2EBackgroundClearDoesNotHang types `/clear` over a running background workflow, answers the
// confirm with `y`, and sees the cleared view.
func TestE2EBackgroundClearDoesNotHang(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "background-boundary"))
	sess, drv := launchBackgroundBoundary(t, stub)
	startHeldScout(drv)

	submit(drv, "/clear")
	stopAtTheConfirm(drv)

	// The cleared view: the closed conversation's reply leaves the transcript, which only the clear's
	// own fold can do — the view is reset once the engine has accepted the clear.
	drv.WaitGone(bgBoundaryAnswered)
	drv.WaitText(idlePromptHead)

	stub.Release(bgBoundaryGate)
	drv.WaitQuiet(settled)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EBackgroundSessionSwitchDoesNotHang switches to another stored session through `/sessions`
// over a running background workflow, answers the confirm with `y`, and sees the restored session.
func TestE2EBackgroundSessionSwitchDoesNotHang(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "background-boundary"))
	sess, drv := launchBackgroundBoundary(t, stub)

	// The session to switch to: one turn, saved, then closed by a /clear that has nothing running
	// to ask about.
	submit(drv, bgBoundaryHello)
	drv.WaitText(bgBoundaryHelloReply)
	drv.WaitFor(func() bool { return len(sess.sessionRecords()) == 1 },
		tuitest.Awaiting("the first session to reach the session store"))
	submit(drv, "/clear")
	drv.WaitGone(bgBoundaryHelloReply)

	// The session this test leaves, saved too, so the browser lists it on top of the one above.
	startHeldScout(drv)
	drv.WaitFor(func() bool { return len(sess.sessionRecords()) == 2 },
		tuitest.Awaiting("the scouting session to reach the session store"))

	// The browser lists the newest first, and the newest is the conversation the run is in, so the
	// row below the top one is the greeting session.
	submit(drv, "/sessions")
	drv.WaitText("⏎ resume")
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Down)
	drv.WaitQuiet(settled)
	drv.Press(tuitest.Enter)
	stopAtTheConfirm(drv)

	// The restored session: its note lands, which only the restore's own fold can write — it is
	// added once the engine has accepted the restore.
	drv.WaitText(bgBoundaryResumed)
	drv.WaitGone(bgBoundaryAnswered)
	drv.WaitText(bgBoundaryHelloReply)

	stub.Release(bgBoundaryGate)
	drv.WaitQuiet(settled)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// launchBackgroundBoundary starts apogee against stub on a two-agent home with `workflow` lifted,
// so sub_agent offers its background switch and the held child runs beside the parent.
func launchBackgroundBoundary(t *testing.T, stub *stubllm.Server) (*e2eSession, *tuitest.Driver) {
	t.Helper()

	home := preemptHome(t, stub, 2)
	appendHomeConfig(t, home, bgSubAgentTools)
	drv := tuitest.NewDriver(t, e2eSize)
	return launchTUIOn(t, drv, stub, home, ""), drv
}

// startHeldScout launches the background sub_agent and returns once the parent's reply has ended
// with the child still held on its gate: a background workflow is running and the TUI is idle.
func startHeldScout(drv *tuitest.Driver) {
	submit(drv, bgBoundaryPrompt)
	drv.WaitText(bgBoundaryAnswered)
	drv.WaitQuiet(settled)
}

// stopAtTheConfirm waits for the boundary's stop-or-keep confirm and answers `y`, letting the
// boundary stop the running workflow.
func stopAtTheConfirm(drv *tuitest.Driver) {
	drv.WaitText(bgBoundaryConfirm)
	drv.WaitQuiet(settled)
	drv.Type("y")
}
