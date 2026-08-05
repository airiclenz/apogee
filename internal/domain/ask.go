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

// AskRequest is the question put to the human: a free-text prompt, optionally accompanied by a
// short closed set of pickable answers. It is a STRUCT (not a bare string) for freeze-safety
// (D7); Choices landed post-v1.0.0 as exactly the additive, non-breaking field that
// freeze-safety anticipated, and a future refinement (say, a default-choice hint) stays an
// additive change to this surface.
type AskRequest struct {
	// Question is the free-text prompt the human answers.
	Question string

	// Choices is the optional set of answers offered to the human alongside the free-text box —
	// a word or a whole sentence apiece, since the Driver's prompt wraps what it cannot fit on one
	// line; nil/empty means free-text only (the original behaviour). The human may
	// always type a custom answer instead, so Choices never gates the reply, and the chosen
	// label travels back in AskAnswer.Text (D9) — no Choice index is returned.
	Choices []string

	// MultiSelect opts the question into several-of-the-above answering: the Driver lets the
	// human check any number of Choices and returns every chosen label, each on its own line
	// in AskAnswer.Text (D9 stands — labels, never indices). It is exactly the additive,
	// non-breaking refinement this struct's freeze-safety anticipated: false (the zero value,
	// and the absent-on-the-wire case) is the unchanged single-answer question, so no existing
	// caller, Driver, or reply changes shape. Meaningless without Choices — a multi-select
	// request with none simply leaves the human nothing to check, which is not an error.
	MultiSelect bool
}

// AskAnswer is the human's free-text reply. A STRUCT for the same freeze-safety reason
// (a post-v1 Choice index is an additive field).
type AskAnswer struct {
	// Text is the human's typed answer, or the label they picked from Choices (D9). When the
	// request was MultiSelect, it carries every chosen label — each on its own line, in the
	// order the Choices were offered — still as labels, never indices; a single chosen label
	// is therefore byte-identical to a single-select reply.
	Text string
}
