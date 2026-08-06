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

// statusTargetCells caps a tool target in the status line, in the CELLS the screen spends on it
// (toolPhrase measures it through the width authority — width.go). It is far tighter than
// clipDetail's transcript cap: the left slot shares one row with the context gauge, so a long
// path or a pasted command must not push the gauge off the line. The gap < 1 truncation in
// statusLine stays the floor for a window too narrow even for this.
const statusTargetCells = 32

// toolActivityLabel builds the actTool phrase for a call from the presentation registry: the
// tool's active verb and, when the call names one, the target it acts on ("reading · main.go",
// "running · npm test"). An unregistered (dynamic MCP) tool inherits presentToolCall's
// "running <raw name>" fallback, so it is still a truthful fragment.
//
// It needs no escape-stripping and no path-shortening of its own: the phrase is built ONLY from
// presentToolCall's view, which leaves that function through finishDisplay, so the status line
// inherits the tool card's seam rather than re-deriving the discipline — and reads the same
// workspace-relative path the block beneath it will. That matters here more than it looks —
// foldActivity paints this label the moment a call is ANNOUNCED, before any approval gate runs, so
// it is the earliest point at which a hostile model's argument reaches the screen. It also buys the
// left slot its width back: statusTargetCells clips at 32 cells, which a project-relative path fits
// and an absolute one routinely did not.
//
// measure is the width authority the cap is spent through (toolPhrase), threaded in rather than
// hard-wired so the budget is counted in the measure the painter is actually using (width.go).
func toolActivityLabel(measure widthAuthority, call domain.ToolCall, ws workspaceRoot) string {
	return toolPhrase(measure, presentToolCall(call, ws))
}

// toolPhrase is the composition itself: a call's view worded as the sentence fragment naming what
// it is doing right now. It is split out from toolActivityLabel because a COLLAPSED sub-agent run
// says the same thing about the call its span has open (subAgentGist, render.go) and must not word
// it a second way — there is ONE phrase for "what is happening", wherever it is shown, so a change
// to the wording moves the status line and the run's summary together.
//
// It takes the view rather than the call, since the transcript's own entries carry views already
// sanitized by presentToolCall; toolActivityLabel is the seam that builds one from a raw call.
func toolPhrase(measure widthAuthority, tv toolView) string {
	if tv.Target == "" {
		return tv.Verb
	}
	// The cap is spent in CELLS, through the width authority (width.go) — the painter's own measure,
	// so the budget the slot promises is the budget the screen bills. Spent in RUNES it was no budget
	// at all: a double-width glyph is one rune the screen pays two cells for, so a 32-rune CJK path
	// painted up to 64 cells, statusLeft truthfully truncated that over-wide phrase against the whole
	// window, and the gauge this cap exists to keep on an 80-column row went off it. The ellipsis is
	// spent INSIDE the budget (Truncate's tail), so the clipped target totals at most
	// statusTargetCells cells rather than the cap plus one more.
	//
	// The target is EXPANDED before the cap counts it (expandTabs, render.go) and stays so: statusLeft
	// composes the phrase through a style, which rewrites the tab into its four spaces before
	// th.measure reads the result, and the gist's line is wrapped by wrapText, which settles its own
	// tabs — so no tab ever reaches the screen, and the cap must count the form that does.
	return tv.Verb + " · " + measure.Truncate(expandTabs(tv.Target), statusTargetCells, "…")
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
func (m Model) foldActivity(e domain.Event, openCall bool) Model {
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
		m.setActivity(actTool, toolActivityLabel(m.th.measure, e.Call, m.transcript.ws), e.Depth)
	case domain.ToolResultEvent:
		// One result does not end the tool phase while another call is still open (a parallel
		// batch); today's loop dispatches sequentially, so this normally falls straight through
		// to thinking — the model has the result and is deciding what to do with it.
		if openCall {
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
