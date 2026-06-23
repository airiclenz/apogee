package tui

import (
	"context"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Approval rendezvous (phase-2 detail plan §3 C3)
// ----------------------------------------------------------------------------

// uiApprover is the cross-goroutine Approval gate. The engine calls Approve synchronously
// inside a Step (on the worker goroutine) and may block on the human; uiApprover hands the
// request to the single-threaded Update loop via the program and blocks on a buffered reply
// channel until the human decides — or until ctx is cancelled, which a user stop does. This
// is the single most race-prone piece of the seam (it carries the heaviest test).
type uiApprover struct {
	prog *programRef
}

// uiApprover is the engine's Approver.
var _ domain.Approver = (*uiApprover)(nil)

// Approve hands req to the Update loop and waits for the human's decision. The reply channel
// is buffered (cap 1) so the Update loop never blocks sending the decision, and so a reply
// that arrives *after* a cancel is absorbed by the buffer rather than parking the UI — no
// goroutine leak. A cancelled ctx (the user stopped the in-flight Exchange) unblocks this
// human gate and returns ApprovalDeny with ctx.Err(); the engine then rolls the Turn back to
// a quiescent boundary with StatusCancelled (ADR 0007). The deny is the safe verdict for an
// abandoned request — but the cancellation, not the verdict, is what ends the Turn.
func (a *uiApprover) Approve(ctx context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	reply := make(chan domain.ApprovalDecision, 1) // buffered: the UI never blocks replying
	a.prog.send(approvalReqMsg{Request: req, Reply: reply})
	select {
	case d := <-reply:
		return d, nil
	case <-ctx.Done():
		return domain.ApprovalDeny, ctx.Err()
	}
}
