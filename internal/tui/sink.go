package tui

import "github.com/airiclenz/apogee/internal/domain"

// ----------------------------------------------------------------------------
// Event→Msg bridge (phase-2 detail plan §3 C2)
// ----------------------------------------------------------------------------

// teaSink is the EventSink the engine emits through. Emit wraps each Event in an eventMsg
// and hands it to the running program via Send — Bubble Tea's goroutine-safe, async-to-
// Update enqueue — which is exactly the mechanism the EventSink contract intends and
// satisfies "Emit must not block the loop for long" (Send is async, so the Step goroutine
// never blocks here).
//
// Delivery is lossless by default: every Event becomes one Msg, never dropped — the
// correctness floor the bench-side ordering and the TUI both want. If TokenEvent flooding
// ever shows program-queue pressure, coalesce adjacent TokenEvents within this sink
// (concatenate their text in a short window) — coalescing, never dropping — behind this
// same interface; do not pre-optimise (phase-2 detail plan §3 C2).
type teaSink struct {
	prog *programRef
}

// teaSink is the engine's EventSink.
var _ domain.EventSink = (*teaSink)(nil)

// Emit forwards e to the Update loop as an eventMsg. It is called synchronously on the Step
// goroutine, in Turn order; the async Send keeps the loop moving.
func (s *teaSink) Emit(e domain.Event) {
	s.prog.send(eventMsg{Event: e})
}
