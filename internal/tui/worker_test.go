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
