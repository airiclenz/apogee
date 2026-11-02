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
// (Agent.ownsUpstream), so an unrouted child that shares the holder still never closes the
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
// the generation whose binding its own Config reflects (Agent.appliedGen) and re-applies bound on its
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
