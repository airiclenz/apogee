package tui

// engineHold names one thing that can hold the engine away from the Update loop. Holders are not
// exclusive — a launcher verb can be in flight while a worker runs, a record write can queue
// behind a /bg launch — so the record of who holds the engine is a SET of these bits, never a
// single-valued state.
type engineHold uint8

const (
	// holdWorker: a worker drives the engine — an Exchange runs, or blocks on an approval or an
	// ask_user answer ([Model.busy]).
	holdWorker engineHold = 1 << iota
	// holdActuation: a launcher verb is in flight and the server the engine dials is restarting
	// under it (the actuation latch, ADR 0029).
	holdActuation
	// holdBgLaunch: a /bg launch is reading the Agent off the loop (foldBgStarted releases it).
	holdBgLaunch
	// holdSessionLoad: a /sessions load is restoring the engine.
	holdSessionLoad
	// holdRecordWrite: a record write or fork is in flight or queued behind one — each snapshots
	// the engine at idle.
	holdRecordWrite
	// holdQuitting: a quit whose exit is deferred to a worker's end or to the closing flush.
	holdQuitting
	// holdPrebound: the session has no engine bound yet ([Model.prebound]).
	holdPrebound
)

// The hold groups the questions below read. Each is named once so two questions that ask the same
// thing cannot drift apart.
const (
	// holdsOwningEngine are the holds under which the engine or the server it dials belongs to
	// someone else right now: a beat failure says nothing, a config edit could be read mid-run,
	// and no Firing may start.
	holdsOwningEngine = holdWorker | holdActuation
	// holdsTakingAgent are the holds under which a verb driven now would race something reading
	// or driving the Agent: an idle-only command or a second /bg launch waits for them.
	holdsTakingAgent = holdWorker | holdBgLaunch
	// holdsBlockingRebind are the holds under which Agent.Rebind, idle-only by construction, must
	// be stashed for the boundary rather than applied.
	holdsBlockingRebind = holdsOwningEngine | holdBgLaunch
	// holdsBlockingResume are the holds an idle-only resume or wake waits out: no engine yet, an
	// exit under way, or an idle-only operation still in flight.
	holdsBlockingResume = holdPrebound | holdQuitting | holdSessionLoad | holdBgLaunch | holdRecordWrite
)

// engineHoldSources maps each hold to the Model field (or predicate) it is read from — the one
// place a hold's meaning meets the Model's storage.
var engineHoldSources = [...]struct {
	hold   engineHold
	isHeld func(Model) bool
}{
	{holdWorker, Model.busy},
	{holdActuation, func(m Model) bool { return m.actuation.inFlight }},
	{holdBgLaunch, func(m Model) bool { return m.bgLaunching }},
	{holdSessionLoad, func(m Model) bool { return m.sessionLoading }},
	{holdRecordWrite, func(m Model) bool { return m.writeBusy || len(m.pendingWrites) > 0 }},
	{holdQuitting, func(m Model) bool { return m.quitting }},
	{holdPrebound, Model.prebound},
}

// engineHolds is a snapshot of who holds the engine — the hold set — together with the non-hold
// facts the engine gates also read. It is a value: the questions on it answer for the moment
// [Model.engineHolds] took it, and nothing writes back through it.
type engineHolds struct {
	held engineHold

	// isIdle reports stateIdle. stateErrored is NOT idle here — wake and resume wait for its
	// dismissal — though no worker holds the engine there, so the quiescent question counts it idle.
	isIdle bool
	// isConfirmOpen reports the workflow-boundary stop-or-keep confirm is up.
	isConfirmOpen bool
	// hasInterjections reports a held or staged message is waiting to go out.
	hasInterjections bool
	// hasQueuedCommands reports an idle-only /command typed mid-run is waiting for the next idle.
	hasQueuedCommands bool
	// isModalPaneOpen reports a pane that owns the keyboard is up ([Model.modalPaneOpen]).
	isModalPaneOpen bool
}

// engineHolds takes the snapshot the engine gates are asked through: the hold set read from the
// Model's own fields (engineHoldSources) plus the non-hold facts beside it.
func (m Model) engineHolds() engineHolds {
	var held engineHold
	for _, source := range engineHoldSources {
		if source.isHeld(m) {
			held |= source.hold
		}
	}
	return engineHolds{
		held:              held,
		isIdle:            m.state == stateIdle,
		isConfirmOpen:     m.boundaryConfirmOpen(),
		hasInterjections:  len(m.pendingInterjections) > 0,
		hasQueuedCommands: len(m.deferredCommands) > 0,
		isModalPaneOpen:   m.modalPaneOpen(),
	}
}

// holding reports whether any hold in group is held.
func (h engineHolds) holding(group engineHold) bool {
	return h.held&group != 0
}

// commandRunnable reports whether parsed's verb may be driven now: every verb while nothing takes
// the Agent, and only the reporting lines (parsedInput.safeWhileRunning) while a worker or a /bg
// launch does. The name is [Model.commandRunnable]'s (ADR 0027 decision 6).
func (h engineHolds) commandRunnable(parsed parsedInput) bool {
	return !h.holding(holdsTakingAgent) || parsed.safeWhileRunning()
}

// canRunDeferred reports whether the queued idle-only commands may be drained now: nothing takes
// the Agent, the program is not exiting, and the stop-or-keep confirm is not waiting on an answer.
func (h engineHolds) canRunDeferred() bool {
	return !h.holding(holdsTakingAgent|holdQuitting) && !h.isConfirmOpen
}

// canRebind reports whether an observed rebind may be applied now rather than stashed for the
// boundary that releases the engine.
func (h engineHolds) canRebind() bool {
	return !h.holding(holdsBlockingRebind)
}

// beatMayCount reports whether a failed beat counts toward the offline crossing: not while a
// worker's stream may be holding a single-slot server, nor while an actuation is restarting it.
func (h engineHolds) beatMayCount() bool {
	return !h.holding(holdsOwningEngine)
}

// canEditConfigExternally reports whether an external config edit may be launched now: not while a
// run would read the config mid-flight.
func (h engineHolds) canEditConfigExternally() bool {
	return !h.holding(holdsOwningEngine)
}

// canLaunchBg reports whether a /bg launch may start now: nothing else takes the Agent.
func (h engineHolds) canLaunchBg() bool {
	return !h.holding(holdsTakingAgent)
}

// quiescent reports whether nothing this session owns is in flight, the fact the Gate holds a due
// Firing on ([Model.quiescent], ADR 0033). It deliberately ignores the /bg launch and session-load
// holds (ADR 0033 decision 5), and stateErrored with nothing queued counts as quiescent.
func (h engineHolds) quiescent() bool {
	return !h.holding(holdsOwningEngine) && !h.hasInterjections && !h.hasQueuedCommands
}

// canResumeWorkflows reports whether the engine is the loop's to resume workflows on: idle, and no
// hold an idle-only operation waits out.
func (h engineHolds) canResumeWorkflows() bool {
	return h.isIdle && !h.holding(holdsBlockingResume)
}

// canWake reports whether the engine is the loop's to wake: the resume conditions, plus no held
// message (the human's next ⏎ sends it) and no modal pane the human is answering.
func (h engineHolds) canWake() bool {
	return h.canResumeWorkflows() && !h.hasInterjections && !h.isModalPaneOpen
}
