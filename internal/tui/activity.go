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
// This file is pure — no lipgloss, no I/O — the toolpresent.go discipline, so the whole
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

// text renders the activity as the status line's unstyled phrase. Idle says nothing at all —
// the input box below already invites a message, so a word there would be noise. A phrase from
// a sub-agent (Depth > 0) is prefixed with its identity, so "sub-agent · searching" reads as one
// sentence fragment at any nesting level.
//
// name is the acting delegation's short name, resolved by the caller (runningPhrase, against
// transcript.runName) and "" when the delegation was given none. A named child takes its name in
// place of the generic word — "repo-scout · reading" — because with a fan-out running
// the slot can name only one delegate at a time and "sub-agent" says nothing about which. An
// unnamed one keeps the subAgentLabel the transcript rail uses, to the byte.
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
	}
	if phrase == "" {
		return "" // an actTool with no label (a tool with neither verb nor target): say nothing
	}
	if a.depth > 0 {
		if name != "" {
			return name + " · " + phrase
		}
		return subAgentLabel + " · " + phrase
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

// statusTargetCells caps a tool target in the CELLS the screen spends on it (toolPhrase measures
// it through the width authority — width.go). It is far tighter than clipDetail's transcript cap:
// the phrase it bounds rides a COLLAPSED run's single summary line (subAgentGist, render.go),
// beside that run's own call count and context fill, so a long path or a pasted command must not
// crowd them off it.
const statusTargetCells = 32

// toolActivityVerb builds the actTool phrase for a call from the presentation registry: the tool's
// active verb and nothing else ("reading", "running"). An unregistered (dynamic MCP) tool inherits
// presentToolCall's "running <raw name>" fallback, so it is still a truthful fragment.
//
// The TARGET is deliberately absent. The status line shares one row with the context gauge, and the
// path it used to carry was both the thing that pushed the gauge off that row and a restatement of
// the tool-call block already on screen a line below — so the slot keeps the half only it can say
// (what is happening right now) and leaves the target to the block. toolPhrase still words the pair
// for the one surface that has no block to read: a collapsed sub-agent run's gist.
//
// It needs no escape-stripping of its own: the phrase is built ONLY from presentToolCall's view,
// which leaves that function through finishDisplay, so the status line inherits the tool card's
// seam rather than re-deriving the discipline. That matters here more than it looks — foldActivity
// paints this label the moment a call is ANNOUNCED, before any approval gate runs, so it is the
// earliest point at which a hostile model's argument reaches the screen.
func toolActivityVerb(call domain.ToolCall, ws workspaceRoot) string {
	return presentToolCall(call, ws).Verb
}

// toolPhrase words a call's view as the sentence fragment naming what it is doing right now, target
// and all ("reading · main.go", "running · npm test"). It belongs to the COLLAPSED sub-agent run's
// gist (subAgentGist, render.go) and to that alone: inside a collapsed run the gist is the only live
// view of what the child is touching, so it keeps the target the status line sheds
// (toolActivityVerb).
//
// It takes the view rather than the call, since the transcript's own entries carry views already
// sanitized by presentToolCall.
func toolPhrase(measure widthAuthority, tv toolView) string {
	if tv.Target == "" {
		return tv.Verb
	}
	// The cap is spent in CELLS, through the width authority (width.go) — the painter's own measure,
	// so the budget the slot promises is the budget the screen bills. Spent in RUNES it was no budget
	// at all: a double-width glyph is one rune the screen pays two cells for, so a 32-rune CJK path
	// painted up to 64 cells and the row carrying it was truthfully truncated whole against the
	// window. The ellipsis is spent INSIDE the budget (Truncate's tail), so the clipped target totals
	// at most statusTargetCells cells rather than the cap plus one more.
	//
	// The target is EXPANDED before the cap counts it (expandTabs, render.go) and stays so: the gist's
	// line is wrapped by wrapText, which settles its own tabs — so no tab ever reaches the screen, and
	// the cap must count the form that does.
	return tv.Verb + " · " + measure.Truncate(expandTabs(tv.Target), statusTargetCells, "…")
}

// setActivity moves the model to a new activity that is not an open tool call, keyed on the phrase
// it renders (kind and label). Tool calls go through setToolActivity, which carries the identity
// their clock is keyed on instead.
func (m *Model) setActivity(kind activityKind, label string, depth int, spawn string) {
	m.moveActivity(activity{kind: kind, label: label, depth: depth, spawn: spawn})
}

// setToolActivity moves the model to the tool call an announcement opened: verb is the phrase the
// slot renders (toolActivityVerb) and call is the id of the call it describes (domain.ToolCall.ID),
// which is what the elapsed clock is keyed on — a second read of a second file is a new call and
// deserves a clock of its own even though both phrases read "reading".
func (m *Model) setToolActivity(verb, call string, depth int, spawn string) {
	m.moveActivity(activity{kind: actTool, label: verb, call: call, depth: depth, spawn: spawn})
}

// moveActivity is the single seat of the elapsed clock's origin: next inherits the running clock
// unless what it says the worker is doing actually changed — its kind, its phrase, or the tool call
// it names. A stream of TokenEvents must keep one running clock, not reset it on every chunk, and
// the same holds for the repeated announcement of one call. Depth and spawn alone do not restart it
// either: the phrase's sub-agent prefix changes, but the same work is still in flight.
//
// next.since is ignored — callers state what is happening, never when it started.
func (m *Model) moveActivity(next activity) {
	next.since = m.act.since
	if m.act.kind != next.kind || m.act.label != next.label || m.act.call != next.call {
		next.since = time.Now()
	}
	m.act = next
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
// stopping is STICKY: once Esc has fired the cancel the worker keeps emitting events until it
// reaches a quiescent boundary, and overwriting the phrase there would tell the human their
// stop was ignored. Only finishWorker clears it, when the worker has actually unwound.
//
// Every arm carries the emitting agent's SPAWNING call id beside its depth, and reads it as
// e.EventBase.CallID rather than e.CallID: a variant that also reports a call of its own names it
// CallID too and therefore shadows the embedded field (domain.AuditEvent — not folded here, but the
// spelling must not depend on which arm you are in). It is the id the delegation's name is looked
// up by; empty at depth 0, where nothing spawned the emitter.
func (m Model) foldActivity(e domain.Event, openCall bool) Model {
	if m.act.kind == actStopping {
		return m
	}
	switch e := e.(type) {
	case domain.ReasoningEvent:
		// The honest "thinking": the model is reasoning, not merely unanswered.
		m.setActivity(actThinking, "", e.Depth, e.EventBase.CallID)
	case domain.TokenEvent:
		m.setActivity(actResponding, "", e.Depth, e.EventBase.CallID)
	case domain.StreamResetEvent:
		m.setActivity(actRetrying, "", e.Depth, e.EventBase.CallID)
	case domain.ToolCallEvent:
		m.setToolActivity(toolActivityVerb(e.Call, m.transcript.ws), e.Call.ID, e.Depth, e.EventBase.CallID)
	case domain.ToolResultEvent:
		// One result does not end the tool phase while another call is still open (a parallel
		// batch); today's loop dispatches sequentially, so this normally falls straight through
		// to thinking — the model has the result and is deciding what to do with it.
		if openCall {
			break
		}
		m.setActivity(actThinking, "", e.Depth, e.EventBase.CallID)
	case domain.MessageEvent:
		// A completed message does not mean idle: the loop may keep stepping (a tool turn
		// follows a narration). finishWorker is what decides the exchange is over.
		m.setActivity(actThinking, "", e.Depth, e.EventBase.CallID)
	}
	return m
}
