package tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// TestApproverReturnsUIDecision proves the rendezvous returns whatever the UI replied —
// for every decision — when the reply arrives from another goroutine (the Update loop).
func TestApproverReturnsUIDecision(t *testing.T) {
	t.Parallel()
	for _, want := range []domain.ApprovalDecision{
		domain.ApprovalAllow,
		domain.ApprovalDeny,
		domain.ApprovalAllowForSession,
	} {
		t.Run(string(want), func(t *testing.T) {
			t.Parallel()
			prog := newStubProgram()
			prog.replyWith(want)
			ref := &programRef{}
			ref.bind(prog)
			approver := &uiApprover{prog: ref}

			got, err := approver.Approve(context.Background(),
				domain.ApprovalRequest{Tool: "write_file", Reason: "write"})
			if err != nil {
				t.Fatalf("Approve: unexpected error %v", err)
			}
			if got != want {
				t.Errorf("decision = %q; want %q", got, want)
			}
			prog.wait()
		})
	}
}

// TestApproverCancelledCtxReturnsDenyNoLeak is the headline C3 test: when ctx is cancelled
// (a user stop) and the UI never replies, Approve returns ApprovalDeny + ctx.Err() promptly
// (no deadlock), and the reply channel it handed the UI is buffered so a late reply is
// absorbed rather than parking a goroutine (no leak).
func TestApproverCancelledCtxReturnsDenyNoLeak(t *testing.T) {
	t.Parallel()
	prog := newStubProgram() // no answer hook → the UI never replies
	ref := &programRef{}
	ref.bind(prog)
	approver := &uiApprover{prog: ref}

	ctx, cancel := context.WithCancel(context.Background())

	type outcome struct {
		d   domain.ApprovalDecision
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		d, err := approver.Approve(ctx, domain.ApprovalRequest{Tool: "write_file"})
		done <- outcome{d, err}
	}()

	// The user stops the in-flight Exchange while the human gate is pending.
	cancel()

	select {
	case got := <-done:
		if got.d != domain.ApprovalDeny {
			t.Errorf("decision = %q; want %q", got.d, domain.ApprovalDeny)
		}
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("err = %v; want context.Canceled", got.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Approve did not return after ctx cancel (deadlock)")
	}

	// The request reached the UI exactly once, and its reply channel is buffered: a late
	// reply (the human pressing a key after the stop) must not block — proving no goroutine
	// would leak parked on the send.
	msgs := prog.messages()
	if len(msgs) != 1 {
		t.Fatalf("captured %d msgs; want exactly 1 approvalReqMsg", len(msgs))
	}
	req, ok := msgs[0].(approvalReqMsg)
	if !ok {
		t.Fatalf("captured msg is %T; want approvalReqMsg", msgs[0])
	}
	select {
	case req.Reply <- domain.ApprovalAllow:
	default:
		t.Error("reply channel was not buffered; a late UI reply would block (goroutine leak)")
	}
}

// TestApproverUnboundReturnsOnCancel proves the approver does not hang when no program is
// bound (the no-op send): a cancelled ctx still unblocks it cleanly.
func TestApproverUnboundReturnsOnCancel(t *testing.T) {
	t.Parallel()
	approver := &uiApprover{prog: &programRef{}} // never bound

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := approver.Approve(ctx, domain.ApprovalRequest{})
	if got != domain.ApprovalDeny {
		t.Errorf("decision = %q; want %q", got, domain.ApprovalDeny)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v; want context.Canceled", err)
	}
}
