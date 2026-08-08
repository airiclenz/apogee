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
//
// An Asker NEVER has to be safe for concurrent use, exactly as Approver never has to be: the
// engine QUEUES questions on its side, so an implementation sees one Ask at a time even while a
// depth-0 sub-agent fan-out has several children running (ADR 0039 decision 12). That queue is
// what makes "one question on the screen" true without every Driver building a queue of its own —
// the asking child blocks on its turn while its siblings keep working, and the host's
// wait-tolerance (ADR 0031) is what lets a queued question wait as long as the human takes.
//
// It is the SAME queue the Approval gate uses, not a second one beside it: a question and an
// Approval contend for a single PromptSlot, so an Asker is never called while the host still owes
// a verdict on an Approve, or the reverse. A Driver with one prompt surface — which is what a
// terminal has — can therefore serve both from it.
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

	// SubAgentTask is the delegated task of the SUB-AGENT that asked — the answer to "which agent
	// is asking", which stops being obvious the moment several children run at once and their
	// questions queue one behind another (ADR 0039). It is the twin of ApprovalRequest.SubAgentTask
	// and carries the same text: the task from the spawning sub_agent call, so a nested delegation
	// names its OWN task rather than its parent's.
	//
	// It is empty when the top-level agent asks: a session that never delegates carries no extra
	// fact, and its question reads exactly as it always has. Unlike the Approval one — which the
	// loop stamps on a request it builds itself — this field is filled from the ctx the tool call
	// runs under (WithSubAgentTask), because the ask_user TOOL is what builds an AskRequest.
	SubAgentTask string
}

// subAgentTaskCtxKey keys the asking Agent's delegated task in a tool call's context.
type subAgentTaskCtxKey struct{}

// WithSubAgentTask returns a context carrying task as the delegated task of the sub-agent whose
// tool call runs under it, so a host-delegate tool can name the agent that invoked it
// (AskRequest.SubAgentTask). It is the identity carrier the Approval path does not need: the loop
// builds an ApprovalRequest itself and stamps the task straight onto it, while an AskRequest is
// built by the ask_user tool, one interface boundary away from the Agent that knows the task.
// Request-scoped identity crossing an API the caller does not own is what a context value is for —
// the same seam WithConfinement uses to hand a subprocess tool its box.
//
// The engine installs it per tool call, so a nested delegation overwrites its parent's value with
// its own (a sub_agent task is non-empty by construction) and a top-level agent installs nothing.
func WithSubAgentTask(ctx context.Context, task string) context.Context {
	return context.WithValue(ctx, subAgentTaskCtxKey{}, task)
}

// SubAgentTaskFromContext returns the delegated task installed by WithSubAgentTask, or "" when the
// call belongs to the top-level agent — the same emptiness that means "nobody but the top-level
// agent could be asking" on the request itself, so a caller needs no second signal.
func SubAgentTaskFromContext(ctx context.Context) string {
	task, _ := ctx.Value(subAgentTaskCtxKey{}).(string)
	return task
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
