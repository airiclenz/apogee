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
// eventMsg case hands the Event straight here, so the four folds a view update is made of —
// the status-line stats, the retained reasoning tail, the transcript, the live activity phrase —
// have one caller, in one order, in one file, instead of switches in as many files ordered by a
// comment.
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

// foldEvent folds one engine Event into the view: the live token stats, the retained reasoning
// tail, then the transcript, then the activity phrase. It mutates the local copy and returns it,
// like every Update fold; repainting the viewport is the caller's (the eventMsg case's).
func (m Model) foldEvent(e domain.Event) Model {
	m = m.foldStats(e)
	// Order-free, and placed here rather than woven into the dependency below because it has none:
	// the reasoning tail reads nothing the other folds establish, and nothing in the view reads what
	// it writes — it is retention behind the fold and no surface at all (reasoning.go).
	m = m.foldReasoning(e)
	m.transcript.apply(e)
	// The transcript fold's second half, and separate for one reason: a sub-agent's usage reading
	// is a FILL, so it needs the window it fills — and where the reading names none, that window is
	// a Model fact. It is HANDED in, by the same rule foldActivity is handed its answer below,
	// rather than reached for sideways from inside the transcript. The session's own bound model
	// rides in beside it on the same terms: the reading names the model that produced it, and only
	// the Model knows what that is DIFFERENT from (ADR 0045). The two travel together and are used
	// oppositely — the window is the FALLBACK behind the reading's own (a routed child fills the
	// Delegation target's window), the model the YARDSTICK the reading's own is measured against.
	m.transcript.applyUsage(e, m.opts.ContextWindow, m.opts.Model)
	// foldActivity runs after apply and is HANDED what apply established: its ToolResultEvent
	// rule asks whether a call is still open (a parallel batch holds the tool phrase), and only
	// the call/result pairing apply performs can answer that.
	return m.foldActivity(e, m.transcript.hasOpenToolCall())
}

// usageTotals is one agent's cumulative token accounting as the view holds it: the completions
// accounted for and the tokens they carried. It is read LATEST-WINS off the emitting agent's own
// running sum (domain.UsageEvent's Cumulative* fields) — the view never adds events up, so a fold
// that joined the stream late, or dropped an event, still reports the same totals as one that saw
// every one. The Model keeps the main agent's (foldStats) and each sub-agent run head keeps its
// own (transcript.applyUsage); plain ints throughout, so it rides safely in the value-copied
// Model (ADR 0011). Its field set matches session.Usage exactly, which is what lets the
// save/restore boundary convert between the two instead of mapping them member by member.
type usageTotals struct {
	Calls            int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// usageReading projects the cumulative half of a UsageEvent onto the view's own shape, and says
// whether the event carried one at all: an event stamped by an agent that has accounted for no
// call — a hand-built stream, or a record from before the engine counted — reports ok=false so
// its zeros never blank a reading that already stands.
func usageReading(e domain.UsageEvent) (usageTotals, bool) {
	if e.CumulativeCalls <= 0 {
		return usageTotals{}, false
	}
	return usageTotals{
		Calls:            e.CumulativeCalls,
		PromptTokens:     e.CumulativePromptTokens,
		CompletionTokens: e.CumulativeCompletionTokens,
		TotalTokens:      e.CumulativeTotalTokens,
	}, true
}

// foldStats updates the live token stats from one engine Event (the eventMsg fold). Only the
// top-level agent's (Depth 0) accounting drives the status line: a sub-agent's usage nests in
// the stream, but the gauge tracks the conversation the human is steering — the child's reading
// is not dropped, it fills its own run block instead (transcript.applyUsage). It marks when a
// Turn's content begins streaming (its first token) so a later UsageEvent can time the
// completion for a tokens/sec readout, resets that clock when the Turn re-streams, and on usage
// adopts the new context fill (the gauge's Used) and throughput. It mutates the local copy and
// returns it, like every Update fold.
//
// A usage report moves TWO readings, and they part company on the Maintenance flag: the fill and
// the throughput describe the conversation as it stands, so a maintenance event — the compaction
// call, whose prompt is the summarizer's own request — must leave the gauge and the generation
// clock exactly where the last Turn left them, while the cumulative totals take it like any other
// call, because those tokens were really spent (domain.UsageEvent).
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
			break // a delegate's fill, which is its run block's business and not the gauge's
		}
		if totals, ok := usageReading(e); ok {
			m.usage = totals // the main agent's own running sum, latest-wins — maintenance included
		}
		if e.Maintenance {
			break // accounted for above; the gauge and the generation clock skip it
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
