package agent

import (
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Child addressing (ADR 0063) — a running sub-agent is reachable by its spawn call-ID
// ----------------------------------------------------------------------------
//
// A delegation is opaque to everything outside the engine: runSubAgent drives the child to its
// boundary inside the parent's Turn (ADR 0013 D5) and nobody else holds the child. The two types
// here are the whole seam that makes a RUNNING child addressable anyway — a registry the parent
// publishes its live children in, and a mailbox each child drains at its own between-Steps
// boundaries. Together they let a Driver say "this message is for that sub-agent" with nothing
// but the id it already paints the delegation by, and they add no goroutine: the child's own
// Step-driving loop does the delivering.

// childRegistry is the set of sub-agents ONE Agent currently has running, keyed by the id of the
// sub_agent call that spawned each — the same id the child stamps on every Event it emits, so a
// caller addresses a child by the identity it already sees.
//
// Membership is exactly the child's run: runSubAgent registers before it drives the child and
// unregisters in the defer that closes it, so a lookup that succeeds names a child that was
// running at the moment of the lookup. It is guarded because the depth-0 fan-out registers and
// unregisters from several pool workers at once, while lookups arrive from the host's goroutine.
//
// The zero value is ready to use.
type childRegistry struct {
	mu       sync.Mutex
	byCallID map[string]*Agent
}

// register publishes child under its spawn call-ID. Registering the same id twice replaces the
// entry: ids are the model's to choose and two calls of one Turn can collide (ADR 0059 §6), so
// last-in wins rather than the pair silently sharing a mailbox.
func (r *childRegistry) register(spawnCallID string, child *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byCallID == nil {
		r.byCallID = make(map[string]*Agent, 1)
	}
	r.byCallID[spawnCallID] = child
}

// unregister removes the entry for spawnCallID. It is a no-op for an id that is not registered,
// so the defer that calls it is safe on every early return runSubAgent takes before registering.
func (r *childRegistry) unregister(spawnCallID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.byCallID, spawnCallID)
}

// lookup returns the running child registered under spawnCallID.
func (r *childRegistry) lookup(spawnCallID string) (*Agent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	child, ok := r.byCallID[spawnCallID]
	return child, ok
}

// all returns a snapshot of the running children, in unspecified order. It is a copy so the
// caller can recurse into each child without holding this registry's lock — which it must not,
// because a grandchild's registry is locked one level down.
func (r *childRegistry) all() []*Agent {
	r.mu.Lock()
	defer r.mu.Unlock()
	children := make([]*Agent, 0, len(r.byCallID))
	for _, child := range r.byCallID {
		children = append(children, child)
	}
	return children
}

// childMailbox holds the user messages queued for ONE agent while it runs as somebody's child,
// in the order they were queued. It is the handover between the goroutine a message is typed on
// and the goroutine driving the child's Steps: adding is non-blocking and safe from anywhere,
// draining happens only on the driving goroutine, at a between-Steps boundary where Interject is
// legal (ADR 0025's caller rule).
//
// It closes exactly once, when the child's run ends, and refuses everything after: a message that
// cannot be delivered must be refused at the door rather than accepted into a mailbox nothing will
// ever drain, because every accepted message owes its sender a ChildInterjectionEvent.
//
// The zero value is ready to use.
type childMailbox struct {
	mu     sync.Mutex
	queued []domain.UserInput
	closed bool
}

// add queues in and reports whether the mailbox accepted it. A closed mailbox accepts nothing.
func (m *childMailbox) add(in domain.UserInput) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	m.queued = append(m.queued, in)
	return true
}

// drain takes everything queued so far, leaving the mailbox open for more.
func (m *childMailbox) drain() []domain.UserInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	queued := m.queued
	m.queued = nil
	return queued
}

// close takes everything still queued and refuses every later add. The returned messages never
// reached the model and are the caller's to account for.
func (m *childMailbox) close() []domain.UserInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	queued := m.queued
	m.queued = nil
	return queued
}

// InterjectChild queues a user message for the RUNNING sub-agent spawned by spawnCallID, anywhere
// in this Agent's tree: its own children first, then — recursively — theirs, so a host holding
// only the top-level Agent reaches a grandchild at depth 2. The message lands at that child's next
// between-Steps boundary as an ordinary interjection (Agent.Interject), with the child's own tool
// set, mode and confinement unchanged: addressing a child grants it nothing (ADR 0005, ADR 0063 D6).
//
// Contract: non-blocking and safe from ANY goroutine — it only appends to a guarded mailbox and
// never touches the child's conversation. It is the second engine call legal from an interactive
// host's own goroutine while the loop runs, beside AbortExchange; the delivery it schedules is
// performed by the goroutine that owns the child's Steps.
//
// It returns domain.ErrNoSuchChild when spawnCallID names no running sub-agent — the child
// finished, was cancelled, or never existed — and that refusal is the message's whole account:
// nothing was queued, so no ChildInterjectionEvent follows. On success exactly one
// ChildInterjectionEvent will report the message's fate, Landed either way.
func (a *Agent) InterjectChild(spawnCallID string, in domain.UserInput) error {
	if spawnCallID == "" {
		return domain.ErrNoSuchChild
	}
	if child, ok := a.children.lookup(spawnCallID); ok {
		if child.mailbox.add(in) {
			return nil
		}
		// Registered but already closing: the child ended between the lookup and the add, so it
		// is no more addressable than one that was never there.
		return domain.ErrNoSuchChild
	}
	for _, child := range a.children.all() {
		if err := child.InterjectChild(spawnCallID, in); err == nil {
			return nil
		}
	}
	return domain.ErrNoSuchChild
}

// drainMailbox commits everything queued for this child into its open Exchange, in queue order,
// and reports each message's fate. It is called by Run at a between-Steps boundary it is about to
// step past — the one place Interject's caller rule is satisfied without a host driving Step —
// and only for a CHILD: a top-level Run drains nothing and emits no ChildInterjectionEvent,
// because a top-level interjection stays the host's own call between the Steps it drives
// (ADR 0025; ADR 0063 D1 supersedes that rejection for depth > 0 only).
//
// turn is the Turn the messages are about to reach, which is what the events report.
func (a *Agent) drainMailbox(turn int) {
	if a.depth == 0 {
		return
	}
	queued := a.mailbox.drain()
	for i, in := range queued {
		if err := a.Interject(in); err != nil {
			// The first refusal STOPS the drain, exactly as the TUI's own delivery does
			// (deliverInterjections): an Interject error is a statement about the Exchange, not
			// about that one message, so pressing on would produce more of the same and deliver
			// the human's remarks out of order.
			a.reportUndelivered(turn, queued[i:])
			return
		}
		// Counted here and nowhere else: what LANDED is what the parent is told about when this
		// child's result comes back (runSubAgent's trailer), so a refused or undelivered message
		// never inflates the count.
		a.steered++
		a.cfg.Events.Emit(domain.ChildInterjectionEvent{EventBase: a.base(turn), Input: in, Landed: true})
	}
}

// reportUndelivered emits the Landed:false half of the delivery contract for messages that never
// reached the model — the tail of a refused drain, and whatever the mailbox still held when the
// child's run ended. Every accepted message is accounted for exactly once, so a Driver never has
// to guess what became of one it painted as queued.
func (a *Agent) reportUndelivered(turn int, queued []domain.UserInput) {
	for _, in := range queued {
		a.cfg.Events.Emit(domain.ChildInterjectionEvent{EventBase: a.base(turn), Input: in, Landed: false})
	}
}
