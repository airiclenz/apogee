package tui

import (
	"context"
	"errors"
	"reflect"
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

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "hi"}, nil, nil, nil)

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

// TestDriveExchangeNotifiesPerTurn proves the per-Turn save cadence: the worker snapshots the
// engine after each StatusTurnComplete and hands the snapshot to notify — one turnSnapshotMsg per
// completed Turn, and none on the terminal StatusExchangeComplete (the Model saves at idle itself).
func TestDriveExchangeNotifiesPerTurn(t *testing.T) {
	t.Parallel()
	snaps := []domain.Session{
		{State: []byte(`{"turn":0}`)},
		{State: []byte(`{"turn":1}`)},
	}
	var i int
	eng := &fakeEngine{
		stepFn: scriptedSteps(
			stepResult{res: domain.StepResult{Status: domain.StatusTurnComplete}},
			stepResult{res: domain.StepResult{Status: domain.StatusTurnComplete}},
			stepResult{res: domain.StepResult{Status: domain.StatusExchangeComplete}},
		),
		snapshotFn: func() (domain.Session, error) { s := snaps[i]; i++; return s, nil },
	}

	var got []domain.Session
	notify := func(msg tea.Msg) {
		if ts, ok := msg.(turnSnapshotMsg); ok {
			got = append(got, ts.Sess)
		}
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "hi"}, nil, notify, nil)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
	}
	if len(got) != 2 {
		t.Fatalf("per-Turn notifications = %d; want one per completed Turn (2)", len(got))
	}
	if string(got[0].State) != `{"turn":0}` || string(got[1].State) != `{"turn":1}` {
		t.Errorf("per-Turn snapshots = %q,%q; want turn 0 then turn 1", got[0].State, got[1].State)
	}
}

// TestStepBoundaryFlushesCoalescedTokens pins the invariant token coalescing must not break: every
// Event a Step emitted has reached the Update loop by the time anything the worker sends about that
// Step does. The sink's window outlives the test, so nothing can arrive by timer here — the tokens
// the program holds when the Turn's snapshot is sent, and when the drive returns, are there because
// the Step boundary flushed them.
func TestStepBoundaryFlushesCoalescedTokens(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)
	eng := &fakeEngine{
		stepFn: func(_ context.Context, call int) (domain.StepResult, error) {
			base := domain.EventBase{Turn: call}
			sink.Emit(domain.TokenEvent{EventBase: base, Text: "tok"})
			sink.Emit(domain.TokenEvent{EventBase: base, Text: "en"})
			if call == 0 {
				return domain.StepResult{Status: domain.StatusTurnComplete}, nil
			}
			return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
		},
	}

	atSnapshot := -1
	notify := func(msg tea.Msg) {
		if _, ok := msg.(turnSnapshotMsg); ok {
			atSnapshot = len(prog.events())
		}
	}

	msg := driveExchange(context.Background(), eng, domain.UserInput{Text: "hi"}, nil, notify, sink.flush)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
	}
	if atSnapshot != 1 {
		t.Errorf("events delivered when the Turn's snapshot was sent = %d; want that Turn's merged token (1)", atSnapshot)
	}
	want := []domain.Event{
		domain.TokenEvent{EventBase: domain.EventBase{Turn: 0}, Text: "token"},
		domain.TokenEvent{EventBase: domain.EventBase{Turn: 1}, Text: "token"},
	}
	if got := prog.events(); !reflect.DeepEqual(got, want) {
		t.Errorf("events = %#v; want %#v", got, want)
	}
	if stillBuffering(sink) {
		t.Error("a token was still buffered after the drive returned; it would land after the Model folded the Exchange")
	}
}

