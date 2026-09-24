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
// deltas of one kind sharing an EventBase are coalesced — TokenEvents with TokenEvents,
// ReasoningEvents with ReasoningEvents — their text accumulates for a short window
// (tokenCoalesceWindow) and lands as a single Event of that kind — which is the option phase-2
// detail plan §3 C2 held in reserve, taken because a provider emits one delta per visible
// byte-run (internal/agent/loop.go) and every Msg costs the TUI a render (the transcript for a
// token, the /thinking pane for a reasoning chunk). The kind is part of the merge key because
// the reasoning and visible tokens of one Turn share an EventBase: a change of kind flushes, so
// the two streams never merge into one another. Merging is coalescing, never dropping, with one
// visible edge: the receiving seam strips escapes over the merged text, so an escape sequence
// the provider split across two deltas is stripped whole rather than leaving its tail behind as
// literal text (the token path has always had this over sink-merged tokens). Every other variant
// flushes the open buffer ahead of itself, so what the Update loop sees is exactly the order the
// engine emitted; flush() closes the window at the Step boundary, so a stream never spills past
// the Step that produced it.
type teaSink struct {
	prog *programRef

	// mu guards the coalescing buffer AND covers every send made out of it, so the Emit
	// goroutine and the timer goroutine can never interleave halfway through a delivery:
	// whatever order two goroutines take the lock in is the order the Update loop sees.
	mu sync.Mutex
	// pending is the accumulated delta text — a plain string, never a strings.Builder,
	// per this package's no-copy-type hygiene (ADR 0011, doc.go).
	pending string
	// base is the (Depth, Turn, run identity) the pending text belongs to. Only deltas sharing all
	// three — and reasoning, below — may merge: a sub-agent's stream (Depth > 0) nests inside the
	// parent's and is a different block in the transcript, a Turn boundary is a commit point, and
	// two children of one reply share a depth but never a run id (domain.EventBase.RunID — their
	// spawning call ids can collide), so the run identity is what keeps concurrent siblings' text
	// from merging into one another's block (ADR 0039).
	base domain.EventBase
	// reasoning is the kind of the open buffer: true for ReasoningEvent text, false for
	// TokenEvent text. It is part of the merge key beside base, because one Turn's reasoning and
	// visible tokens share an EventBase and must never merge into one another.
	reasoning bool
	// buffering says a buffer is open, which an empty pending string cannot: a zero-length
	// token must still be delivered rather than silently swallowed.
	buffering bool
	// gen identifies the open buffer, so a window timer that lost the race to the lock
	// (its buffer was already flushed by an Emit) returns instead of cutting the NEXT
	// buffer's window short.
	gen uint64
	// timer closes the current window. It is armed by the delta that opens a buffer and
	// never re-armed while that buffer grows, so a continuous stream still flushes once
	// per window rather than sliding forever.
	timer *time.Timer
	// window overrides tokenCoalesceWindow; zero means the default. Tests shrink it.
	window time.Duration
}

// tokenCoalesceWindow is how long adjacent deltas (tokens or reasoning) may accumulate before the
// buffer is delivered: about two frames at 60 fps — imperceptible as latency, and it caps
// delta-driven repaints near ~33/s no matter how fast the provider streams.
const tokenCoalesceWindow = 30 * time.Millisecond

// teaSink is the engine's EventSink.
var _ domain.EventSink = (*teaSink)(nil)

// Emit forwards e to the Update loop as an eventMsg. It is called synchronously on the Step
// goroutine, in Turn order; the async Send keeps the loop moving.
//
// Adjacent TokenEvents, and adjacent ReasoningEvents, are coalesced (see the type doc): a delta
// joins the open buffer when it matches its kind and EventBase, and anything else delivers that
// buffer first and then itself. The flush-before is not a nicety — every other variant depends
// on the deltas that preceded it: StreamResetEvent
// discards them, MessageEvent/ToolCallEvent commit them as narration, and UsageEvent times
// the generation the first token started.
//
// A SeamClosedEvent crosses without its Value: the seam's payload is a live reference into the
// engine's working value, valid only for the duration of this call (domain.SeamClosedEvent), and
// the Update goroutine reads the Msg after Emit has returned — by which point the engine is
// mutating what it points at. Nothing on the TUI side reads it, so the copy that crosses carries
// Seam and Fired and a nil Value. The caller's own copy is untouched: a Reaction Runner wrapping
// this sink matches on the value it was handed, not on what was forwarded.
func (s *teaSink) Emit(e domain.Event) {
	switch d := e.(type) {
	case domain.TokenEvent:
		s.emitDelta(d.EventBase, d.Text, false)
		return
	case domain.ReasoningEvent:
		s.emitDelta(d.EventBase, d.Text, true)
		return
	}
	if seam, ok := e.(domain.SeamClosedEvent); ok {
		seam.Value = nil
		e = seam
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flushLocked()
	s.prog.send(eventMsg{Event: e})
}

// emitDelta merges one delta's text into the open buffer, or opens a new one when nothing is
// buffered or the stream moved to another (Depth, Turn, run identity) or another kind (reasoning
// versus visible tokens). Nothing is delivered here unless a boundary forces it: the window timer
// does the delivering.
func (s *teaSink) emitDelta(base domain.EventBase, text string, reasoning bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buffering && (s.base != base || s.reasoning != reasoning) {
		s.flushLocked()
	}
	if !s.buffering {
		s.buffering = true
		s.base = base
		s.reasoning = reasoning
		s.gen++
		gen := s.gen
		s.timer = time.AfterFunc(s.coalesceWindow(), func() { s.closeWindow(gen) })
	}
	s.pending += text
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

// flushLocked delivers the buffered deltas as one Event of the buffer's kind — a TokenEvent or a
// ReasoningEvent — and closes the buffer. The
// caller holds mu. It is a no-op when nothing is buffered, so every boundary can call it
// unconditionally.
//
// The send happens under mu deliberately: it is what keeps a timer-goroutine flush from
// landing between the two sends of an Emit that is already flushing.
func (s *teaSink) flushLocked() {
	if !s.buffering {
		return
	}
	var merged domain.Event = domain.TokenEvent{EventBase: s.base, Text: s.pending}
	if s.reasoning {
		merged = domain.ReasoningEvent{EventBase: s.base, Text: s.pending}
	}
	s.pending = ""
	s.base = domain.EventBase{}
	s.reasoning = false
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
