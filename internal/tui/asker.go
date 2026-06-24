package tui

import (
	"context"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Ask-user rendezvous (P3.11) — the free-text question gate, sibling of uiApprover
// ----------------------------------------------------------------------------

// uiAsker is the cross-goroutine ask-user gate, the free-text sibling of uiApprover. The
// engine calls Ask synchronously inside a Step (on the worker goroutine) when the model
// invokes the ask_user tool; uiAsker hands the question to the single-threaded Update loop
// via the program and blocks on a buffered reply channel until the human types an answer —
// or until ctx is cancelled (a user stop), which unblocks it with an empty answer and
// ctx.Err() so the loop rolls the Turn back (ADR 0007). It is the public Asker analogue of
// the Approval rendezvous (phase-2 §3 C3), reusing the same late-bound program seam.
type uiAsker struct {
	prog *programRef
}

// uiAsker is the engine's Asker.
var _ domain.Asker = (*uiAsker)(nil)

// Ask hands req to the Update loop and waits for the human's typed answer. The reply channel
// is buffered (cap 1) so the Update loop never blocks sending the answer, and so an answer
// arriving after a cancel is absorbed by the buffer rather than parking the UI — no goroutine
// leak. A cancelled ctx returns an empty AskAnswer and ctx.Err(); the engine then rolls the
// Turn back to a quiescent boundary with StatusCancelled. This is fail-safe by construction:
// it never hangs past ctx, so a non-interactive shutdown unblocks it.
func (a *uiAsker) Ask(ctx context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	reply := make(chan domain.AskAnswer, 1) // buffered: the UI never blocks replying
	a.prog.send(askReqMsg{Request: req, Reply: reply})
	select {
	case ans := <-reply:
		return ans, nil
	case <-ctx.Done():
		return domain.AskAnswer{}, ctx.Err()
	}
}
