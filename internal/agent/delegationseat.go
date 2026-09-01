package agent

// The Delegation SEAT facts (ADR 0069). A model offered `run_on` is choosing between two places to
// put a sub-task, and a choice between two opaque labels is a coin toss: it needs to know what each
// seat IS — which model sits there, on which server, described how — before "send the mechanical
// half to the other one" is a decision rather than a guess. This file holds what the engine is told
// about the FAR seat, the Sub-agent server; the session seat is described by the Config fields the
// Upstream already carries (domain.Config.ServerName / ServerDescription).
//
// It is deliberately NOT the Delegation target (delegationtarget.go), and the split is the point.
// The target is a DIAL fact re-stated by the host's second heartbeat on every beat, so it moves
// while the session runs and goes nil the moment the far server stops answering. These are DISPLAY
// facts, moved only where the human moves them (`/sub-agents-server`), which is what lets the
// orientation block name the seat without becoming a per-beat availability report — the rendered
// block stays a per-session constant and the prefix cache with it (ADR 0023 §6). A target that is
// momentarily unusable is reported to the parent by the delegation's own result note (subagent.go),
// never by the standing prompt.
//
// The engine stays wire-silent (ADR 0031): it never learns that a `servers:` list exists or which
// entry the key names. It is handed three strings and renders them.

// DelegationSeat is the Sub-agent server as the MODEL is told about it: the host's own words for
// the far Delegation seat, resolved whole by the composition root from the flagged `servers:` entry
// and installed through SetDelegationSeat. Every field is optional and independently so — an entry
// with no description describes nothing, one with no `model:` pin names no model — and a seat that
// renders nothing at all is the same as no seat installed.
//
// It is treated as IMMUTABLE once installed: the host builds a fresh value when the human moves the
// key and installs that, so a render taken mid-Turn stays valid for the request it was taken for.
type DelegationSeat struct {
	// Name is the `servers:` entry the `sub-agents-server:` key names — the human's own label for
	// the box, which is the only handle the model and the human share when they talk about where a
	// delegation ran.
	Name string
	// Description is that entry's free-text `description:`, verbatim (ADR 0069). It exists because
	// nothing else in the seat says what the box is FOR: "fast local 27B — search and edits" is what
	// turns the seat choice into a routing judgment the model can actually make.
	Description string
	// Model is the entry's `model:` PIN and nothing else — never the model a heartbeat observed
	// bound there. A pin is a per-session constant the human wrote down; an observation moves when
	// the far server loads something else, and a standing prompt that followed it would re-write the
	// system message mid-session for a fact the model cannot act on. Empty means the entry pins
	// none, and the rendered line names the seat without naming a model.
	Model string
}

// SetDelegationSeat installs what the orientation block tells the model about the Sub-agent server,
// or clears it with nil so the block names only the session seat (ADR 0069). The host resolves the
// value whole from the flagged entry and pushes it where the human moves the key — at startup, and
// on every `/sub-agents-server` switch — so the engine reads no config of its own (ADR 0031).
//
// It is goroutine-safe and never idle-gated, like SetDelegationTarget beside it: it changes nothing
// about a running Step, only what the NEXT request renders. But unlike that latch it is NOT the
// heartbeat's to write — a beat that finds the far server down must leave these facts standing, or
// the standing system message would churn per beat and cost the prefix cache the very stability
// ADR 0023 §6 promises. Availability is the result note's story, not the prompt's.
//
// The seat is offered at depth 0 only (ADR 0069 decision 3), so this is a top-level Agent's door:
// a child renders no Delegations line and is handed no seat of its own.
func (a *Agent) SetDelegationSeat(seat *DelegationSeat) {
	a.seatMu.Lock()
	a.seat = seat
	a.seatMu.Unlock()
}

// subAgentsSeat snapshots the installed Sub-agent-server facts for one render, nil meaning none is
// installed — the key is unset, or names an entry there is nothing to say about. It is the ONE read
// seam for the field, so the orientation block is race-free against a `/sub-agents-server` switch
// landing beside it. The value behind the pointer is never mutated in place, so the caller may read
// it after the lock is dropped.
func (a *Agent) subAgentsSeat() *DelegationSeat {
	a.seatMu.RLock()
	defer a.seatMu.RUnlock()
	return a.seat
}
