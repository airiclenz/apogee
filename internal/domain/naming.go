package domain

import "context"

// ----------------------------------------------------------------------------
// Delegation naming (ADR 0068 — an unnamed delegation is named out of band)
// ----------------------------------------------------------------------------

// DelegationNaming is everything the engine knows about a delegation that needs a name, and
// therefore everything a DelegationNamer is given. It is deliberately thin: the engine holds the
// delegated task and the child's routing, and nothing else about naming is the engine's business —
// which endpoint answers, which model, which prompt and which sanitiser all live with the host that
// implements the namer (ADR 0031, wire-silent engine).
type DelegationNaming struct {
	// Task is the delegated task exactly as the spawning sub_agent call stated it — the same text
	// the child receives as its brief, and the only material there is to name the run from. It is
	// untrusted model text; a namer that renders or logs it treats it as such.
	Task string

	// Routed reports whether this child runs on the Sub-agent server rather than the session's own
	// (ADR 0066). It is here because the naming completion goes to the CHILD's Upstream (ADR 0068
	// decision 2): the machine already warm for this run answers the call about this run, so a
	// session that routes its grunt work to a cheap box does not hand the smart box one extra call
	// per delegation. The host reads it to pick which Upstream to build the call on.
	Routed bool
}

// DelegationNamer is the host-supplied namer for a delegation whose sub_agent call carried no name
// of its own (ADR 0068). It is the Approver precedent one level down: the engine states what it
// knows, the host runs whatever completion it likes on the child's Upstream, and a name or an error
// comes back. The engine builds no request, holds no client, reads no config key and touches no
// endpoint.
//
// It is called CONCURRENTLY with the child it names and bounded by that child's lifetime: ctx is
// cancelled when the run ends, and a name that arrives after the run has finished is dropped rather
// than applied, because a finished run has already been read under the name it wore. An
// implementation may therefore block for as long as its ctx lives, and must not assume it is called
// one at a time — a depth-0 fan-out names its members at once, unlike Approver, whose requests the
// engine queues onto a single PromptSlot.
//
// Every failure is silent by contract (ADR 0068 decision 8): an error, an empty name, or a name
// that arrives too late leaves the run wearing the delegated task's first line, which is the
// behaviour that shipped before naming existed. A namer therefore never has to distinguish "could
// not reach the server" from "nothing usable came back" — both are an error, and both read the
// same to the human.
//
// A nil Config.Namer means naming never fires: the bench, an embedder and every existing test
// compose exactly as they did before this seam existed.
type DelegationNamer interface {
	NameDelegation(ctx context.Context, req DelegationNaming) (string, error)
}
