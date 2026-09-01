package tui

import (
	"fmt"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The live activity (the status line's left slot)
// ----------------------------------------------------------------------------
//
// activity answers the question the human is actually asking while a worker runs — is it
// reasoning, writing, running a tool, or stuck? — in place of the turn index, which answered
// none of it. It is DERIVED, never authoritative: foldActivity folds the same Event stream the
// transcript folds (the last of the three folds foldEvent owns — fold.go), and the handful of
// transitions no Event announces — a submit, /compact, a stop, the worker's terminal Msg — set
// it directly.
//
// It is not a lifecycle state. compacting and stopping are activities, not uiStates, so the
// ADR 0011 state machine is untouched: statusLine still switches on m.state and only the
// running branch consults the activity.
//
// There is one activity per RUN and not one per session (runActivities, [Model.acts]): concurrent
// delegates (ADR 0039) each own a phrase and a clock, the status line composes ONE sentence out of
// the board, and a run view can show its own child's slot (ADR 0063).
//
// This file is pure — no lipgloss, no I/O — the toolregistry.go discipline, so the whole
// vocabulary is table-testable (activity_test.go); statusLine owns the styling. activity is a
// plain value type reached by the value-copied Model, so it must never hold a strings.Builder
// or any other self-pointer no-copy type (ADR 0011; doc.go; TestModelNoBuilderByValue).

// activityKind is what the worker is doing right now — the coarse vocabulary the status line
// renders. actTool is the one kind that carries a payload (the label naming what the tool is
// doing); every other kind renders a fixed word.
type activityKind int

const (
	actIdle       activityKind = iota // no worker: the left slot renders nothing
	actThinking                       // a request is in flight, or reasoning chunks are arriving
	actResponding                     // visible assistant text is streaming
	actTool                           // a tool call is open (no result yet)
	actRetrying                       // the loop is re-streaming the Turn (StreamResetEvent)
	actCompacting                     // the /compact worker is folding the conversation
	actStopping                       // Esc fired the cancel; the worker has not unwound yet
	actWorking                        // ≥2 delegates are acting at once: the merged top-level phrase
)

// activity is the status line's live left slot: what is happening, since when, at which
// sub-agent nesting depth, and on whose behalf. label is used by actTool only; since is when
// THIS activity began, so the elapsed clock measures the work in flight rather than the whole
// exchange.
type activity struct {
	kind  activityKind
	label string // actTool only: the presented verb alone ("reading", "running mcp_weather")
	// call is WHICH tool call the phrase describes: the id of the call whose announcement set this
	// activity (domain.ToolCall.ID), empty for every other kind. It is what the elapsed clock is
	// keyed on, because a verb-only label can no longer tell two calls apart — back-to-back reads
	// both word themselves "reading", and a clock restarted only when the TEXT changes would count
	// the previous file's call straight through the new one.
	call  string
	depth int // > 0 → the phrase is prefixed with the acting sub-agent's identity
	// spawn is WHICH sub-agent is acting: the id of the sub_agent call that spawned the agent
	// whose event set this activity (domain.EventBase.CallID), empty for the top-level agent.
	// Depth cannot answer that once children run CONCURRENTLY (ADR 0039) — siblings share it —
	// and the slot names one delegate at a time, so it must name the right one. It is the LOOKUP
	// key, not the name: the delegation's name is resolved against the transcript at compose time
	// (transcript.runName), so a run head that has not folded yet simply reads as unnamed rather
	// than freezing a stale answer here.
	spawn string
	since time.Time // when this activity began — the elapsed clock's origin
}

// runActivity is ONE run's slot on the activity board: what that agent is doing now, when its slot
// was opened, and when that run was last heard from.
//
// since is the RUN's clock and deliberately not the phrase's: it is stamped once, when the run's
// first event opens the slot, and never restamped, while [activity.since] inside act moves with
// every change of phrase. The merged top-level phrase counts from the OLDEST live child's since
// (runningPhrase), so a sibling emitting cannot restart the number the human is reading.
//
// lastEvent is this run's own stall clock: a child's silence is measured from ITS last word,
// because a busy sibling is no evidence the quiet one is alive. The top-level run keeps reading the
// engine-wide clock instead ([Model.lastEvent], Model.quietClock) — every Event variant refreshes
// that one, including the ones no fold acts on.
//
// Three plain value fields with no no-copy type, so the board rides the value-copied Model
// (ADR 0011; doc.go).
type runActivity struct {
	act       activity
	since     time.Time
	lastEvent time.Time
}

// runActivities is the status line's activity board: one [runActivity] per RUN, keyed by the run
// identity that names it (runRef — transcript.go), where the ZERO key is the human's own top-level
// conversation and every other key is a delegate.
//
// One slot per run is what stopped the single slot flickering. Concurrent children (ADR 0039) all
// wrote the one slot in turn, so a fan-out's status line jittered between whichever delegate spoke
// last; now each run owns its own phrase and clock, the top level composes ONE sentence out of them
// (runningPhrase), and a run view can show its own child's slot verbatim (ADR 0063).
//
// Slots are opened by the Event fold (foldActivity) and closed by exactly three things: the child's
// SubAgentFinished phase, a depth-0 event — a parent event means its children are over, ADR 0013 D5
// — and finishWorker, which drops the whole board when the worker unwinds.
//
// It is a map, so it is a reference the value-copied Model shares between its copies rather than a
// value each copy owns ([Model.spentSkills] is the same shape). That is safe for the same reason:
// the Event fold is its only writer and the Update that folds is the only owner of the Model at
// that moment, so no second copy is reading the board while it changes.
type runActivities map[runRef]runActivity

// at is run's slot, or the zero slot when that run holds none — a run with no slot has said nothing
// yet, which is exactly what an idle activity renders (activity.text).
func (a runActivities) at(run runRef) runActivity {
	return a[run]
}

// put writes run's slot, allocating the board on first use so a zero Model needs no constructor.
func (a *runActivities) put(run runRef, slot runActivity) {
	if *a == nil {
		*a = runActivities{}
	}
	(*a)[run] = slot
}

// drop closes one run's slot. Dropping a run that holds none is a no-op, which is what lets the
// SubAgentFinished of a delegation that never emitted anything fold like any other.
func (a runActivities) drop(run runRef) {
	delete(a, run)
}

// dropChildren closes every DELEGATE's slot, leaving the top-level one alone. A child runs
// atomically inside its parent's Turn (ADR 0013 D5), so the parent being heard from at depth 0 is
// proof every child of that Turn is over — including one whose SubAgentFinished the view never saw
// (a cancelled group is dropped unappended and emits no finished phase).
func (a runActivities) dropChildren() {
	for run := range a {
		if run.depth > 0 {
			delete(a, run)
		}
	}
}

// children is every live delegate slot's run, in no particular order. It is the top-level phrase's
// one input: none means the human's own conversation is what is running, one names that delegate,
// and two or more are merged into a single count (runningPhrase).
func (a runActivities) children() []runRef {
	if len(a) == 0 {
		return nil
	}
	kids := make([]runRef, 0, len(a))
	for run := range a {
		if run.depth > 0 {
			kids = append(kids, run)
		}
	}
	return kids
}

// oldestChild is the delegate whose slot opened FIRST, and false when no delegate holds one. It is
// what the merged phrase's clock counts from, so that clock measures the fan-out rather than
// whichever sibling happened to speak last. Ties — two children whose first events landed inside
// the same clock tick — break on the spawning call id, so the answer is stable across repaints
// rather than a map iteration's accident.
func (a runActivities) oldestChild() (runRef, bool) {
	var oldest runRef
	found := false
	for _, run := range a.children() {
		if !found {
			oldest, found = run, true
			continue
		}
		since, best := a[run].since, a[oldest].since
		if since.Before(best) || (since.Equal(best) && run.spawn < oldest.spawn) {
			oldest = run
		}
	}
	return oldest, found
}

// subAgentActivityName is what the status line calls an UNNAMED delegate — "sub-agent · reading".
// It is the status line's own word and no longer a label the transcript also draws: the rail and
// its ┌─┶ header are the whole of what the transcript says about a descent now, so nothing here is
// held to another surface's spelling. The transcript's own word for the tool is the registry's
// "Sub-Agent" label, which titles its blocks (subAgentGroupLabel).
const subAgentActivityName = "sub-agent"

// text renders the activity as the status line's unstyled phrase. Idle says nothing at all —
// the input box below already invites a message, so a word there would be noise. A phrase from
// a sub-agent (Depth > 0) is prefixed with its identity, so "sub-agent · searching" reads as one
// sentence fragment at any nesting level.
//
// name is the acting delegation's short name, resolved by the caller (runningPhrase, against
// transcript.runName) and "" when the delegation has no name yet. A named child takes its name in
// place of the generic word — "repo-scout · reading" — because with a fan-out running
// the slot can name only one delegate at a time and "sub-agent" says nothing about which. An
// unnamed one falls back to subAgentActivityName.
//
// "Yet" is load-bearing: a delegation the model left unnamed is named out of band while it runs
// (ADR 0068, transcript.addSubAgentName), and because the name is looked up per compose rather than
// frozen into the slot, the phrase moves off subAgentActivityName onto the generated name the first
// frame after the rename folds.
func (a activity) text(name string) string {
	var phrase string
	switch a.kind {
	case actIdle:
		return ""
	case actThinking:
		phrase = "thinking"
	case actResponding:
		phrase = "responding"
	case actTool:
		phrase = a.label
	case actRetrying:
		phrase = "retrying"
	case actCompacting:
		phrase = "compacting"
	case actStopping:
		phrase = "stopping"
	case actWorking:
		// The merged phrase says only that delegates are working, because with a fan-out running the
		// slot cannot honestly say WHAT without picking one of them (runningPhrase). The count rides
		// in as the name, so "2 sub-agents · working" composes through the same prefix rule.
		phrase = "working"
	}
	if phrase == "" {
		return "" // an actTool with no label (a tool with neither verb nor target): say nothing
	}
	if a.depth > 0 {
		if name != "" {
			return name + " · " + phrase
		}
		return subAgentActivityName + " · " + phrase
	}
	return phrase
}

// elapsed is how long this activity has been running at now. A zero since (the activity was
// never set — the defensive case) and a clock that moved backwards both read as zero rather
// than as an absurd duration on the status line.
func (a activity) elapsed(now time.Time) time.Duration {
	if a.since.IsZero() {
		return 0
	}
	if d := now.Sub(a.since); d > 0 {
		return d
	}
	return 0
}

// isQuietWatched is whether the stall guard watches this activity kind: the single seat of that
// rule, read by BOTH of the guard's gates — quiet below, which decides whether the silence is
// reported, and moveActivity, which decides whether a move restamps the clock that silence is
// measured from. The two were coupled by hand before, so a kind added to one would have inherited
// the other silently. Which kinds are watched, and why only those, is quiet's own doc comment.
func (k activityKind) isQuietWatched() bool {
	return k == actThinking || k == actResponding
}

// quiet is whether the status line owes the human the fact that the ENGINE has gone silent at now.
// lastEvent is when it was last heard from ([Model.lastEvent]); after is the `ui.stall-after`
// threshold, where 0 turns the guard off entirely.
//
// It answers WHETHER and deliberately not how long. The row spends the fact as a bare `quiet`
// qualifier in front of the activity's own clock — "thinking · quiet · 2m 59s" (runningPhrase,
// model.go) — because the silence and the activity are the same span in the case the guard was
// built for (an engine that never spoke at all), where two clocks read as one fact stated twice.
// A silence duration returned here and painted nowhere would only invite that second clock back.
//
// The guard reports SILENCE and never a verdict, which is what the three gates encode. It speaks
// only while the worker claims to be thinking or responding: a long silent tool call is normal
// (the tool, not the engine, is the thing taking its time), a stopping worker is already
// answering for itself, and a compaction emits nothing until it lands. It reports nothing at all
// until the engine has been heard from once — a zero lastEvent would otherwise measure from the
// epoch, the elapsed posture above — and nothing on a clock that moved backwards, since after is
// always positive here.
func (a activity) quiet(lastEvent, now time.Time, after time.Duration) bool {
	if after <= 0 || lastEvent.IsZero() {
		return false
	}
	if !a.kind.isQuietWatched() {
		return false
	}
	return now.Sub(lastEvent) >= after
}

// secondsPerMinute is the elapsed clock's rollover point (formatElapsed).
const secondsPerMinute = 60

// formatElapsed renders a duration as the status line's compact clock: "3s" below a minute,
// "1m 04s" above it (zero-padded seconds, so the readout does not jitter in width as it
// counts). There is deliberately no hour form — a long-running call simply keeps counting
// minutes ("60m 00s"), which stays unambiguous without a third format to parse at a glance.
func formatElapsed(d time.Duration) string {
	secs := int(d / time.Second)
	if secs < 0 {
		secs = 0
	}
	if secs < secondsPerMinute {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm %02ds", secs/secondsPerMinute, secs%secondsPerMinute)
}

// toolActivityVerb builds the actTool phrase for a call from the presentation registry: the tool's
// active verb and nothing else ("reading", "running"). An unregistered (dynamic MCP) tool inherits
// presentToolCall's "running <raw name>" fallback, so it is still a truthful fragment.
//
// The TARGET is deliberately absent. The status line shares one row with the context gauge, and the
// path it used to carry was both the thing that pushed the gauge off that row and a restatement of
// the tool-call block already on screen a line below — so the slot keeps the half only it can say
// (what is happening right now) and leaves the target to the block. Nothing words the pair any more:
// a collapsed sub-agent run's gist was the last surface that did, and it now says nothing while the
// child works (subAgentGist, subagentblock.go).
//
// It needs no escape-stripping of its own: the phrase is built ONLY from presentToolCall's view,
// which leaves that function through finishDisplay, so the status line inherits the tool card's
// seam rather than re-deriving the discipline. That matters here more than it looks — foldActivity
// paints this label the moment a call is ANNOUNCED, before any approval gate runs, so it is the
// earliest point at which a hostile model's argument reaches the screen.
func toolActivityVerb(call domain.ToolCall, ws workspaceRoot) string {
	// No resolved path is handed in, for the same reason the target is absent: this slot says what
	// is happening, and where a path lands is the block's line to draw (resolvedPathNote).
	return presentToolCall(call, "", ws).Verb
}

// setActivity moves ONE run's slot to a new activity, keyed on the phrase it renders (kind and
// label). It is the seat for the handful of transitions no Event announces — a submit, a /compact,
// a stop — which all speak for the top-level run (runRef{}); the Event fold writes its slots
// through foldSlot instead, because those carry a rule about the OTHER slots that these do not.
func (m *Model) setActivity(run runRef, kind activityKind, label string) {
	m.moveActivity(run, activity{kind: kind, label: label})
}

// foldSlot writes the slot ONE Event names, and carries the rule that is the Event stream's alone:
// a depth-0 event means every delegate of this Turn is over — a child runs atomically inside its
// parent's Turn (ADR 0013 D5) — so the parent's own word closes their slots as it takes the board
// back. A stop deliberately does NOT go through here (setActivity above): a stop is not the parent
// resuming, and the sticky stopping phrase outranks whatever the child slots hold anyway
// (runningPhrase).
func (m *Model) foldSlot(run runRef, next activity) {
	if run.depth == 0 {
		m.acts.dropChildren()
	}
	m.moveActivity(run, next)
}

// moveActivity is the single seat of the elapsed clock's origin: next inherits the running clock
// unless what it says the worker is doing actually changed — its kind, its phrase, or the tool call
// it names. A stream of TokenEvents must keep one running clock, not reset it on every chunk, and
// the same holds for the repeated announcement of one call. Depth and spawn alone do not restart it
// either: the phrase's sub-agent prefix changes, but the same work is still in flight.
//
// next.since is ignored — callers state what is happening, never when it started.
//
// It is also the second seat of the QUIET clock, beside the eventMsg case that stamps it on every
// Event: an activity moves either because an Event moved it (the engine spoke) or because a worker
// was just launched and its request is away (the four transitions no Event announces), and both are
// the engine being heard from. Restamping here is what keeps a fresh Exchange from inheriting the
// silence of the one before it, structurally, rather than by every launch site remembering to.
//
// That restamp is gated on the kinds the guard actually watches (isQuietWatched), so this seat can
// never outgrow the one that reports: a move to compacting or stopping leaves the clock where it
// was — neither claims the engine is working, and the guard says nothing for either — while a move
// to thinking or responding measures the new claim's silence from now.
func (m *Model) moveActivity(run runRef, next activity) {
	slot := m.acts.at(run)
	// The slot's key already says whose run this is, so the activity's own copy of that pair is
	// filled in here rather than by every caller: text() stays a pure function of the slot.
	next.depth, next.spawn = run.depth, run.spawn
	next.since = slot.act.since
	if slot.act.kind != next.kind || slot.act.label != next.label || slot.act.call != next.call {
		next.since = time.Now()
	}
	slot.act = next
	// The RUN's own clock starts when its slot opens and never moves again: the merged phrase counts
	// the fan-out from it, and a phrase changing inside one child must not restart that number.
	if slot.since.IsZero() {
		slot.since = next.since
	}
	if next.kind.isQuietWatched() {
		slot.lastEvent = time.Now()
	}
	m.acts.put(run, slot)
	if next.kind.isQuietWatched() {
		m.noteEngineHeard()
	}
}

// noteEngineHeard restamps the quiet clock: the engine has just been heard from, so the stall guard
// measures its silence from now (the status line's quiet qualifier — quiet above, runningPhrase in
// model.go). Timers say nothing here: a heartbeat, a spinner frame and an elapsed-clock repaint all
// prove the TUI is alive, which was never the question the guard asks.
func (m *Model) noteEngineHeard() {
	m.lastEvent = time.Now()
}

// foldActivity derives the live activity from one engine Event (the third fold foldEvent runs).
// openCall is whether any tool call is still waiting for its result — the call/result pairing
// transcript.apply establishes and foldEvent hands over (fold.go), so the ToolResultEvent rule
// below reads a value rather than an ordering.
//
// Events that say nothing about what the worker is doing next — an error notice, usage
// accounting, an audit record, a fired mechanism, an approval record — leave the activity
// alone, so the phrase does not flicker off the work actually in flight.
//
// stopping is STICKY, and run-wide: once Esc has fired the cancel the worker keeps emitting events
// until it reaches a quiescent boundary, and overwriting the phrase there would tell the human
// their stop was ignored. The stop is the whole RUN's — a child cannot outlive the Turn it runs
// inside (ADR 0013 D5) — so a stopping TOP-LEVEL slot freezes every slot on the board, not just its
// own. Only finishWorker clears it, when the worker has actually unwound.
//
// Every arm carries the emitting agent's SPAWNING call id beside its depth, and reads it as
// e.EventBase.CallID rather than e.CallID: a variant that also reports a call of its own names it
// CallID too and therefore shadows the embedded field (domain.AuditEvent — not folded here, but the
// spelling must not depend on which arm you are in). It is the id the delegation's name is looked
// up by; empty at depth 0, where nothing spawned the emitter.
func (m Model) foldActivity(e domain.Event, openCall bool) Model {
	if m.acts.at(runRef{}).act.kind == actStopping {
		return m
	}
	switch e := e.(type) {
	case domain.ReasoningEvent:
		// The honest "thinking": the model is reasoning, not merely unanswered.
		m.foldSlot(runOf(e.EventBase), activity{kind: actThinking})
	case domain.TokenEvent:
		m.foldSlot(runOf(e.EventBase), activity{kind: actResponding})
	case domain.StreamResetEvent:
		m.foldSlot(runOf(e.EventBase), activity{kind: actRetrying})
	case domain.ToolCallEvent:
		// call is the id of the call the phrase describes, and is what the elapsed clock is keyed
		// on: a second read of a second file is new work and deserves a clock of its own even
		// though both phrases read "reading" (moveActivity).
		m.foldSlot(runOf(e.EventBase), activity{
			kind:  actTool,
			label: toolActivityVerb(e.Call, m.transcript.ws),
			call:  e.Call.ID,
		})
	case domain.ToolResultEvent:
		// One result does not end the tool phase while another call is still open (a parallel
		// batch); today's loop dispatches sequentially, so this normally falls straight through
		// to thinking — the model has the result and is deciding what to do with it.
		if openCall {
			break
		}
		m.foldSlot(runOf(e.EventBase), activity{kind: actThinking})
	case domain.MessageEvent:
		// A completed message does not mean idle: the loop may keep stepping (a tool turn
		// follows a narration). finishWorker is what decides the exchange is over.
		m.foldSlot(runOf(e.EventBase), activity{kind: actThinking})
	case domain.SubAgentPhaseEvent:
		// The delegation is OVER: its slot goes, and the top-level phrase falls back to whatever is
		// still running — a sibling, the merged count, or the parent's own word (runningPhrase).
		// Its start is deliberately not folded: a child that has produced nothing has nothing to
		// say, and its first real event opens the slot.
		if e.Phase == domain.SubAgentFinished {
			m.acts.drop(runOf(e.EventBase))
		}
	}
	return m
}
