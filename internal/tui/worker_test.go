package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestDriveExchangeRunsToExchangeBoundary proves the worker mirrors the canonical drive
// loop: Submit once, then Step through each StatusTurnComplete to the StatusExchangeComplete
// boundary, returning a single exchangeDoneMsg carrying the closing StepResult.
func TestDriveExchangeRunsToExchangeBoundary(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{
		stepFn: scriptedSteps(
			stepResult{res: domain.StepResult{Status: domain.StatusTurnComplete, TurnIndex: 0}},
			stepResult{res: domain.StepResult{Status: domain.StatusTurnComplete, TurnIndex: 1}},
			stepResult{res: domain.StepResult{Status: domain.StatusExchangeComplete, TurnIndex: 2}},
		),
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "hi"})

	done, ok := msg.(exchangeDoneMsg)
	if !ok {
		t.Fatalf("msg = %T; want exchangeDoneMsg", msg)
	}
	if done.Result.Status != domain.StatusExchangeComplete {
		t.Errorf("Result.Status = %q; want %q", done.Result.Status, domain.StatusExchangeComplete)
	}
	if eng.submits() != 1 {
		t.Errorf("Submit calls = %d; want 1", eng.submits())
	}
	if eng.steps() != 3 {
		t.Errorf("Step calls = %d; want 3", eng.steps())
	}
	if got := eng.submitted[0].Text; got != "hi" {
		t.Errorf("submitted text = %q; want %q", got, "hi")
	}
}

// TestDriveExchangeSubmitError proves a Submit failure short-circuits to errMsg and never
// steps the loop.
func TestDriveExchangeSubmitError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("submit boom")
	eng := &fakeEngine{
		submitFn: func(domain.UserInput) error { return wantErr },
		stepFn: func(context.Context, int) (domain.StepResult, error) {
			t.Error("Step ran after Submit failed")
			return domain.StepResult{}, nil
		},
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{})

	e, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("msg = %T; want errMsg", msg)
	}
	if !errors.Is(e.Err, wantErr) {
		t.Errorf("errMsg.Err = %v; want %v", e.Err, wantErr)
	}
	if eng.steps() != 0 {
		t.Errorf("Step calls = %d; want 0", eng.steps())
	}
}

// TestDriveExchangeStepError proves a loop-level Step error becomes an errMsg.
func TestDriveExchangeStepError(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("step boom")
	eng := &fakeEngine{
		stepFn: func(context.Context, int) (domain.StepResult, error) {
			return domain.StepResult{}, wantErr
		},
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{})

	e, ok := msg.(errMsg)
	if !ok {
		t.Fatalf("msg = %T; want errMsg", msg)
	}
	if !errors.Is(e.Err, wantErr) {
		t.Errorf("errMsg.Err = %v; want %v", e.Err, wantErr)
	}
}

// TestStartExchangeCancelYieldsCancelledMsg proves C4: the model's CancelFunc cancels the
// in-flight Step, which returns at the next boundary with StatusCancelled, and the worker
// hands back a cancelledMsg.
func TestStartExchangeCancelYieldsCancelledMsg(t *testing.T) {
	t.Parallel()
	// The engine's Step blocks until ctx is cancelled, then reports StatusCancelled — the
	// engine's cancellation contract (ADR 0007), modelled here.
	eng := &fakeEngine{
		stepFn: func(ctx context.Context, _ int) (domain.StepResult, error) {
			<-ctx.Done()
			return domain.StepResult{Status: domain.StatusCancelled}, nil
		},
	}

	cmd, cancel := startExchange(context.Background(), eng, domain.UserInput{Text: "go"})

	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()

	cancel()

	select {
	case msg := <-out:
		if _, ok := msg.(cancelledMsg); !ok {
			t.Fatalf("msg = %T; want cancelledMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not return after cancel (deadlock)")
	}
	if eng.submits() != 1 {
		t.Errorf("Submit calls = %d; want 1", eng.submits())
	}
}

// TestStartCompactCancelYieldsCancelledMsg proves the /compact worker's cancel path: the
// CancelFunc cancels the in-flight summary call, Compact returns context.Canceled (the
// reducer's contract when a cancel pre-empts the summary, leaving the conversation untouched),
// and startCompact classifies that as the shared cancelledMsg — never a compactDoneMsg.
func TestStartCompactCancelYieldsCancelledMsg(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{
		compactFn: func(ctx context.Context) (bool, error) {
			<-ctx.Done()
			return false, ctx.Err() // context.Canceled — the pre-empted-summary contract
		},
	}

	cmd, cancel := startCompact(context.Background(), eng)

	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()

	cancel()

	select {
	case msg := <-out:
		if _, ok := msg.(cancelledMsg); !ok {
			t.Fatalf("msg = %T; want cancelledMsg (a pre-empted compaction is a cancel)", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("compact worker did not return after cancel (deadlock)")
	}
	if eng.compactCalls != 1 {
		t.Errorf("Compact calls = %d; want 1", eng.compactCalls)
	}
}

// TestStartCompactLateCancelStillReportsCompacted is the 2a inverse: an Esc that lands after
// Compact has already committed the fold returns a nil error, so the outcome must be classified
// from that returned error (compacted), not a fresh ctx.Err() read — which, with a cancelled
// ctx, would wrongly report "cancelled" and leave the gauge stale though the history had folded.
func TestStartCompactLateCancelStillReportsCompacted(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{
		// The fold committed before the (late) Esc landed: a nil error regardless of ctx.
		compactFn: func(context.Context) (bool, error) { return false, nil },
	}

	cmd, cancel := startCompact(context.Background(), eng)
	cancel() // the late Esc — the ctx is now cancelled, but Compact already returned nil

	msg := cmd()

	done, ok := msg.(compactDoneMsg)
	if !ok {
		t.Fatalf("msg = %T; want compactDoneMsg (a committed fold reports compacted despite a late cancel)", msg)
	}
	if done.Err != nil {
		t.Errorf("compactDoneMsg.Err = %v; want nil (a committed fold is not a failure)", done.Err)
	}
	if done.Skipped {
		t.Error("compactDoneMsg.Skipped = true; want false (a real fold, not a skip)")
	}
}
