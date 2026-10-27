package agent

import (
	"context"
	"slices"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// sessionBriefing is the module that briefs a session before its first prompt (ADR 0076 D6): it
// owns one Session boundary's PRE-FIRST-PROMPT window and the session-start advise lane, and it
// makes the observe Runner's session-started calls. The Agent drives it through four events only —
// a boundary armed its entries (boundaryArmed: construction, ClearContext, RestoreSession), a live
// swap armed more (liveArmed: SetReactions), an input arrived (inputArrived: Submit, Wake, step),
// and the Agent closed (close) — and keeps the choice of what to hand it: the isDelegate gate, the
// specs Bypass leaves on and the observe ids its generation holds, read under genMu before the call.
// The module never takes genMu, so the lock order stays swapMu → genMu(R) → mu → lane.mu.
//
// The window is the span from a boundary's session-started announcement to the session's first
// user message (Submit, or the Wake that opens an Exchange without one) or, for a session restored
// mid-Exchange or with its input still queued, to the first Step that continues it (Agent.step) —
// or to Agent.Close, whichever comes first: a closed Agent's window stays shut for both lanes. A
// session-started entry a live swap arms inside it runs for the session it joined — an advise entry
// fenced at the first opening, an observe entry fired through the Runner; one armed after it waits
// for the next boundary.
//
// fired is the boundary's fired-id set: the ids of the session-started entries the boundary ran —
// the advise runs it started and the observe entries it reached — plus every id run since through a
// live swap. "Newly armed" is read against it, never against the generation the swap replaced, so
// removing an entry and adding it back inside the window does not run it twice. The window is per
// boundary: each boundary reopens it with a fresh set, and the zero value — a constructed Agent
// before its announcement, a delegate — is shut. It opens with or without an observe Runner: the
// advise lane is the engine's own.
//
// mu guards the window, and a boundary holds it across the lane's replace and the window's opening,
// so an input sees the boundary wholly before or wholly after: never the boundary's runs pending in
// a window still shut, which a later live swap could not then join. The boundaries and the swaps
// hold swapMu as well, but the first user message arrives from whatever goroutine submits.
type sessionBriefing struct {
	mu    sync.Mutex
	open  bool
	start domain.SessionStart
	turn  int
	fired map[string]bool

	// lane is the session-start advise lane (sessionadvise.go): the background advise runs the
	// current boundary started, plus the ones a live swap armed inside its window, joined by the
	// session's next Exchange opening.
	lane sessionAdviseLane

	// launch starts runs in the background under ctx for Turn turn (Agent.launchSessionAdvice). It
	// is bound by the top-level Agent's seed; a delegate's briefing opens no window and so never
	// calls it.
	launch func(ctx context.Context, turn int, runs []*sessionAdviseRun)
}

// sessionArmed is what a boundary or a live swap hands the briefing, read by the caller: the armed
// advise Reactions on session-started that Bypass leaves on, in ladder order, the ids of the
// session-started entries of the observe list the Runner holds, and that Runner — nil when the
// Agent holds none.
type sessionArmed struct {
	advise  []domain.Reaction
	observe []string
	runner  domain.ObserveRunner
}

// boundaryArmed is one Session boundary's announcement: it cancels the previous boundary's runs and
// starts armed's advise entries in the background, each under its own deadline, for the boundary
// announced with start at Turn turn, then reopens the window with every armed id — both lanes — as
// fired, and hands the session-started notice to the Runner. The lane's replace and the window's
// opening are one critical section under mu. It returns at once whatever the handlers do: the
// boundary never waits on an advise handler.
func (b *sessionBriefing) boundaryArmed(start domain.SessionStart, turn int, armed sessionArmed) {
	b.mu.Lock()
	runs := newSessionAdviseRuns(armed.advise, sessionStartedDoc(start))
	if ctx := b.lane.replace(runs); len(runs) > 0 {
		b.launch(ctx, turn, runs)
	}
	b.open, b.start, b.turn, b.fired = true, start, turn, map[string]bool{}
	for _, id := range append(sessionStartedIDs(armed.advise), armed.observe...) {
		b.fired[id] = true
	}
	b.mu.Unlock()

	if armed.runner != nil {
		armed.runner.SessionStarted(start)
	}
}

// liveArmed is a live swap's share: while the window is open it runs the entries armed names that
// have not yet run at this boundary — and only those, with the boundary's own start and Turn —,
// booking them as fired. An advise entry starts in the background and joins the boundary's pending
// runs, so the session's first opening fences its answer with the boundary's own; an observe entry
// fires through the Runner's narrowed door (ObserveRunner.SessionStartedFor). It does nothing while
// the window is shut or when every id has run. It runs under mu, so an input arriving concurrently
// waits for it: whatever it puts in place is there before the session's first Step, which reports
// its input before it joins its opening. It therefore only enqueues.
func (b *sessionBriefing) liveArmed(armed sessionArmed) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.open {
		return
	}
	var fresh []string
	for _, id := range append(sessionStartedIDs(armed.advise), armed.observe...) {
		if !b.fired[id] {
			b.fired[id] = true
			fresh = append(fresh, id)
		}
	}
	if len(fresh) == 0 {
		return
	}

	advise := slices.DeleteFunc(armed.advise, func(r domain.Reaction) bool { return !slices.Contains(fresh, r.ID) })
	if len(advise) > 0 {
		runs := newSessionAdviseRuns(advise, sessionStartedDoc(b.start))
		b.launch(b.lane.extend(runs), b.turn, runs)
	}
	observe := slices.DeleteFunc(armed.observe, func(id string) bool { return !slices.Contains(fresh, id) })
	if armed.runner != nil && len(observe) > 0 {
		armed.runner.SessionStartedFor(b.start, observe)
	}
}

// inputArrived ends the window: the session has sent its first user message, or a Step has
// continued a restored one. Ending a shut window is a no-op.
func (b *sessionBriefing) inputArrived() {
	b.shut()
}

// close is the Agent's shutdown: it shuts the window before it stops the lane, so an in-flight live
// arm — which holds mu — is waited out first and the stop that follows cancels whatever it
// enqueued, and no later swap arms a session-started entry for either lane.
func (b *sessionBriefing) close() {
	b.shut()
	b.lane.stop()
}

// shut ends the window and drops its fired-id set.
func (b *sessionBriefing) shut() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.open, b.fired = false, nil
}
