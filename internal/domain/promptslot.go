package domain

import "context"

// ----------------------------------------------------------------------------
// The prompt slot (ADR 0039 decision 12) — ONE prompt in front of the human, of ANY kind
// ----------------------------------------------------------------------------

// PromptSlot is the one prompt surface a Driver has, modelled as a single slot the engine hands to
// one asking Agent at a time. A Driver draws ONE prompt: the TUI's approval pane and its ask_user
// box are the same real estate and the same keyboard, so whichever arrives second REPLACES the
// first and orphans the reply channel the first was waiting on — the child behind it then blocks
// until the Turn is cancelled.
//
// Once a depth-0 fan-out is running (ADR 0039) several children can reach a human gate at the same
// instant, and they do not all reach the same KIND of gate: one may want a tool approved while
// another puts a question to the person. A queue per kind would still let those two collide, so
// this slot is kind-BLIND — an Approval (domain.Approver) and an ask_user question (domain.Asker)
// contend for exactly one, and "one prompt at a time" is true across both. It is the shared half of
// two seams that live in different packages: the engine's Approval queue (internal/agent) and the
// ask_user tool's question queue (internal/tools) both hold THIS type, which is why it lives in
// domain — the one package both already depend on.
//
// The wait is a channel rather than a mutex because it has to be ctx-AWARE. A sibling queued behind
// the visible prompt must be able to give up when the human cancels the Turn; under a mutex it
// could not — it would sit in Lock until the prompt ahead of it resolved and only THEN hand the
// Driver a request belonging to a Turn that has already rolled back.
//
// The slot is held for the whole of the host's call — the human's entire deliberation — which is the
// point: the queue behind it is what "one prompt at a time" means, and the wait-tolerance invariant
// (ADR 0031) is what makes an unbounded hold legitimate. Which waiter is admitted next is
// deliberately unspecified; what is guaranteed is that exactly one is in front of the human at a
// time and that every other child goes on working while it waits.
//
// A PromptSlot is safe for concurrent use by construction — the channel IS the synchronisation —
// and the zero value is NOT usable: build one with NewPromptSlot.
type PromptSlot struct {
	slot chan struct{} // capacity 1: the prompt currently in front of the human
}

// NewPromptSlot returns an empty prompt slot — nobody is in front of the human yet.
func NewPromptSlot() *PromptSlot {
	return &PromptSlot{slot: make(chan struct{}, 1)}
}

// Acquire blocks until this caller owns the prompt surface, then returns nil; a cancelled ctx
// abandons the wait and returns ctx.Err() instead, WITHOUT the caller's request ever reaching the
// Driver. Every nil return must be paired with exactly one Release (the callers `defer` it); a
// non-nil return acquired nothing and must not be released.
func (p *PromptSlot) Acquire(ctx context.Context) error {
	select {
	case p.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release gives the surface up to whoever is waiting. It is valid only after an Acquire that
// returned nil — releasing a slot this caller does not hold would block until somebody else took it,
// which is why the pairing is a `defer` at every call site rather than a rule to remember.
func (p *PromptSlot) Release() {
	<-p.slot
}

// promptSlotCtxKey keys the designated prompt surface in a running Agent's context.
type promptSlotCtxKey struct{}

// WithPromptSlot designates slot as THE prompt surface every human gate reached under ctx queues
// on — unless ctx already designates one, in which case it is returned unchanged. That idempotence
// is the tree rule: a sub-agent runs under a context derived from its parent's, so the outermost
// Agent's slot is the one the whole tree queues on, however deep or wide it grows, and a nested
// Agent never opens a private, uncontended surface of its own.
//
// The context is the carrier because the two seams that hold the slot are separated by an API
// neither owns: the loop consults the Approver itself, while a QUESTION is raised by the ask_user
// TOOL — one interface boundary away, in a registry the HOST may have built (cmd/apogee builds its
// own to register MCP tools on top). A slot installed at construction would be dead in exactly that
// Driver; one installed on the call's context reaches every tool in every registry. It is the same
// seam WithConfinement uses to hand a subprocess tool its box, and WithSubAgentTask to name the
// asking child.
func WithPromptSlot(ctx context.Context, slot *PromptSlot) context.Context {
	if slot == nil || PromptSlotFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, promptSlotCtxKey{}, slot)
}

// PromptSlotFromContext returns the prompt surface designated by WithPromptSlot, or nil when the
// call is not running under one — a seam used outside a running Agent (a Driver embedding the
// engine differently, a unit test) rather than a missing installation.
func PromptSlotFromContext(ctx context.Context) *PromptSlot {
	slot, _ := ctx.Value(promptSlotCtxKey{}).(*PromptSlot)
	return slot
}

// PromptSlotFor returns the surface ctx designates, falling back to own when it designates none.
// It is what makes the two queueing seams share ONE slot inside a running Agent while each stays a
// working queue on its own: own is the seam's private surface, correct when nothing above it has
// designated a shared one.
func PromptSlotFor(ctx context.Context, own *PromptSlot) *PromptSlot {
	if slot := PromptSlotFromContext(ctx); slot != nil {
		return slot
	}
	return own
}
