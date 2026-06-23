package tui

import (
	"context"
	"errors"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestNewBridgeDelegatesNonNil proves the composition root gets usable Config delegates
// from an unbound Bridge.
func TestNewBridgeDelegatesNonNil(t *testing.T) {
	t.Parallel()
	b := NewBridge()
	if b.Sink() == nil {
		t.Error("Sink() is nil")
	}
	if b.Approver() == nil {
		t.Error("Approver() is nil")
	}
}

// TestBridgeBindRoutesSinkAndApprover proves a single Bind wires both the Sink and the
// Approver to the same running program (they share one programRef).
func TestBridgeBindRoutesSinkAndApprover(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	prog.replyWith(domain.ApprovalAllowForSession)
	b := NewBridge()
	b.Bind(prog)

	b.Sink().Emit(domain.TokenEvent{Text: "hi"})
	got, err := b.Approver().Approve(context.Background(), domain.ApprovalRequest{Tool: "t"})
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if got != domain.ApprovalAllowForSession {
		t.Errorf("decision = %q; want %q", got, domain.ApprovalAllowForSession)
	}
	prog.wait()

	// Both the eventMsg and the approvalReqMsg reached the same bound program.
	var sawEvent, sawApproval bool
	for _, m := range prog.messages() {
		switch m.(type) {
		case eventMsg:
			sawEvent = true
		case approvalReqMsg:
			sawApproval = true
		}
	}
	if !sawEvent {
		t.Error("the bound program never received the event")
	}
	if !sawApproval {
		t.Error("the bound program never received the approval request")
	}
}

// TestBridgeUnboundDelegatesAreSafe proves the delegates are safe before Bind: Emit is a
// silent no-op and Approve unblocks on a cancelled ctx rather than hanging.
func TestBridgeUnboundDelegatesAreSafe(t *testing.T) {
	t.Parallel()
	b := NewBridge() // never bound

	b.Sink().Emit(domain.TokenEvent{Text: "x"}) // must not panic

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err := b.Approver().Approve(ctx, domain.ApprovalRequest{})
	if got != domain.ApprovalDeny || !errors.Is(err, context.Canceled) {
		t.Errorf("Approve(unbound, cancelled) = (%q, %v); want (deny, context.Canceled)", got, err)
	}
}

// TestSeamConcurrentEmitApproveCancel is the headline acceptance test: drive the full seam
// — the worker stepping the engine, the engine emitting bursts of events and seeking
// approval, independent goroutines hammering Emit, a concurrent rebind, and a user stop —
// all at once, and require it to finish without a deadlock, panic, or data race (run under
// -race). It asserts only that the worker returns a terminal seam Msg; the exact terminal
// (done vs cancelled) depends on whether the stop lands before the boundary, and both are
// valid.
func TestSeamConcurrentEmitApproveCancel(t *testing.T) {
	t.Parallel()
	prog := newStubProgram()
	prog.replyWith(domain.ApprovalAllow) // the "Update loop" auto-approves, asynchronously
	b := NewBridge()
	b.Bind(prog)
	sink := b.Sink()
	approver := b.Approver()

	// Each Step emits a burst of tokens and, on the first Turn, seeks approval — the
	// interleaving the real loop produces — then ends the Exchange (honouring a stop).
	eng := &fakeEngine{
		stepFn: func(ctx context.Context, call int) (domain.StepResult, error) {
			for i := 0; i < 20; i++ {
				sink.Emit(domain.TokenEvent{Text: "x"})
			}
			if call == 0 {
				_, _ = approver.Approve(ctx, domain.ApprovalRequest{Tool: "write_file"})
				if ctx.Err() != nil {
					return domain.StepResult{Status: domain.StatusCancelled}, nil
				}
				return domain.StepResult{Status: domain.StatusTurnComplete}, nil
			}
			sink.Emit(domain.MessageEvent{Text: "done"})
			return domain.StepResult{Status: domain.StatusExchangeComplete}, nil
		},
	}

	cmd, cancel := startExchange(context.Background(), eng, domain.UserInput{Text: "go"})

	var wg sync.WaitGroup
	var workerMsg tea.Msg

	wg.Add(1)
	go func() { defer wg.Done(); workerMsg = cmd() }()

	// Independent producers stress the bridge's atomic send path under -race.
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				sink.Emit(domain.TokenEvent{Text: "y"})
			}
		}()
	}
	// A concurrent rebind exercises programRef.box.Store racing with Load in send.
	wg.Add(1)
	go func() { defer wg.Done(); b.Bind(prog) }()
	// A user stop — may or may not catch the in-flight Approve; both outcomes are valid.
	wg.Add(1)
	go func() { defer wg.Done(); cancel() }()

	wg.Wait()
	prog.wait()

	switch workerMsg.(type) {
	case exchangeDoneMsg, cancelledMsg, errMsg:
	default:
		t.Fatalf("worker returned %T; want a terminal seam Msg", workerMsg)
	}
}
