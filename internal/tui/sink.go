package tui

import (
	"sync"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Event→Msg bridge (phase-2 detail plan §3 C2)
// ----------------------------------------------------------------------------

// teaSink is the EventSink the engine emits through. Emit wraps Events in eventMsgs and hands
// them to the running program via Send — Bubble Tea's goroutine-safe, async-to-Update enqueue —
// which is exactly the mechanism the EventSink contract intends and satisfies "Emit must not
// block the loop for long" (Send is async, so the Step goroutine never blocks here).
//
// Delivery is lossless: no Event's content is ever dropped or reordered — the correctness floor
// the bench-side ordering and the TUI both want. It is not, however, one-Msg-per-Event: adjacent
// TokenEvents sharing an EventBase are coalesced — their text accumulates for a short window
// (tokenCoalesceWindow) and lands as a single TokenEvent — which is the option phase-2 detail
// plan §3 C2 held in reserve, taken because a provider emits one delta per visible byte-run
// (internal/agent/loop.go) and every Msg costs the TUI a transcript render. Every other variant
// flushes the open buffer ahead of itself, so what the Update loop sees is exactly the order the
// engine emitted; flush() closes the window at the Step boundary, so a stream never spills past
// the Step that produced it.
type teaSink struct {
	prog *programRef

	// mu guards the coalescing buffer AND covers every send made out of it, so the Emit
	// goroutine and the timer goroutine can never interleave halfway through a delivery:
	// whatever order two goroutines take the lock in is the order the Update loop sees.
	mu sync.Mutex
	// pending is the accumulated TokenEvent text — a plain string, never a strings.Builder,
	// per this package's no-copy-type hygiene (ADR 0011, doc.go).
	pending string
	// base is the (Depth, Turn, run identity) the pending text belongs to. Only tokens sharing
	// all three may merge: a sub-agent's stream (Depth > 0) nests inside the parent's and is a
	// different block in the transcript, a Turn boundary is a commit point, and two children of
	// one reply share a depth but not a spawning call id (domain.EventBase.CallID), so the id is
	// what keeps concurrent siblings' text from merging into one another's block (ADR 0039).
	base domain.EventBase
	// buffering says a buffer is open, which an empty pending string cannot: a zero-length
	// token must still be delivered rather than silently swallowed.
	buffering bool
	// gen identifies the open buffer, so a window timer that lost the race to the lock
	// (its buffer was already flushed by an Emit) returns instead of cutting the NEXT
	// buffer's window short.
	gen uint64
	// timer closes the current window. It is armed by the token that opens a buffer and
	// never re-armed while that buffer grows, so a continuous stream still flushes once
	// per window rather than sliding forever.
	timer *time.Timer
	// window overrides tokenCoalesceWindow; zero means the default. Tests shrink it.
	window time.Duration
}

// tokenCoalesceWindow is how long adjacent tokens may accumulate before the buffer is
// delivered: about two frames at 60 fps — imperceptible as latency, and it caps
// token-driven repaints near ~33/s no matter how fast the provider streams.
const tokenCoalesceWindow = 30 * time.Millisecond

// teaSink is the engine's EventSink.
var _ domain.EventSink = (*teaSink)(nil)

// Emit forwards e to the Update loop as an eventMsg. It is called synchronously on the Step
// goroutine, in Turn order; the async Send keeps the loop moving.
//
// Adjacent TokenEvents are coalesced (see the type doc): a token joins the open buffer,
// anything else delivers that buffer first and then itself. The flush-before is not a
// nicety — every other variant depends on the tokens that preceded it: StreamResetEvent
// discards them, MessageEvent/ToolCallEvent commit them as narration, and UsageEvent times
// the generation the first token started.
func (s *teaSink) Emit(e domain.Event) {
	if tok, ok := e.(domain.TokenEvent); ok {
		s.emitToken(tok)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	s.prog.send(eventMsg{Event: e})
}

// emitToken merges tok into the open buffer, or opens a new one when nothing is buffered or
// the stream moved to another (Depth, Turn, run identity). Nothing is delivered here unless a boundary
// forces it: the window timer does the delivering.
func (s *teaSink) emitToken(tok domain.TokenEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buffering && s.base != tok.EventBase {
		s.flushLocked()
	}
	if !s.buffering {
		s.buffering = true
		s.base = tok.EventBase
		s.gen++
		gen := s.gen
		s.timer = time.AfterFunc(s.coalesceWindow(), func() { s.closeWindow(gen) })
	}
	s.pending += tok.Text
}

// flush delivers whatever is buffered right now. It is the Step boundary's flush: the worker
// calls it the instant a Step returns (worker.go), which is what keeps the coalescing window a
// WITHIN-Step affair and the "the sink delivered this Step's Events before the Step returned"
// invariant the worker and the Model both rest on (worker.go, messages.go) true.
//
// Nothing else could stand in for it on the path that matters. A cancelled Turn returns from the
// loop emitting no further event (internal/agent/loop.go), so no boundary event would flush the
// tail of a stream Esc interrupted: the buffer would ride the window timer instead and land after
// the Model folded cancelledMsg — after finishWorker zeroed the generation clock (model.go),
// re-latching it, so the NEXT turn's tok/s would be timed across the human's idle time.
//
// It is a no-op when nothing is buffered, so the worker can call it after every Step.
func (s *teaSink) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
}

// closeWindow delivers the buffer whose window just expired. gen names that buffer: a
// mismatch means an Emit already flushed it and possibly opened another, whose own timer
// owns the delivery.
func (s *teaSink) closeWindow(gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gen != gen {
		return
	}
	s.flushLocked()
}

// flushLocked delivers the buffered tokens as one TokenEvent and closes the buffer. The
// caller holds mu. It is a no-op when nothing is buffered, so every boundary can call it
// unconditionally.
//
// The send happens under mu deliberately: it is what keeps a timer-goroutine flush from
// landing between the two sends of an Emit that is already flushing.
func (s *teaSink) flushLocked() {
	if !s.buffering {
		return
	}
	merged := domain.TokenEvent{EventBase: s.base, Text: s.pending}
	s.pending = ""
	s.base = domain.EventBase{}
	s.buffering = false
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	s.prog.send(eventMsg{Event: merged})
}

// coalesceWindow reports the window this sink coalesces over — the package default unless a
// test shrank it, so the zero-value sink the Bridge builds needs no wiring.
func (s *teaSink) coalesceWindow() time.Duration {
	if s.window > 0 {
		return s.window
	}
	return tokenCoalesceWindow
}
