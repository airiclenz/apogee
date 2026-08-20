package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

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

// Approve hands req to the Update loop and waits for the human's decision, through the one
// rendezvous body both human gates share ([parkCall], parkedcall.go — which is also where the
// buffered-reply and no-leak reasoning lives).
//
// The abandoned verdict this gate chooses is ApprovalDeny, NOT the zero ApprovalDecision: a
// cancelled ctx (the user stopped the in-flight Exchange) unblocks the gate with the safe
// verdict for a request nobody is left to answer. It is the cancellation, not the verdict,
// that ends the Turn (ADR 0007) — the deny simply refuses to let an abandoned request be read
// as an allow.
func (a *uiApprover) Approve(ctx context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	return parkCall(ctx, a.prog,
		func(reply chan domain.ApprovalDecision) tea.Msg {
			return approvalReqMsg{Request: req, Reply: reply}
		},
		domain.ApprovalDeny)
}
