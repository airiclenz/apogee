package tui

import (
	"slices"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The Event fold (the eventMsg path; ADR 0011)
// ----------------------------------------------------------------------------
//
// EVERY engine Event enters the view through foldEvent and nowhere else. The Update loop's
// eventMsg case hands the Event straight here, so the five folds a view update is made of —
// the status-line stats, the thinking board, the Inspector's wire ring, the transcript, the live
// activity phrase — have one caller, in one order, in one file, instead of switches in as many
// files ordered by a comment.
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

// throughputWindowFloor is the shortest generation window the tok/s readout will divide by.
// Under it the elapsed time is dominated by scheduling jitter rather than by generation, and
// the quotient stops describing the server at all — 200 completion tokens over a 130 µs window
// reads as 1.5 million tok/s. A sub-floor window therefore yields NO reading (tokPerSec 0, the
// suffix hidden) instead of a clamped or invented one.
const throughputWindowFloor = 250 * time.Millisecond

// foldEvent folds one engine Event into the view: the live token stats, the thinking board, the
// wire ring, then the transcript, then the activity phrase. It mutates the local copy and returns
// it, like every Update fold; repainting the viewport is the caller's (the eventMsg case's).
func (m Model) foldEvent(e domain.Event) Model {
	m = m.foldStats(e)
	// Order-free, and placed here rather than woven into the dependency below because it has none:
	// the thinking board reads nothing the other folds establish, and nothing but the /thinking
	// pane reads what it writes (thinking.go).
	m = m.foldThinking(e)
	// Order-free for the same reason, and deliberately NOT part of the transcript fold below: a wire
	// record is not a conversation entry (inspector.go), so it lands in the Inspector's own ring
	// beside the transcript and disturbs no entry pairing. It reads nothing the other folds
	// establish, and nothing but /inspect reads what it writes.
	m = m.foldWire(e)
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
	// The band's half of the same delivery report the transcript just folded: a message the human
	// addressed to a running child is accounted for by the engine (ADR 0063), so the staged row
	// that stood for it comes off here rather than on a timer (interject.go).
	if child, ok := e.(domain.ChildInterjectionEvent); ok {
		m.foldChildDelivery(child)
	}
	// Inside a run view the prompt box addresses the child on screen, by that child's lifecycle and
	// name — both of which this event may have just moved. Nothing re-resolves the legend here: it
	// is derived at paint off the head this fold just updated ([Model.legend]), so the next frame
	// names the phase and the name the run wears now.
	// foldActivity runs after apply and is HANDED what apply established: its ToolResultEvent
	// rule asks whether a call is still open (a parallel batch holds the tool phrase), and only
	// the call/result pairing apply performs can answer that.
	return m.foldActivity(e, m.transcript.hasOpenToolCall())
}

// usageReading is the cumulative half of a UsageEvent as one domain.Usage: the emitting agent's
// own running sum (the Cumulative* fields), which the view reads LATEST-WINS rather than adding
// events up — so a fold that joined the stream late, or dropped an event, still reports the same
// totals as one that saw every one. An event stamped by an agent that has accounted for no call —
// a hand-built stream, or a record from before the engine counted — reads as the zero Usage, whose
// zero Calls is the absence of accounting (domain.Usage) rather than a spend of zero, so it never
// blanks a reading that already stands (Adopt refuses it; foldStats checks the same counter).
func usageReading(e domain.UsageEvent) domain.Usage {
	if e.CumulativeCalls <= 0 {
		return domain.Usage{}
	}
	return domain.Usage{
		Calls:              e.CumulativeCalls,
		PromptTokens:       e.CumulativePromptTokens,
		CachedPromptTokens: e.CumulativeCachedPromptTokens,
		CompletionTokens:   e.CumulativeCompletionTokens,
		TotalTokens:        e.CumulativeTotalTokens,
	}
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
//
// A PruneEvent moves no reading at all, and deliberately: pruning frees window the NEXT request
// will show, and the gauge reports what the last reply actually reported. Subtracting the freed
// tokens here would paint a fill no Upstream has confirmed.
//
// The generation clock starts on the Turn's first depth-0 OUTPUT event — a reasoning chunk as
// readily as a visible token — because the completion count it is divided by includes the
// reasoning tokens. A window shorter than throughputWindowFloor is refused rather than divided:
// a cached or instant completion timed over a millisecond reads in the millions, which is noise
// wearing a number's clothes.
func (m Model) foldStats(e domain.Event) Model {
	switch e := e.(type) {
	case domain.TokenEvent:
		if e.Depth == 0 && m.genStart.IsZero() {
			m.genStart = time.Now()
		}
	case domain.ReasoningEvent:
		// Reasoning is generated output like any other: the server counts it in
		// completion_tokens, so the window the throughput divides by has to start here too
		// when a Turn thinks before it speaks. Timing only the visible tokens divided the
		// WHOLE completion by the tail of the generation, which is where the absurd readings
		// came from.
		if e.Depth == 0 && m.genStart.IsZero() {
			m.genStart = time.Now()
		}
	case domain.StreamResetEvent:
		if e.Depth == 0 {
			m.genStart = time.Time{} // the Turn re-streams (events.go) — time the fresh generation
		}
	case domain.UsageEvent:
		// The model that answered is a SESSION fact before it is any one agent's: a routed child's
		// reading (ADR 0045) names a model the session was answered by just as the main agent's does,
		// so the set is folded ahead of the depth guard below, in order first seen and once each.
		if e.ServedModel != "" && !slices.Contains(m.servedModels, e.ServedModel) {
			m.servedModels = append(slices.Clone(m.servedModels), e.ServedModel)
		}
		// A maintenance reading is the ONE signal an automatic fold gives — a child's at every
		// quiescent Turn boundary, the main agent's at an Exchange opening — because the fold itself
		// is quiet on success (agent/compact.go, autoCompact). So the reading is where the fold's
		// trace is written, at the depth that folded (transcript.addCompacted), ahead of the depth
		// guard below. The /compact worker's own reading is the exception: its terminal Msg writes
		// the note (foldCompactDone), and a second one here would say the fold happened twice.
		if e.Maintenance && !(e.Depth == 0 && m.acts.at(runRef{}).act.kind == actCompacting) {
			m.transcript.addCompacted(runOf(e.EventBase))
		}
		if e.Depth != 0 {
			break // a delegate's fill, which is its run block's business and not the gauge's
		}
		if reading := usageReading(e); reading.Calls > 0 {
			// The reading is the ENGINE's own running sum since THIS session was opened — latest-wins,
			// maintenance included — and the base is what the record carried into it (Model.usageBase,
			// zero on a fresh launch). Adding the two is the only sum here: a resume restarts the
			// engine's count at zero, so replacing rather than offsetting would drop everything the
			// reopened record had already spent. The base is a fixed offset, so adding it to each
			// latest reading adds it once, not once per event. The offset is why this is not a plain
			// Adopt onto m.usage: what stands there is base plus the PREVIOUS reading, and an uncounted
			// event must leave that whole sum alone, not fall back to the base.
			m.usage = domain.Sum(m.usageBase, reading)
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
			if window := time.Since(m.genStart); window >= throughputWindowFloor {
				m.tokPerSec = float64(e.CompletionTokens) / window.Seconds()
			} else {
				m.tokPerSec = 0 // sub-floor: unmeasured, and the suffix hides itself
			}
		}
		m.genStart = time.Time{}
	}
	return m
}

// progressSaveTrigger reports whether folding e has left the record worth re-persisting MID-Turn —
// the cadence of the delegation progress save (Model.progressSave, sessionsave.go; ADR 0022
// addendum). It is a pure function of the Event, so the eventMsg case can ask it beside the fold
// and no other surface has to carry the rule.
//
// Exactly three arms answer true, and each marks a point at which a reader of the record — a second
// session, a reviewer, headless tooling — would otherwise be looking at a conversation that stopped
// at the previous tool call:
//
//   - A depth-0 ToolCallEvent for sub_agent: the delegation is ISSUED. This is the save that puts
//     the assistant message that delegated, and the prompt it carried, into the record. Without it a
//     Turn holding a fan-out shows nothing of the delegation until the whole Turn ends.
//   - A ToolResultEvent at depth 1 or deeper: a CHILD crossed a tool boundary, which is the running
//     delegation's progress. A depth-0 result is deliberately not one — the Turn's own tool calls
//     are followed by the per-Turn snapshot that saves them (turnSnapshotMsg), and a long LEAF tool
//     is out of scope (the plan's ratified call 3: generalising is one predicate away, later).
//   - A SubAgentPhaseEvent reporting SubAgentFinished with Cancelled false: one delegation of a
//     group reached its boundary and its report is in the record. Under a fan-out its siblings run
//     on, so this is a progress point of its own rather than the Turn's end. A CANCELLED finished
//     is not one — it closes a bracket for a log reader (ADR 0075 decision 12) and nothing reached
//     the record to save.
//   - A SubAgentNamedEvent: a running delegation has just been given its generated name (ADR 0068),
//     and what a run is CALLED is part of the run rather than view state — so a record saved
//     before the rename, and resumed after it, would paint the task's first line the session had
//     already stopped showing.
//
// SubAgentStarted is deliberately NOT one: the head's own ToolCallEvent already fired the save, and
// under a fan-out a queued child's start adds nothing the record does not already show. Every other
// variant answers false — streamed tokens, reasoning, approvals, usage, audit and wire records, a
// seam closure (whose payload is a live working value the sink may not even retain, let alone
// persist), and a pruning notice (which rewrites what the ENGINE keeps, never the record, and whose
// host note the per-Turn save carries anyway) either move nothing a reader of the record could act
// on or are already covered by the per-Turn save that follows the Turn they belong to.
func progressSaveTrigger(e domain.Event) bool {
	switch e := e.(type) {
	case domain.ToolCallEvent:
		return e.Depth == 0 && e.Call.Tool == subAgentToolName
	case domain.ToolResultEvent:
		return e.Depth >= 1
	case domain.SubAgentPhaseEvent:
		// A CANCELLED finished is the exception: it closes the bracket of a delegation the parent
		// Turn rolled back, so no report landed and the record has gained nothing to save.
		return e.Phase == domain.SubAgentFinished && !e.Cancelled
	case domain.SubAgentNamedEvent:
		return true
	}
	return false
}
