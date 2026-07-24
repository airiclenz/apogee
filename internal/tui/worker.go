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
// shutdown also cancels an in-flight Exchange. notify sends a per-Turn snapshot into the running
// program (Run wires it to the Bridge's late-bound sender); a nil notify disables per-Turn saves,
// which is exactly what the seam tests that drive driveExchange in isolation pass.
func startExchange(parent context.Context, eng Engine, input domain.UserInput, notify func(tea.Msg)) (tea.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithCancel(parent)
	cmd := func() tea.Msg { return driveExchange(ctx, eng, input, notify) }
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

// driveExchange runs one Exchange from its Submit to the quiescent Exchange boundary and
// returns the single terminal Msg the model folds. It mirrors the canonical drive loop
// (Agent.Run / the bench's coreagent.Run): Submit the input, then Step to the boundary,
// treating StatusTurnComplete as "keep stepping." All intermediate output — streamed tokens,
// tool calls, approvals, results — reaches the UI as Events through the teaSink, never
// through this return value (the Cmd yields exactly one Msg, at the end).
//
// It is the only caller of eng's drive methods, which is what preserves the single-goroutine
// contract (C1). The StepStatus set is open; only StatusTurnComplete continues, and any other
// terminal status returns the model to idle (treated as an Exchange end) rather than looping.
//
// After each committed Turn it snapshots the engine and hands the snapshot to notify for a
// per-Turn save (the session system's every-Turn cadence). The snapshot is valid here because
// between Steps this worker is the engine's single driver (agent.go). It is sent AFTER the Turn's
// Events — the teaSink delivered them synchronously inside the Step that just returned — so the
// Model folds it into a transcript consistent with the snapshot (the events-before-notify
// ordering the existing exchangeDoneMsg path already relies on). A Snapshot error simply skips
// that Turn's save; the loop keeps stepping.
func driveExchange(ctx context.Context, eng Engine, input domain.UserInput, notify func(tea.Msg)) tea.Msg {
	if err := eng.Submit(input); err != nil {
		return errMsg{Err: err}
	}
	for {
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
