package tui

import (
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The Event fold (the eventMsg path; ADR 0011)
// ----------------------------------------------------------------------------
//
// EVERY engine Event enters the view through foldEvent and nowhere else. The Update loop's
// eventMsg case hands the Event straight here, so the three folds a view update is made of —
// the status-line stats, the transcript, the live activity phrase — have one caller, in one
// order, in one file, instead of three switches in three files ordered by a comment.
//
// The order is load-bearing, and it is a DATA dependency rather than prose: the activity's
// ToolResultEvent rule needs to know whether any tool call is still open, transcript.apply is
// what pairs a result with its call, so foldEvent reads that fact AFTER apply and passes it
// in. A fold that needs something another fold establishes takes it as a parameter; none of
// them reaches sideways for it.
//
// Adding an Event variant therefore means answering, in one place, what each fold does with
// it — and TestFoldEventCoversEveryEventVariant reads internal/domain/events.go to check the
// answer was actually given, including "deliberately nothing".

// foldEvent folds one engine Event into the view: the live token stats, then the transcript,
// then the activity phrase. It mutates the local copy and returns it, like every Update fold;
// repainting the viewport is the caller's (the eventMsg case's).
func (m Model) foldEvent(e domain.Event) Model {
	m = m.foldStats(e)
	m.transcript.apply(e)
	// foldActivity runs after apply and is HANDED what apply established: its ToolResultEvent
	// rule asks whether a call is still open (a parallel batch holds the tool phrase), and only
	// the call/result pairing apply performs can answer that.
	return m.foldActivity(e, m.transcript.hasOpenToolCall())
}

// foldStats updates the live token stats from one engine Event (the eventMsg fold). Only the
// top-level agent's (Depth 0) accounting drives the status line: a sub-agent's usage nests in
// the stream, but the gauge tracks the conversation the human is steering. It marks when a
// Turn's content begins streaming (its first token) so a later UsageEvent can time the
// completion for a tokens/sec readout, resets that clock when the Turn re-streams, and on usage
// adopts the new context fill (the gauge's Used) and throughput. It mutates the local copy and
// returns it, like every Update fold.
func (m Model) foldStats(e domain.Event) Model {
	switch e := e.(type) {
	case domain.TokenEvent:
		if e.Depth == 0 && m.genStart.IsZero() {
			m.genStart = time.Now()
		}
	case domain.StreamResetEvent:
		if e.Depth == 0 {
			m.genStart = time.Time{} // the Turn re-streams (events.go) — time the fresh generation
		}
	case domain.UsageEvent:
		if e.Depth != 0 {
			break
		}
		// Prefer the server's total; fall back to prompt+completion when it omits the sum.
		total := e.TotalTokens
		if total == 0 {
			total = e.PromptTokens + e.CompletionTokens
		}
		if total > 0 {
			m.ctxUsed = total
		}
		if !m.genStart.IsZero() && e.CompletionTokens > 0 {
			if secs := time.Since(m.genStart).Seconds(); secs > 0 {
				m.tokPerSec = float64(e.CompletionTokens) / secs
			}
		}
		m.genStart = time.Time{}
	}
	return m
}