// TestCancelledStepFlushesCoalescedTokens is the mid-stream Esc that made this flush necessary. A
// cancelled Turn returns emitting no further Event (internal/agent/loop.go), so no boundary event
// can push the buffer out: without the Step-boundary flush the tail would ride the window timer and
// land after the Model folded cancelledMsg — after finishWorker zeroed the generation clock, which
// the late token would re-latch, timing the NEXT turn's tok/s across the human's idle time.
func TestCancelledStepFlushesCoalescedTokens(t *testing.T) {
	t.Parallel()
	sink, prog := newBufferingSink(t)
	eng := &fakeEngine{
		stepFn: func(ctx context.Context, _ int) (domain.StepResult, error) {
			sink.Emit(domain.TokenEvent{Text: "half a rep"})
			<-ctx.Done()
			sink.Emit(domain.TokenEvent{Text: "ly"}) // the last delta before the loop reports the cancel
			return domain.StepResult{Status: domain.StatusCancelled}, nil
		},
	}

	cmd, cancel := startExchange(context.Background(), eng, domain.UserInput{Text: "go"}, nil, nil, sink.flush)

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

	want := []domain.Event{domain.TokenEvent{Text: "half a reply"}}
	if got := prog.events(); !reflect.DeepEqual(got, want) {
		t.Errorf("events = %#v; want %#v", got, want)
	}
	if stillBuffering(sink) {
		t.Error("a token was still buffered when the cancelled drive returned; it would re-latch the generation clock")
	}
}

// TestDriveResumeStepsWithoutSubmit proves the interrupted-resume drive: driveResume Steps an
// already-open Exchange to its StatusExchangeComplete boundary WITHOUT a Submit (the snapshot
// round-tripped InExchange: true, so the Exchange is already open), notifying per completed Turn
// exactly like driveExchange.
func TestDriveResumeStepsWithoutSubmit(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{
		stepFn: scriptedSteps(
			stepResult{res: domain.StepResult{Status: domain.StatusTurnComplete}},
			stepResult{res: domain.StepResult{Status: domain.StatusExchangeComplete}},
		),
	}

	var turns int
	notify := func(msg tea.Msg) {
		if _, ok := msg.(turnSnapshotMsg); ok {
			turns++
		}
	}

	msg := driveResume(context.Background(), eng, nil, notify, nil)

	if _, ok := msg.(exchangeDoneMsg); !ok {
		t.Fatalf("terminal msg = %T; want exchangeDoneMsg", msg)
	}
	if eng.submits() != 0 {
		t.Errorf("Submit calls = %d; want 0 (a resume re-Steps an already-open Exchange)", eng.submits())
	}
	if eng.steps() != 2 {
		t.Errorf("Step calls = %d; want 2", eng.steps())
	}
	if turns != 1 {
		t.Errorf("per-Turn notifications = %d; want one for the single completed Turn", turns)
	}
}

// TestStartResumeCancelYieldsCancelledMsg proves the resume worker honours a cancel the same way
// startExchange does: the CancelFunc unblocks the in-flight Step, which returns StatusCancelled, and
// the worker hands back a cancelledMsg — with no Submit ever made.
func TestStartResumeCancelYieldsCancelledMsg(t *testing.T) {
	t.Parallel()
	eng := &fakeEngine{
		stepFn: func(ctx context.Context, _ int) (domain.StepResult, error) {
			<-ctx.Done()
			return domain.StepResult{Status: domain.StatusCancelled}, nil
		},
	}

	cmd, cancel := startResume(context.Background(), eng, nil, nil, nil)

	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()

	cancel()

	select {
	case msg := <-out:
		if _, ok := msg.(cancelledMsg); !ok {
			t.Fatalf("msg = %T; want cancelledMsg", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("resume worker did not return after cancel (deadlock)")
	}
	if eng.submits() != 0 {
		t.Errorf("Submit calls = %d; want 0 (a resume never Submits)", eng.submits())
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

	msg := driveExchange(context.Background(), eng, domain.UserInput{}, nil, nil, nil)

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

	msg := driveExchange(context.Background(), eng, domain.UserInput{}, nil, nil, nil)

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

	cmd, cancel := startExchange(context.Background(), eng, domain.UserInput{Text: "go"}, nil, nil, nil)

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
