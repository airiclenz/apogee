package agent

import (
	"context"
	"slices"
	"sync"

	"github.com/airiclenz/apogee/internal/domain"
)

// sessionBriefing is the module that briefs a session (ADR 0076 D6): it owns one Session boundary's
// PRE-FIRST-PROMPT window, both advise lanes — the session-start lane and the compaction lane — and
// their runs, and it makes the observe Runner's session-started and compaction-finished calls. The
// Agent drives it through its events only — a boundary armed its entries (boundaryArmed:
// construction, ClearContext, RestoreSession), a live swap armed more (liveArmed: SetReactions), an
// input arrived (inputArrived: Submit, Wake, step), a fold began and ended (foldBegan, foldEnded:
// foldFor), a user-role message entered the request (joinOpening: step's opening; joinBridge:
// refold's overflow bridge), an Exchange closed or was aborted (exchangeClosed, exchangeAborted),
// and the Agent closed (close) — and keeps the choice of what to hand it: the isDelegate gate, the
// specs Bypass leaves on and the observe ids its generation holds, read under genMu before the call,
// and the landing that turns a joined run into the conversation's advice and the Driver's events.
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

	// compaction is the compaction lane (sessionadvise.go): the background advise runs the last
	// top-level fold that ran started, joined by the next user-role message — the next Exchange's
	// opening, or the overflow bridge of a mid-Exchange fold. A boundary and close discard it.
	compaction sessionAdviseLane

	// call is the handler call every run makes (Agent.callSessionAdvice), injected by the top-level
	// Agent's seed. A delegate's briefing opens no window and stages no fold, so it never calls it.
	call adviseCaller
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

// adviseCaller calls one run's handler, under ctx, for Turn turn, with doc — the run's seam
// document — and answers what the handler answered: the Outcome's Inject is the advice, empty is
// silence. Production's is Agent.callSessionAdvice.
type adviseCaller func(ctx context.Context, turn int, doc domain.SeamPayload, r domain.Reaction) (domain.Outcome, error)

// adviceLanding lands one joined run on the user-role message that just entered the request —
// the Agent's attachSessionAdvice, or nothing under Bypass — and reports whether the run answered:
// false for a run it reported (timed out, errored, panicked), which the join then drops for good.
type adviceLanding func(run *sessionAdviseRun) bool

// stagedFold is one fold's share of the compaction lane between foldBegan and foldEnded: the
// trigger's spelling and Turn its compaction-finished notice carries, the observe Runner that
// notice goes to — nil when the Agent holds none — and the advise runs the fold started, staged on
// a lane of their own until the fold's end decides their fate.
type stagedFold struct {
	fold   domain.CompactionFold
	turn   int
	runner domain.ObserveRunner
	runs   sessionAdviseLane
}

// boundaryArmed is one Session boundary's announcement: it discards the compaction lane's runs,
// cancels the previous boundary's session-start runs and
// starts armed's advise entries in the background, each under its own deadline, for the boundary
// announced with start at Turn turn, then reopens the window with every armed id — both lanes — as
// fired, and hands the session-started notice to the Runner. The lane's replace and the window's
// opening are one critical section under mu. It returns at once whatever the handlers do: the
// boundary never waits on an advise handler.
func (b *sessionBriefing) boundaryArmed(start domain.SessionStart, turn int, armed sessionArmed) {
	b.compaction.stop() // the history the last fold's runs would re-brief is gone with the boundary
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

// close is the Agent's shutdown: it shuts the window before it stops the session-start lane, so an
// in-flight live arm — which holds mu — is waited out first and the stop that follows cancels
// whatever it enqueued, and no later swap arms a session-started entry for either lane; then it
// stops the compaction lane, cancelling the runs the last fold started.
func (b *sessionBriefing) close() {
	b.shut()
	b.lane.stop()
	b.compaction.stop()
}

// shut ends the window and drops its fired-id set.
func (b *sessionBriefing) shut() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.open, b.fired = false, nil
}

