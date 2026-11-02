package agent

import (
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// sharedUpstream is the one holder an Agent reads its Upstream through. A top-level Agent builds
// its own (New, Resume, newAgent), and so do SwitchUpstream and a ROUTED spawn; an unrouted child
// is handed its parent's holder by reference (newChildAgentOn), so a parent and every child
// speaking over the parent's client read the same cell, and an Upstream put into that cell is the
// one each of them streams from at its next request.
//
// It holds the Responder and nothing about who may close it: ownership stays per Agent
// (seatBinding.ownsUpstream), so an unrouted child that shares the holder still never closes the
// session's client. A server switch installs a FRESH holder for the switching Agent rather than
// replacing this one (SwitchUpstream), which leaves a detached reader that copied the old holder —
// a background workflow's host (backgroundHost) — on the client it started with.
//
// generation counts the replacements: it starts at zero and advances by one on each replace or
// rehome, so a reader that noted the generation beside the Responder it sent a request over can tell
// whether the holder has moved on since.
//
// bound is the binding the latest replacement came with: the fallback a Server fallback rehome moved
// the seat onto (rehome), nil after a bare replace and before any. Every Agent on the holder notes
// the generation whose binding its own Config reflects (seatBinding.appliedGen) and re-applies bound on its
// own goroutine when the holder has moved past it (Agent.followUpstream), so the parent and every
// unrouted child speak to the fallback with the fallback's window, model, profile and dialect.
//
// seat names whose Upstream the holder is — the session's, or a routed Sub-agents seat's — which is
// what an UpstreamSwitchedEvent reports as the seat that moved, never the emitting Agent's depth. It
// is set when the holder is built and never written after, so it needs no lock.
//
// retired says a server switch left the holder behind (SwitchUpstream): the session's seat now
// speaks over a fresh holder, and whoever still reads this one — a background workflow's host and
// its children (backgroundHost) — reads a client the human moved the session off. Such a reader
// never moves the seat: a fault on it consults no fallback resolver and abandons, as a background
// workflow did before Server fallback (ADR 0089), and a rehome already in flight when the switch
// landed is refused (rehome). It is guarded by mu, and once set it is never cleared.
//
// Its reads are safe on a nil holder, which answers no Upstream at generation zero: an Agent built
// bare in a test has none, and its Close must still close nothing rather than panic.
type sharedUpstream struct {
	mu         sync.Mutex
	up         provider.Responder
	generation uint64
	bound      *rehomeBinding
	seat       domain.UpstreamSeat
	retired    bool
}

// rehomeOutcome is what a holder did with a Server fallback move it was asked to make (rehome).
type rehomeOutcome int

const (
	rehomeLanded         rehomeOutcome = iota // the holder moved onto the fallback
	rehomeFollows                             // another Agent on the seat moved it first; the caller follows that move
	rehomeRefusedRetired                      // a server switch retired the holder; nothing moves and the caller abandons
)

// rehomeBinding is what a Server fallback rehome moved a seat onto: the fallback's UpstreamSpec and
// the RebindSpec for the model its heartbeat observed, as the FallbackResolver answered them. It is
// never mutated once it is in a holder, so every Agent that reads it reads the same value.
type rehomeBinding struct {
	upstream UpstreamSpec
	rebind   RebindSpec
}

// applyTo projects the binding onto cfg the way a server switch followed by its first Rebind would:
// the switch's cells first (dial facts, the seat's human words, the four token bounds, the model
// unbound), then the per-model ones the rebind states over them (ADR 0024's one code path, run as
// one step). It is pure, like serverBinding.applyTo.
func (b *rehomeBinding) applyTo(cfg domain.Config) domain.Config {
	return b.rebind.binding().applyTo(b.upstream.binding().applyTo(cfg))
}

// newSharedUpstream returns a holder over up at generation zero, on the session's seat.
func newSharedUpstream(up provider.Responder) *sharedUpstream {
	return newSharedUpstreamOn(domain.UpstreamSeatSession, up)
}

// newSharedUpstreamOn returns a holder over up at generation zero, on seat.
func newSharedUpstreamOn(seat domain.UpstreamSeat, up provider.Responder) *sharedUpstream {
	return &sharedUpstream{up: up, seat: seat}
}

// seatOf names the holder's seat; a nil holder is the session's, the seat a bare test Agent is on.
func (s *sharedUpstream) seatOf() domain.UpstreamSeat {
	if s == nil {
		return domain.UpstreamSeatSession
	}
	return s.seat
}

// responder returns the Upstream the holder currently holds.
func (s *sharedUpstream) responder() provider.Responder {
	up, _ := s.current()
	return up
}

// current returns the Upstream the holder currently holds together with its generation, read
// under one lock so the pair always describes the same replacement.
func (s *sharedUpstream) current() (provider.Responder, uint64) {
	if s == nil {
		return nil, 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.up, s.generation
}

// replace puts up into the holder and advances its generation, returning the new one. Every Agent
// that shares the holder reads up from its next request on; a request already streaming keeps the
// Responder it was opened over. The retired Responder is not closed here: whether it may be is the
// owning Agent's call (closeOwnedUpstream).
func (s *sharedUpstream) replace(up provider.Responder) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.up = up
	s.generation++
	s.bound = nil
	return s.generation
}

// rehome moves the holder onto a fallback, once per failed generation: when the holder is still at
// from — the generation the faulted request went out at — it puts the Responder dial returns into
// the holder with b beside it, advances the generation and returns the Responder it retired and
// rehomeLanded. When the holder has already moved past from, another Agent on the seat answered the
// same fault first: nothing is dialled or replaced, and it returns rehomeFollows, leaving the caller
// to follow the move that was made (Agent.followUpstream). When a server switch has retired the
// holder — possibly while the caller's resolver waited — nothing is dialled or replaced either, and
// it returns rehomeRefusedRetired: the human moved the session off this holder, so a reader left on
// it must not move the seat after them. dial runs under the lock, so two Agents reporting the same
// fault at once never dial the fallback twice; a Dialer builds a client and opens no connection, so
// the lock is held for no wire wait.
func (s *sharedUpstream) rehome(from uint64, b *rehomeBinding, dial func() provider.Responder) (provider.Responder, rehomeOutcome) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.retired {
		return nil, rehomeRefusedRetired
	}
	if s.generation != from {
		return nil, rehomeFollows
	}
	retired := s.up
	s.up = dial()
	s.generation++
	s.bound = b
	return retired, rehomeLanded
}

