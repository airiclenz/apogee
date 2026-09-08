package main

// Quitting mid-answer keeps the conversation.
//
// A ⌃c⌃c while the model is still working defers the exit until the worker unwinds (C4), and the
// closing flush rides that deferral: the record is written from the terminal fold, once the engine
// is the Update loop's again. It used to be skipped there, on the reasoning that the per-Turn
// snapshots had already captured every completed Turn — true only of an Exchange that reached a Turn
// boundary. An Exchange the model answers in ONE Step never emits one, so an interrupted first
// answer left nothing on disk and the whole session was gone.
//
// Nothing below the composition can make this claim: it is about a real quit, through the real
// key gesture, leaving a real record in a real home that a relaunch can list.

import (
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompt testdata/stubllm/quitsave.yaml holds its answer to, and the title it gives the record —
// what the relaunched browser paints for the interrupted session.
const (
	quitSavePrompt = "Take your time over this one."
	quitSaveTitle  = "Interrupted answer"

	// The invitation the box paints while an Exchange runs — what says the quit below lands on a
	// BUSY run rather than on an idle one.
	busyPlaceholder = "queue a message…"
)

func TestE2EQuitMidAnswerStillSavesTheSession(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "quitsave"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)

	// The prompt goes out and the answer is held on the server, so the run is still busy: the box
	// has given up its invitation to the running Exchange and offers to QUEUE instead. There is no
	// WaitQuiet here and there cannot be — a running Exchange spins, so the screen is never quiet
	// until the answer that is being withheld arrives.
	submit(drv, quitSavePrompt)
	drv.WaitGone(promptPlaceholder)
	drv.WaitText(busyPlaceholder)

	// ⌃c⌃c here is the interrupt this test is about. The quit cancels the in-flight request — the
	// held reply is dropped rather than written — and exits once the worker has unwound.
	if err := sess.Quit(); err != nil {
		t.Fatalf("the interrupted run returned %v; want a clean quit", err)
	}
	if n := len(sess.sessionRecords()); n != 1 {
		t.Fatalf("session records after a quit mid-answer = %d; want the interrupted conversation, saved", n)
	}
	// And the record is a resumable one: a relaunch over the same home lists it by name.
	next := sess.Relaunch()
	waitIdle(next)
	submit(next, "/sessions")
	next.WaitText(quitSaveTitle)
	next.Press(tuitest.Esc)

	if err := sess.Quit(); err != nil {
		t.Fatalf("the relaunch returned %v; want a clean quit", err)
	}
}
