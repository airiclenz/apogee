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
// transcript folds (beside foldStats in the eventMsg case), and the handful of transitions no
// Event announces — a submit, /compact, a stop, the worker's terminal Msg — set it directly.
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
// renders. actTool is the one kind that carries a payload (the label naming the tool and its
// target); every other kind renders a fixed word.
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

// activity is the status line's live left slot: what is happening, since when, and at which
// sub-agent nesting depth. label is used by actTool only; since is when THIS activity began,
// so the elapsed clock measures the current phrase rather than the whole exchange.
type activity struct {
	kind  activityKind
	label string    // actTool only: "<verb> · <clipped target>", or the bare verb when there is none
	depth int       // > 0 → the phrase is prefixed with subAgentLabel (a sub-agent is acting)
	since time.Time // when this activity began — the elapsed clock's origin
}

// text renders the activity as the status line's unstyled phrase. Idle says nothing at all —
// the input box below already invites a message, so a word there would be noise. A phrase from
// a sub-agent (Depth > 0) is prefixed with the same subAgentLabel the transcript rail uses, so
// "sub-agent · searching" reads as one sentence fragment at any nesting level.
func (a activity) text() string {
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

// statusTargetRunes caps a tool target in the status line. It is far tighter than
// clipDetail's transcript cap: the left slot shares one row with the context gauge, so a long
// path or a pasted command must not push the gauge off the line. The gap < 1 truncation in
// statusLine stays the floor for a window too narrow even for this.
const statusTargetRunes = 32

// toolActivityLabel builds the actTool phrase for a call from the presentation registry: the
// tool's active verb and, when the call names one, the target it acts on ("reading · main.go",
// "running · npm test"). An unregistered (dynamic MCP) tool inherits presentToolCall's
// "running <raw name>" fallback, so it is still a truthful fragment.
func toolActivityLabel(call domain.ToolCall) string {
	tv := presentToolCall(call)
	if tv.Target == "" {
		return tv.Verb
	}
	return tv.Verb + " · " + clipRunes(tv.Target, statusTargetRunes)
}

// setActivity moves the model to a new activity. The elapsed clock restarts only when the
// rendered phrase actually changes (kind or label) — a stream of TokenEvents must keep one
// running clock, not reset it on every chunk. Depth alone does not restart it: the phrase's
// sub-agent prefix changes, but the same work is still in flight.
func (m *Model) setActivity(kind activityKind, label string, depth int) {
	if m.act.kind != kind || m.act.label != label {
		m.act.since = time.Now()
	}
	m.act.kind = kind
	m.act.label = label
	m.act.depth = depth
}

// foldActivity derives the live activity from one engine Event (the eventMsg fold, beside
// foldStats). It must run AFTER transcript.apply: the ToolResultEvent rule reads the
// transcript's call/result pairing, which apply is what establishes.
//
// Events that say nothing about what the worker is doing next — an error notice, usage
// accounting, an audit record, a fired mechanism, an approval record — leave the activity
// alone, so the phrase does not flicker off the work actually in flight.
//
// stopping is STICKY: once Esc has fired the cancel the worker keeps emitting events until it
// reaches a quiescent boundary, and overwriting the phrase there would tell the human their
// stop was ignored. Only finishWorker clears it, when the worker has actually unwound.
func (m Model) foldActivity(e domain.Event) Model {
	if m.act.kind == actStopping {
		return m
	}
	switch e := e.(type) {
	case domain.ReasoningEvent:
		// The honest "thinking": the model is reasoning, not merely unanswered.
		m.setActivity(actThinking, "", e.Depth)
	case domain.TokenEvent:
		m.setActivity(actResponding, "", e.Depth)
	case domain.StreamResetEvent:
		m.setActivity(actRetrying, "", e.Depth)
	case domain.ToolCallEvent:
		m.setActivity(actTool, toolActivityLabel(e.Call), e.Depth)
	case domain.ToolResultEvent:
		// One result does not end the tool phase while another call is still open (a parallel
		// batch); today's loop dispatches sequentially, so this normally falls straight through
		// to thinking — the model has the result and is deciding what to do with it.
		if m.transcript.hasOpenToolCall() {
			break
		}
		m.setActivity(actThinking, "", e.Depth)
	case domain.MessageEvent:
		// A completed message does not mean idle: the loop may keep stepping (a tool turn
		// follows a narration). finishWorker is what decides the exchange is over.
		m.setActivity(actThinking, "", e.Depth)
	}
	return m
}
