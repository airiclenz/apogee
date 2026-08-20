package tui

import (
	"context"

	tea "charm.land/bubbletea/v2"

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

// Ask hands req to the Update loop and waits for the human's typed answer, through the one
// rendezvous body both human gates share ([parkCall], parkedcall.go — which is also where the
// buffered-reply and no-leak reasoning lives).
//
// The abandoned answer this gate chooses is the empty AskAnswer, which is the zero value here
// rather than a named verdict: a question has no safe default to substitute, so an abandoned
// one yields no text at all. The engine rolls the Turn back to a quiescent boundary with
// StatusCancelled; fail-safe by construction, since the gate never hangs past ctx and a
// non-interactive shutdown therefore unblocks it.
func (a *uiAsker) Ask(ctx context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	return parkCall(ctx, a.prog,
		func(reply chan domain.AskAnswer) tea.Msg {
			return askReqMsg{Request: req, Reply: reply}
		},
		domain.AskAnswer{})
}