// retire marks the holder as one a server switch left behind (SwitchUpstream). A nil holder has
// nothing to retire.
func (s *sharedUpstream) retire() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retired = true
}

// isRetired reports whether a server switch left the holder behind (retire). A nil holder never was.
func (s *sharedUpstream) isRetired() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.retired
}

// binding returns the holder's generation and the binding its latest replacement came with, read
// under one lock so the pair describes the same replacement. A nil holder answers generation zero
// and no binding.
func (s *sharedUpstream) binding() (uint64, *rehomeBinding) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation, s.bound
}

// replacedSince reports whether the holder has been replaced since generation — whether a request
// sent over the Upstream current() answered at that generation went out on one the holder has
// since moved off. A fault on such a request is the retired Upstream's, not the current one's, so
// the respond phase resends on the current Upstream rather than asking the fallback resolver about
// a server the seat already left (consultFallback).
func (s *sharedUpstream) replacedSince(generation uint64) bool {
	_, current := s.current()
	return current != generation
}

// seatBinding is an Agent's place on a seat, held as ONE value so the four facts that make it up
// move together: the holder it reads its Upstream through, the Server fallback resolver it consults
// when that Upstream faults (consultFallback, loop.go; nil consults none), the holder generation whose
// binding its Config reflects, and whether it dialled the client in the holder. Every place an Agent
// is built or switched assigns it whole, from one of the three constructors below, so no door can
// move the holder and forget the resolver or the generation beside it.
//
// The seat holds STATE, not a shared Responder: a routed child is on a holder of its own and keeps
// its own client and Inspector tap (wireTap), and the retired flag a server switch sets lives on
// the holder (sharedUpstream.retire), which every Agent that copied it reads.
type seatBinding struct {
	// upstream is the holder (sharedupstream.go): an Agent's own for a top-level Agent and a routed
	// child, shared by HANDLE with every unrouted child and a background workflow's host.
	upstream *sharedUpstream
	// fallback is the resolver the respond phase consults before it abandons a Turn for a fault
	// that is the upstream's own (consultFallback, loop.go); nil — every Agent no host handed one —
	// consults nothing and abandons exactly as before it existed. It is written only where the seat
	// is assigned and only read after, so it needs no lock.
	fallback FallbackResolver
	// appliedGen is the holder generation whose binding this Agent's Config reflects: the holder's
	// binding is re-applied, on this Agent's own goroutine, when the holder has moved past it
	// (followUpstream). Only the goroutine driving this Agent reads or writes it.
	appliedGen uint64
	// ownsUpstream says whether THIS Agent dialled the client in upstream and is therefore the one
	// allowed to tear it down. An unrouted child, a background host and the fake Responder a test
	// injects through newAgent own nothing. Close and SwitchUpstream consult it before closing, so a
	// shared client outlives every child that borrowed it and is closed exactly once — by its owner.
	ownsUpstream bool
}

// ownedSeat is the seat of an Agent that dialled the client in up itself — New and Resume for the
// session, a ROUTED spawn for the Sub-agents seat (the target's own resolver,
// DelegationTarget.Fallback, never the session's): it owns the client, consults fallback, and
// starts at generation zero, which is where a fresh holder is.
func ownedSeat(up *sharedUpstream, fallback FallbackResolver) seatBinding {
	return seatBinding{upstream: up, fallback: fallback, ownsUpstream: true}
}

// inherit is the seat of an Agent that sits on s's seat without dialling anything — an unrouted
// child, or a background workflow's host: the same holder and resolver, so a fault on it falls over
// with the seat (ADR 0105), and the generation s has APPLIED, never the holder's current one, since
// the inheritor starts from a Config that reflects exactly that generation — a move s has not
// followed yet is followed at the inheritor's first Step. It owns nothing, so it never closes the
// client out from under the Agent still speaking over it.
func (s seatBinding) inherit() seatBinding {
	return seatBinding{upstream: s.upstream, fallback: s.fallback, appliedGen: s.appliedGen}
}

// switched is the seat a server switch (SwitchUpstream) moves s onto: up, a fresh holder this Agent
// dialled, owned and at generation zero — and s's resolver KEPT, since a switch moves the seat to
// another server and never takes away the fallover the host armed for it.
func (s seatBinding) switched(up *sharedUpstream) seatBinding {
	return ownedSeat(up, s.fallback)
}