// foldBegan is a fold's start: it starts advise — the armed advise Reactions on compaction-finished
// that Bypass leaves on, read by the caller — in the background, each under its own deadline, with
// fold on its document, and returns them staged, apart from the compaction lane, for foldEnded to
// adopt or discard. The handlers run while the summary call does; foldBegan returns at once
// whatever they do. A delegate's fold re-briefs no user entry, so the Agent stages none.
func (b *sessionBriefing) foldBegan(fold domain.CompactionFold, turn int, advise []domain.Reaction, runner domain.ObserveRunner) *stagedFold {
	staged := &stagedFold{fold: fold, turn: turn, runner: runner}
	doc := domain.SeamPayload{Event: domain.MomentCompactionFinished, Fold: fold}
	runs := newSessionAdviseRuns(advise, doc)
	b.launch(staged.runs.replace(runs), turn, runs)
	return staged
}

// foldEnded is a fold's end. A fold that RAN hands its staged runs to the compaction lane — the
// context they run under included — in place of an earlier fold's, for the next user-role message
// to join, and hands the compaction-finished notice to the Runner's fold door
// (ObserveRunner.CompactionFinished) with the trigger's spelling and the fold's Turn (ADR 0076,
// adr-0076-amend-1021b); the door is enqueue-only, so a fold never waits on a fired entry. A fold
// that faulted, was cancelled or folded nothing discards its staged runs and announces nothing. A
// nil staged — a delegate's fold — does nothing.
func (b *sessionBriefing) foldEnded(staged *stagedFold, ran bool) {
	if staged == nil {
		return
	}
	if !ran {
		staged.runs.stop()
		return
	}
	b.compaction.adopt(&staged.runs)
	if staged.runner != nil {
		staged.runner.CompactionFinished(staged.fold, staged.turn)
	}
}

// joinOpening is the opening's join, run by step right after an Exchange's opening is appended: it
// joins the session-start lane's runs and then the compaction lane's — a fold between Exchanges
// re-briefs the model at the opening that follows it — onto the opening, landing each with land
// (join).
func (b *sessionBriefing) joinOpening(ctx context.Context, land adviceLanding) {
	join(ctx, land, &b.lane, &b.compaction)
}

// joinBridge is the mid-Exchange fold's join, run by refold right after a fold that ran appended
// the overflow bridge: it joins the compaction lane's runs onto the bridge, ahead of its fixed text,
// so the retried request carries the re-brief (join).
func (b *sessionBriefing) joinBridge(ctx context.Context, land adviceLanding) {
	join(ctx, land, &b.compaction)
}

// exchangeClosed drops the runs an Exchange that closed with its opening kept had taken, on both
// lanes: their advice reached the conversation and is never delivered again.
func (b *sessionBriefing) exchangeClosed() {
	b.lane.forgetDelivered()
	b.compaction.forgetDelivered()
}

// exchangeAborted puts the runs an aborted Exchange's opening or overflow bridge took that answered
// back ahead of pending, on both lanes, so the next opening carries their advice instead; a run the
// join reported was dropped there and stays gone.
func (b *sessionBriefing) exchangeAborted() {
	b.lane.restoreDelivered()
	b.compaction.restoreDelivered()
}

// launch starts each of runs in its own goroutine under ctx — the lane's, which the next boundary or
// fold, a discarding fold, or close cancels.
func (b *sessionBriefing) launch(ctx context.Context, turn int, runs []*sessionAdviseRun) {
	for _, run := range runs {
		go b.run(ctx, turn, run)
	}
}

// run is one run's goroutine: the handler called (call) under the run's deadline, its answer
// recorded for the join. A panic is recovered here — a goroutine's panic would end the process —
// and recorded for the join to report, since nothing may be emitted off the loop goroutine.
func (b *sessionBriefing) run(ctx context.Context, turn int, run *sessionAdviseRun) {
	defer close(run.done)
	defer func() {
		if recovered := recover(); recovered != nil {
			run.panicked = recovered
		}
	}()
	ctx, cancel := context.WithDeadline(ctx, run.deadline)
	defer cancel()
	run.out, run.err = b.call(ctx, turn, run.doc, run.spec)
}
