package tui

import (
	"context"
	"errors"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The worker (phase-2 detail plan §3 C1/C4)
// ----------------------------------------------------------------------------

// startExchange builds the cancellable worker that drives one Exchange over eng. It returns
// the tea.Cmd the model schedules (Bubble Tea runs it on its own goroutine) and the
// CancelFunc the model stores, so a stop key cancels the in-flight Step at the next
// quiescent boundary (phase-2 detail plan §3 C4). Only one worker runs at a time — the model
// refuses input while running — so eng is only ever driven from the current worker, and the
// Agent's single-goroutine contract holds by construction (C1).
//
// parent is the program's context; deriving the worker ctx from it means a program-wide
// shutdown also cancels an in-flight Exchange. box is this Exchange's interjection mailbox — the
// Update goroutine stages what the human types while the model works, and the worker delivers it
// between Steps (a nil box is simply an Exchange nothing can be interjected into). notify sends a
// per-Turn snapshot and the interjection-delivery report into the running program (Run wires it to
// the Bridge's late-bound sender); a nil notify disables per-Turn saves, which is exactly what the
// seam tests that drive driveExchange in isolation pass.
func startExchange(parent context.Context, eng Engine, input domain.UserInput, box *interjectBox, notify func(tea.Msg)) (tea.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	cmd := func() tea.Msg { return driveExchange(ctx, eng, input, box, notify) }
	return cmd, cancel
}

// startCompact builds the cancellable worker that runs one /compact over eng — a single
// upstream summary call that must not block the Update loop (ADR 0011), so it rides the same
// worker path as an Exchange. It returns the tea.Cmd the model schedules and the CancelFunc
// the model stores so Esc cancels the in-flight compaction. A cancel surfaces as the shared
// cancelledMsg (the model's cancel handling — AbortExchange is a safe no-op here); otherwise
// the terminal Msg is compactDoneMsg carrying whatever Compact reported.
//
// The outcome is classified from Compact's returned error, NOT a fresh ctx.Err() read: an Esc
// that lands after Compact has already committed the fold returns a nil error, so it must be
// reported as compacted, not cancelled. Only an error that is context.Canceled — which the
// reducer returns exactly when the cancel pre-empted the summary and left the conversation
// untouched — becomes cancelledMsg.
func startCompact(parent context.Context, eng Engine) (tea.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	cmd := func() tea.Msg {
		skipped, err := eng.Compact(ctx)
		if errors.Is(err, context.Canceled) {
			return cancelledMsg{}
		}
		return compactDoneMsg{Skipped: skipped, Err: err}
	}
	return cmd, cancel
}

// startResume builds the cancellable worker that resumes an interrupted Exchange in place — a
// session restored mid-task whose open Exchange waits at a quiescent boundary (eng.InExchange() is
// true right after such a restore). It is startExchange without the Submit: the Exchange is already
// open — the restored snapshot round-tripped InExchange: true — so there is nothing new to enqueue
// and the worker Steps straight on. It returns the tea.Cmd the model schedules and the CancelFunc it
// stores exactly as startExchange does, and notify carries the same per-Turn snapshots. The model
// launches this only from the /continue drive when eng.InExchange() (model.go); the single-worker
// invariant keeps eng driven from one goroutine, so C1 still holds. It takes an interjection box
// for the same reason startExchange does — a resumed Exchange is a running Exchange, and the human
// may type into it.
func startResume(parent context.Context, eng Engine, box *interjectBox, notify func(tea.Msg)) (tea.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	cmd := func() tea.Msg { return driveResume(ctx, eng, box, notify) }
	return cmd, cancel
}

// driveExchange runs one Exchange from its Submit to the quiescent Exchange boundary and returns
// the single terminal Msg the model folds. It Submits the input, then hands off to stepToBoundary —
// the canonical drive loop (Agent.Run / the bench's coreagent.Run). All intermediate output —
// streamed tokens, tool calls, approvals, results — reaches the UI as Events through the teaSink,
// never through this return value (the Cmd yields exactly one Msg, at the end).
//
// It is one of the two callers of eng's drive methods (driveResume is the other); only one worker
// ever runs at a time, which is what preserves the single-goroutine contract (C1).
func driveExchange(ctx context.Context, eng Engine, input domain.UserInput, box *interjectBox, notify func(tea.Msg)) tea.Msg {
	if err := eng.Submit(input); err != nil {
		return errMsg{Err: err}
	}
	return stepToBoundary(ctx, eng, box, notify)
}

// driveResume Steps an already-open Exchange to its quiescent boundary and returns the single
// terminal Msg the model folds — driveExchange minus the Submit. It is the TUI counterpart of the
// bench's re-Step resume path (AbortExchange's doc contrasts the two: the bench re-Steps to
// re-attempt a cancelled Turn; the TUI's /continue re-Steps to finish a session interrupted
// mid-task). The restored engine is already inExchange, so re-Stepping continues the unfinished
// Turn rather than opening a new one; per-Turn notify, cancel, and terminal handling are identical
// to driveExchange because both run stepToBoundary.
func driveResume(ctx context.Context, eng Engine, box *interjectBox, notify func(tea.Msg)) tea.Msg {
	return stepToBoundary(ctx, eng, box, notify)
}

// stepToBoundary is the shared Step loop both drive paths run once the Exchange is open —
// driveExchange after its Submit, driveResume straight away. It Steps to the quiescent Exchange
// boundary, treating StatusTurnComplete as "keep stepping," and returns the single terminal Msg the
// model folds: cancelledMsg on a user stop, exchangeDoneMsg on the final boundary (and on any
// future terminal status). The StepStatus set is open; only StatusTurnComplete continues.
//
// After each committed Turn it snapshots the engine and hands the snapshot to notify for a per-Turn
// save (the session system's every-Turn cadence). The snapshot is valid here because between Steps
// this worker is the engine's single driver (agent.go). It is sent AFTER the Turn's Events — the
// teaSink delivered them synchronously inside the Step that just returned — so the Model folds it
// into a transcript consistent with the snapshot (the events-before-notify ordering the existing
// exchangeDoneMsg path already relies on). A Snapshot error simply skips that Turn's save; the loop
// keeps stepping.
//
// Before each Step it also empties the interjection mailbox into the open Exchange
// (deliverInterjections): the same between-Steps window Snapshot occupies, now carrying the human's
// mid-task remarks into the conversation the next Step's request is built from (ADR 0025).
func stepToBoundary(ctx context.Context, eng Engine, box *interjectBox, notify func(tea.Msg)) tea.Msg {
	for {
		deliverInterjections(eng, box, notify)
		res, err := eng.Step(ctx)
		if err != nil {
			return errMsg{Err: err}
		}
		switch res.Status {
		case domain.StatusTurnComplete:
			if notify != nil {
				if snap, snapErr := eng.Snapshot(); snapErr == nil {
					notify(turnSnapshotMsg{Sess: snap})
				}
			}
			continue
		case domain.StatusCancelled:
			return cancelledMsg{Result: res}
		default: // StatusExchangeComplete and any future terminal status
			return exchangeDoneMsg{Result: res}
		}
	}
}

// deliverInterjections empties box into the open Exchange and reports what landed. It runs on the
// worker goroutine between Steps — the boundary at which Engine.Interject is legal (tui.go), the
// same one Snapshot uses — so the delivered messages are already in the conversation when the next
// Step builds its request, and the model reads them after the tool results of the Turn just past.
//
// Delivery is FIFO and one message per staged row: the human wrote them in order, and a 1:1
// row↔message mapping keeps the transcript an honest record of what the model saw. The rows that
// landed go out as ONE interjectedMsg before the Step, so the Update loop moves exactly them into
// the transcript; an empty mailbox sends nothing at all.
//
// The first refusal STOPS the drain rather than skipping past it. An Interject error is a statement
// about the Exchange (no open Exchange, or an input carrying nothing), not about that one row, so
// pressing on would only produce more of the same — and delivering row 3 after row 2 was refused
// would reorder the human's remarks. The refused rows are not lost: they never appear in the report,
// so the Model keeps them staged and the terminal flush sends them (ADR 0025). In the shipped wiring
// the error cannot fire mid-Exchange at all; this is the honest degradation, not a live path.
func deliverInterjections(eng Engine, box *interjectBox, notify func(tea.Msg)) {
	staged := box.drainAll()
	if len(staged) == 0 {
		return
	}
	delivered := make([]queuedInterjection, 0, len(staged))
	for _, it := range staged {
		if err := eng.Interject(it.input); err != nil {
			break
		}
		delivered = append(delivered, it)
	}
	if notify != nil {
		notify(interjectedMsg{items: delivered})
	}
}
