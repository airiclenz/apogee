package main

// An idle-only command typed mid-run is queued and runs at idle (ADR 0025 D10, amended 2026-09-14).
//
// The unit tests in internal/tui drive the folds directly; what this proves is the surface a human
// sees on a real run over a real upstream: the "queued command" row in the band above the box while
// the Turn is still in flight, and the command's own effect — the compaction note — once the
// Exchange has ended under its own power and the queue drained, with no second keypress.

import (
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

const (
	// queuedCommandRow is the band row the queued /compact paints (internal/tui/interject.go).
	queuedCommandRow = "queued command: /compact"

	// compactedNote is what a compaction that LANDED writes (internal/tui/commandrun.go).
	compactedNote = "context compacted"
)

func TestE2EQueuedCommandRunsAtIdle(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "queued-command"))
	drv := tuitest.NewDriver(t, e2eSize)
	launchTUI(t, drv, stub)
	waitIdle(drv)

	// One completed Exchange first, so the conversation has something for /compact to fold.
	submit(drv, "Say hello")
	drv.WaitText("Hello there.")
	waitIdle(drv)

	// The Turn that hangs, and the command typed while it does. No WaitQuiet here and there cannot
	// be: a running Exchange spins, so the screen is never quiet until the hang expires.
	submit(drv, "Think about this for a while")
	drv.WaitText(busyPlaceholder)
	submit(drv, "/compact")
	drv.WaitText(queuedCommandRow)
	if _, _, found := drv.Frame().Find(compactedNote); found {
		t.Fatal("the compaction ran while the Turn was still in flight; a queued command waits for idle")
	}

	// The hang expires, the Exchange ends on its own, and the queued command runs at that idle: the
	// row leaves the band and the compaction's note lands in the transcript.
	drv.WaitText(compactedNote)
	drv.WaitGone(queuedCommandRow)
	waitIdle(drv)
}
