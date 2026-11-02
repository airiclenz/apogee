package agent

import (
	"sync"

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
// generation counts the replacements: it starts at zero and advances by one on each replace, so a
// reader that noted the generation beside the Responder it sent a request over can tell whether
// the holder has moved on since.
//
// Its reads are safe on a nil holder, which answers no Upstream at generation zero: an Agent built
// bare in a test has none, and its Close must still close nothing rather than panic.
type sharedUpstream struct {
	mu         sync.Mutex
	up         provider.Responder
	generation uint64
}

// newSharedUpstream returns a holder over up at generation zero.
func newSharedUpstream(up provider.Responder) *sharedUpstream {
	return &sharedUpstream{up: up}
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
	return s.generation
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
