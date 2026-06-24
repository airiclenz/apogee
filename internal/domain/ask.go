package domain

import "context"

// ----------------------------------------------------------------------------
// Ask-user (P3.11) — the free-text host question delegate, distinct from Approver
// ----------------------------------------------------------------------------

// Asker is the host-supplied delegate the ask_user tool routes a free-text question to
// mid-task: the model asks the human a clarifying question and waits for a typed answer.
// It is the public analogue of Approver (a deliberate v1-surface addition, D7) but for
// free-text Q&A, NOT a safety gate — an Asker decision carries no allow/deny semantics and
// never bypasses the Approval/disposition machinery. It is consulted synchronously inside a
// Step (on the worker goroutine) and may block on the human; cancelling ctx unblocks it.
//
// In a headless / non-interactive context the host must supply an Asker that FAILS SAFE
// (returns promptly with an error or a scripted answer) rather than hanging — and a nil
// Asker means the ask_user tool is simply not registered (graceful), so the model is never
// offered a question it cannot have answered.
type Asker interface {
	Ask(ctx context.Context, req AskRequest) (AskAnswer, error)
}

// AskRequest is the free-text question put to the human. It is a STRUCT (not a bare string)
// for freeze-safety (D7): a post-v1 multiple-choice field (Choices) is then an additive,
// non-breaking change to the v1.0.0 surface.
type AskRequest struct {
	// Question is the free-text prompt the human answers.
	Question string
}

// AskAnswer is the human's free-text reply. A STRUCT for the same freeze-safety reason
// (a post-v1 Choice index is an additive field).
type AskAnswer struct {
	// Text is the human's typed answer.
	Text string
}
