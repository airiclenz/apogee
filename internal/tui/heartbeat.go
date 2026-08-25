package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// The upstream heartbeat (ADR 0024)
// ----------------------------------------------------------------------------

// heartbeatState is the Model's live view of the Upstream monitor: which tick chain is current,
// how many consecutive idle beats have failed, whether the footer currently says offline, whether
// any beat has ever landed, why the last one did not, what the server advertises, and what the
// last beat observed as served. It holds plain values and one slice of plain values, so it copies
// safely with the Model (ADR 0011).
//
// It is display and policy state only — no binding moves here. The engine's per-model bindings are
// re-resolved by the composition root through a separate seam, at a quiescent boundary.
type heartbeatState struct {
	// gen is the live tick chain's generation; a Msg carrying any other one is inert. 0 means no
	// chain (the monitor is unwired).
	gen int
	// failures counts consecutive failed IDLE beats since the last success — the offline debounce's
	// evidence. A landed beat resets it; a failure while an Exchange runs leaves it where it was.
	failures int
	// offline is what the footer says and what a submit is refused against.
	offline bool
	// everOnline records that at least one beat has landed. Before that there is nothing to weigh a
	// failure against, so the first one is believed immediately.
	everOnline bool
	// switched records that this session has moved to another server (/server). It is set by the
	// switch fold and never cleared — a session that has switched once is no longer a launch,
	// whatever it does afterwards — and its one job is to defeat the quiet first-contact seed: the
	// start-up box a launch's silence relies on is deep in the scrollback by then, and the human
	// asked for this move, so the new server's first bind is the answer to it.
	switched bool
	// lastFailure is the most recent failure's words, quoted in the offline note and in the
	// refusal of a send. "" once a beat lands.
	lastFailure string
	// models is what the server last advertised — the /model picker's rows, derived from it at
	// render time (picker.go) so a beat landing under an open overlay refreshes the offering in
	// place rather than leaving the human choosing from a list the server has moved on from.
	models []heartbeat.ModelSummary
	// observedModel and observedWindow are what the last beat that mattered reported. They are the
	// baseline a model/window change is measured against; the rebind orchestration owns them.
	observedModel  string
	observedWindow int
	// observedDialect is the effort wire dialect that same beat reported for the SERVER (ADR 0060).
	// It is not part of the change TEST beside it — a dial is a fact about the server, so it moves
	// when the model or the window does and never on its own — it is what a rebind driven with no
	// beat of its own is built from: a `/model` pick re-states the binding, and re-stating it with a
	// zero dialect would silently un-dial a session the heartbeat had already dialled.
	observedDialect provider.EffortDialect
	// pendingRebind is a captured change waiting for the engine to be quiescent — set when a beat
	// lands while a worker owns the engine (applied in finishWorker) or while a launcher verb owns
	// the server it talks to (applied in foldActuationDone). Latest-wins: a second change inside the
	// same window replaces the first, so only the newest reality is ever bound. nil ⇒ nothing is
	// deferred. A pointer into a value-copied Model is safe because it is only ever replaced, never
	// written through (the pendingSave posture, ADR 0011).
	pendingRebind *rebindIntent
	// lastRebindFailed is the model id whose rebind last failed, so a refusal is noted once per
	// distinct target instead of once every Interval. "" once a rebind succeeds.
	lastRebindFailed string
}

// rebindIntent is one captured binding change: the model the last beat observed the server to be
// serving, the window it reported for it, the effort wire dialect it reported for the server, and
// whether the beat that saw it was the session's first
// contact. It is what the deferred apply stashes and what the [ServerHost.Rebind] seam is called
// with — plain values, so the value-copied Model carries it safely. It is the OBSERVATION, not the
// binding: what the binary makes of it (a pinned window outranking the observed one) comes back in
// the [RebindResult].
type rebindIntent struct {
	model  string
	window int
	// dialect is the wire shape the beat reported this SERVER reads a thinking-effort intent in
	// (ADR 0060) — the one effort fact that crosses into the engine, carried here so a change
	// stashed for the quiescent boundary binds the dialect the observation actually saw rather than
	// one re-read from a later beat. The zero value is "no dial advertised", which keeps the
	// historical `chat_template_kwargs` shape and so reproduces the request bytes that predate the
	// dialect seam; it is a plain value, so the value-copied Model carries it exactly as the two
	// fields above.
	dialect provider.EffortDialect
	// quietSeed records that this change was observed at FIRST CONTACT — no beat had ever landed
	// and none had ever failed. It is an observation FACT, captured before the fold erases the
	// evidence, rather than presentation state; [rebindNote] is what decides the wording it buys.
	quietSeed bool
}

// offlineFailureThreshold is how many consecutive idle beat failures flip the footer offline once
// a beat has ever landed. One failure is not evidence of an absent server: discovery's own timeout
// can elapse on a server that is merely saturated, and a footer that flickers offline mid-session
// would be worse than useless. Two (~15–25 s after the server actually went away) is the owner's
// debounce. Before any beat has landed there is nothing to weigh against, so a cold start says so
// on the first failure — see foldBeatFailure.
const offlineFailureThreshold = 2

// onlineNote and offlineNote word the two transitions, each recorded exactly once per crossing
// (the saveFailing fail-once posture): a heartbeat that noted every failed beat would fill the
// transcript with one line every ten seconds while a server is down.
const onlineNote = "server back online"

// offlineNote words the offline crossing, naming why the server could not be read when the monitor
// has words for it.
func offlineNote(failure string) string {
	if failure == "" {
		return "server offline"
	}
	return "server offline — " + failure
}

// observesUpstream reports whether anything watches this session's server ([ServerActs.CanObserve]).
// It is the one question every heartbeat gate asks, and it is asked rather than beaten for, because
// an unobserved session is not an offline one — no tick chain is opened, no beat is folded, nothing
// is refused, and the footer keeps what launch gave it.
func (m Model) observesUpstream() bool { return m.serverActs().CanObserve }

// appliesRebinds reports whether an observed change can be acted on at all ([ServerActs.CanRebind]).
// False is the display-frozen heartbeat: the beats still land and still light the offline state, but
// nothing captures a change and nothing claims a binding moved.
func (m Model) appliesRebinds() bool { return m.serverActs().CanRebind }

// heartbeatLive reports whether gen belongs to the current tick chain of a WIRED monitor — the
// guard both heartbeat Msgs pass through. An unwired Model (gen 0) folds nothing, so a stray beat
// can never flip a TUI that monitors nothing into an offline state it can never leave.
func (m Model) heartbeatLive(gen int) bool {
	return m.observesUpstream() && gen == m.hb.gen
}

// beatCmd runs one observation off the Update loop and reports it as a beatMsg stamped with the
// CURRENT generation. It captures the program context, so a shutdown cancels a beat still in
// flight, and the seam itself by value — an interface header, no pointer into the value-copied
// Model (the saveCmd posture). It returns nil when the monitor is unwired, which is what makes
// Init's tea.Batch collapse to the focus Cmd alone.
//
// It CONTINUES a chain and never opens one: Init issues the session's first beat on the generation
// newModel armed, and the tick fold issues the next one of the chain that scheduled the tick.
// Anything else firing an immediate beat wants [Model.armBeat], which retires the running chain
// first — one live chain per session is the invariant, and this func alone cannot keep it.
//
// A PRE-BOUND session issues none either, for the plainer reason that there is nothing to observe:
// the holder behind the seam has no Monitor until the bind installs one (ADR 0036 decision 3), so a
// beat would report an unreachable server and paint the session offline against an endpoint nobody
// has named yet. The chain opens with the bind's own armBeat instead.
func (m Model) beatCmd() tea.Cmd {
	if !m.observesUpstream() || m.prebound() {
		return nil
	}
	observe, ctx, gen := m.opts.Server, m.parent, m.hb.gen
	return func() tea.Msg {
		return beatMsg{gen: gen, beat: observe.Beat(ctx)}
	}
}

// armBeat opens a FRESH tick chain and returns its first beat, fired now rather than one Interval
// from now. It is what every caller that fires an immediate beat outside the chain's own rhythm uses
// — a committed server switch, a completed profile load — because arming is what RETIRES the chain
// already running: a beat issued on the CURRENT generation leaves the running chain's pending tick
// live beside the new one, and two chains poll the server at twice the Interval while halving the
// offline debounce (doc.go's tick-chain invariant, the spinner's doubled frame rate one level
// across). [Model.beatCmd] stays the CONTINUATION — the tick fold re-issues the current chain's next
// observation with it and must not bump.
//
// It takes a pointer because the generation bump must land on the Model copy the caller returns —
// which is also why every caller arms in a statement of its own: in `return m, m.armBeat()` the
// order of the bump and the copy of m is unspecified ([spinnerAnim.arm], ADR 0011).
func (m *Model) armBeat() tea.Cmd {
	m.hb.gen++
	return m.beatCmd()
}

// beatTick schedules the next beat one heartbeat.Interval after the beat that just landed. Timing
// the wait from the landing (rather than from a fixed clock) is what makes overlap impossible: the
// observation and its wait are strictly sequential, whatever the server's latency.
func (m Model) beatTick() tea.Cmd {
	gen := m.hb.gen
	return tea.Tick(heartbeat.Interval, func(time.Time) tea.Msg { return heartbeatTickMsg{gen: gen} })
}

// foldBeatMsg folds one landed observation of the Upstream and re-arms the chain from HERE —
// Interval after the beat landed, never on a fixed clock — so the next beat cannot overlap this one.
// A beat from a retired chain is inert, like a spinner tick.
func (m Model) foldBeatMsg(msg beatMsg) (tea.Model, tea.Cmd) {
	if !m.heartbeatLive(msg.gen) {
		return m, nil
	}
	next, noted := m.foldBeat(msg.beat)
	if noted {
		// Only a beat that MOVED something — the offline state, a binding, or the rows of an open
		// picker — lays out: a beat that changed nothing has nothing to draw, so re-rendering the
		// whole transcript every ten seconds would be work for its own sake. (It no longer costs a
		// live drag-selection: a repaint that appends a note leaves the spanned lines alone, and
		// refreshViewport's keep-if-unchanged rule keeps the selection through it. Economy, not
		// correctness.)
		//
		// layout() rather than a bare refreshViewport, because an offering that moved under an open
		// picker moved the pane's drawn height with it, and the viewport WIDGET's height IS the
		// transcript's drawn row count (layout(), model.go) — left stale, the scroll clamp strands
		// the tail under the pane.
		next.layout()
	}
	return next, next.beatTick()
}

// foldBeat folds one landed observation into the heartbeat state and reports whether it changed
// what the view shows (so the caller repaints only when there is something new to see). Three things
// can move: the offline state, the bindings themselves (through [Model.observeBinding]), and the row
// count of a picker that is OPEN over the offering this beat replaces — which changes how tall that
// pane is drawn, and so how many rows the transcript below it keeps ([Model.transcriptRows]).
//
// A beat that crosses back online AND rebinds says so once, not twice: "connected: <model>" is the
// stronger statement, and it already implies the server answered. The recovery note is for the
// ordinary case, where the server came back serving exactly what it served before and there is
// nothing else to report.
//
// FIRST CONTACT says nothing at all. When the session's very first beat lands clean — nothing has
// ever landed, nothing has ever failed — the binding it seeds is not news: the start-up box
// restated in place a few rows above already names the host, the model and the window, so a
// "connected:" line under it would only repeat what the human is looking at. That fact has to be
// read BEFORE the resets below erase it, which is why it is the fold's first statement; it then
// rides the intent all the way to [rebindNote], which owns the wording.
//
// A session that has SWITCHED servers has no such first contact left to be quiet about, even though
// the fresh heartbeat state looks exactly like a launch's. The box is no longer "a few rows above"
// but far up the scrollback, and the human explicitly asked for the move — so every post-switch
// seed announces itself (see heartbeatState.switched).
func (m Model) foldBeat(beat heartbeat.Beat) (Model, bool) {
	firstContact := !m.hb.everOnline && m.hb.failures == 0 && !m.hb.switched
	if !beat.Reachable {
		return m.foldBeatFailure(beat.Failure)
	}
	m.hb.failures = 0
	m.hb.everOnline = true
	m.hb.lastFailure = ""
	// The offering an open picker is drawn from, counted BEFORE and after the beat replaces it: a
	// row list that grew or shrank under the pane changed how tall the pane is drawn, which is a
	// change to what the view shows exactly as an offline crossing is (see this function's doc).
	// Counted only while the pane is up — with it closed the rows are nobody's height.
	shownBefore := 0
	if m.picker.open {
		shownBefore = m.pickerCount()
	}
	m.hb.models = beat.AvailableModels // the /model picker's rows are derived from it (picker.go)
	// A shorter offering must not leave an open picker highlighting a row that no longer exists.
	m.picker.clampSelection(m.pickerCount())
	offeringMoved := m.picker.open && m.pickerCount() != shownBefore
	crossed := m.hb.offline
	m.hb.offline = false

	m, rebound := m.observeBinding(beat, firstContact)
	if crossed && !rebound {
		m.transcript.addNote(onlineNote)
	}
	return m, crossed || rebound || offeringMoved
}

// observeBinding measures a landed beat against the last observation that mattered and, when the
// upstream has moved, binds the new reality — now if the engine is quiescent, or stashed for the
// exchange-terminal boundary when a worker owns it. It reports whether the view changed.
//
// The comparison is against what was last OBSERVED, not against what is bound, and the observation
// is recorded the moment the intent is captured (before the seam even answers). That one choice is
// what keeps a `context-window:` pin quiet: the server keeps reporting its own window every ten
// seconds, the pin keeps outranking it, and the difference between the two is never mistaken for a
// fresh change — so the TUI needs no knowledge of the pin at all. A beat that reports no window
// (0) is not evidence the window changed, only that this beat could not name it.
//
// firstContact is the pre-fold evidence its caller captured (see [Model.foldBeat]); it is stamped
// into the intent rather than re-derived here, so the deferred path words itself exactly like the
// immediate one — by construction, whenever the change is finally applied.
func (m Model) observeBinding(beat heartbeat.Beat, firstContact bool) (Model, bool) {
	if !m.appliesRebinds() {
		return m, false // a display-frozen heartbeat: nothing to apply a change through
	}
	changed := beat.ActiveModel != m.hb.observedModel ||
		(beat.ContextWindow > 0 && beat.ContextWindow != m.hb.observedWindow)
	if !changed {
		return m, false
	}
	m.hb.observedModel, m.hb.observedWindow = beat.ActiveModel, beat.ContextWindow
	m.hb.observedDialect = beat.EffortSupport.Dialect
	intent := rebindIntent{
		model:  beat.ActiveModel,
		window: beat.ContextWindow,
		// The dialect the beat reported for this server, carried whole with the rest of the
		// observation. It is deliberately NOT part of the changed test above: the dial is a fact
		// about the server, so it moves when the model or the window does and never on its own.
		dialect:   beat.EffortSupport.Dialect,
		quietSeed: firstContact,
	}
	if m.busy() || m.actuation.inFlight {
		// The engine is not the Update loop's to re-point right now, and Agent.Rebind is idle-only by
		// construction. Stash the intent for the boundary rather than refuse it — finishWorker for a
		// worker's Exchange, foldActuationDone for a launcher verb — so the switch the human made
		// upstream lands the moment the engine is quiescent again (latest-wins, so a second change
		// inside the same window simply supersedes this one).
		//
		// The actuation half is the same claim [Model.foldBeatFailure] makes about a FAILED beat, made
		// about one that lands: a profile load's own completion may re-point the whole session
		// (foldActuationDone → ProfileLoadResult.Move), and a rebind driven into the engine beside that
		// move is exactly the unsynchronized pair the latch exists to prevent.
		m.hb.pendingRebind = &intent
		return m, false
	}
	return m.applyRebind(intent)
}

// applyRebind re-resolves the bindings for one captured change through the [ServerHost.Rebind] seam
// and folds the answer into the display: the model and window the binary actually BOUND, the start-up
// box restated in place, the note that says what moved, and any notices the resolution produced. It
// reports whether it wrote to the view.
//
// It is called only where the engine is quiescent — a beat landing at idle, or the exchange-terminal
// fold for a change captured mid-Exchange — because the seam drives Agent.Rebind, which refuses
// anything else (ADR 0024). The conversation is deliberately left alone: history survives a switch,
// and ctxUsed keeps its last measured fill (an over-window conversation renders clamped at 100%
// until the next usage event or a compaction re-measures it).
//
// A failure leaves every binding exactly where it was and says so ONCE per distinct target: the
// monitor beats every ten seconds, and a transcript repeating the same refusal at that rate would
// bury the conversation it is meant to annotate (the saveFailing fail-once posture).
func (m Model) applyRebind(intent rebindIntent) (Model, bool) {
	if !m.appliesRebinds() {
		return m, false
	}
	rebind := m.opts.Server.Rebind
	oldModel, oldWindow := m.opts.Model, m.opts.ContextWindow

	result, err := rebind(intent.model, intent.window, intent.dialect)
	if err != nil {
		if m.hb.lastRebindFailed == intent.model {
			return m, false
		}
		m.hb.lastRebindFailed = intent.model
		m.transcript.addNote(rebindFailNote(oldModel, intent.model, err))
		return m, true
	}

	m.hb.lastRebindFailed = ""
	m.opts.Model, m.opts.ContextWindow = result.Model, result.ContextWindow
	// The box's facts were frozen when it was seeded, so a late seed would otherwise leave a
	// "connecting" box at the top of the scrollback until the next /clear.
	m.transcript.refreshStartup(newStartupView(m.opts))
	if note := rebindNote(oldModel, oldWindow, m.opts.Model, m.opts.ContextWindow, intent.quietSeed); note != "" {
		m.transcript.addNote(note)
	}
	for _, notice := range result.Notices {
		m.transcript.addNote(notice)
	}
	if m.opts.ContextWindow == 0 {
		m.transcript.addNote(unknownWindowNote)
	}
	return m, true
}

// applyPendingRebind binds a change that was captured while the engine was not the Update loop's to
// re-point — a worker owned it, or a launcher verb owned the server it talks to. Both terminal folds
// ARE the boundary Agent.Rebind demands — the same one AbortExchange and the idle save already use —
// and the Msg travelling through the Bubble Tea channel is what establishes the happens-before in
// both directions, which is why the engine's per-model bindings need no lock (ADR 0024). A no-op
// when nothing was deferred.
func (m *Model) applyPendingRebind() {
	intent := m.hb.pendingRebind
	if intent == nil {
		return
	}
	m.hb.pendingRebind = nil
	next, noted := m.applyRebind(*intent)
	*m = next
	if noted {
		m.refreshViewport()
	}
}

// unknownWindowNote is the honesty line for a binding whose context window nobody could name: the
// Budget and automatic Compaction both bind against the window, so with none known they silently do
// nothing. It rides the rebind rather than the start-up sequence because the window is known — or
// not — only once a beat has landed: printed at launch it would fire on every cold start and be
// wrong a second later.
const unknownWindowNote = "context window unknown — automatic compaction and the Budget are inactive; " +
	"set context-window: in config.yaml"

// rebindNote words what a successful rebind actually moved, in the three shapes it can take: the
// late seed that binds the session's first model (the async cold start — same code path, different
// words), a model change, and a window-only change. It returns "" whenever nothing the human is not
// already shown moved, which is two cases. The pinned window: the server switched to a
// differently-sized window, the pin outranked it, and the binding the human can see is unchanged —
// a note there would describe a change that did not happen. And the quiet first-contact seed
// (quietSeed): the session's very first beat landed clean, so the start-up box restated in place is
// already saying host, model and window a few rows up, and "connected:" would only say it again.
// A seed that follows a failed beat, one where a model finally appeared on a server that was up but
// serving nothing, or one that lands on a server the human just SWITCHED to (heartbeatState.switched
// — the box is far up the scrollback by then and the move was asked for), is genuine news and keeps
// its line. The window clause is carried only when the BOUND window moved, so a pin never narrates
// the change it suppressed.
//
// Both ids are rendered the way the footer and the start-up box render them (displayModel), so the
// note and the chrome beside it can never name the same model two different ways. Neither is
// escape-stripped here: the new id is whatever the server advertised, so it is untrusted, but the
// note's one destination is addNote, which strips at the seam for every producer.
func rebindNote(oldModel string, oldWindow int, newModel string, newWindow int, quietSeed bool) string {
	switch {
	case oldModel == "":
		if quietSeed {
			return ""
		}
		note := "connected: " + displayModel(newModel)
		if newWindow > 0 {
			note += ", context " + format.Tokens(newWindow)
		}
		return note
	case oldModel != newModel:
		note := "model changed: " + displayModel(oldModel) + " → " + displayModel(newModel)
		if newWindow != oldWindow {
			note += ", context " + windowWord(oldWindow) + " → " + windowWord(newWindow)
		}
		return note
	case newWindow != oldWindow:
		return "context window changed: " + windowWord(oldWindow) + " → " + windowWord(newWindow)
	}
	return ""
}

// rebindFailNote words a refused rebind. The bindings did not move, so the note's job is to say
// what the server is serving, that apogee is NOT following it, and why — a session silently talking
// to a model the server no longer loads is the failure this whole feature exists to prevent. The
// late seed says it differently because there is no old binding to still be on.
func rebindFailNote(oldModel, target string, err error) string {
	if oldModel == "" {
		return "could not bind " + displayModel(target) + ": " + err.Error() + " — no model is bound yet"
	}
	return "model change detected (" + displayModel(oldModel) + " → " + displayModel(target) +
		") but rebind failed: " + err.Error() + " — still bound to " + displayModel(oldModel)
}

// windowWord renders a context window inside a change clause, naming an unknown one in words rather
// than as the empty string [format.Tokens] yields — "context  → 16k" reads as a rendering bug, which
// is the very thing the gauge's clamp was fixed for.
func windowWord(n int) string {
	if n <= 0 {
		return "unknown"
	}
	return format.Tokens(n)
}

// foldServerSwitch folds a COMMITTED server switch (`/server`, picker.go) into the display. It is
// reached only after [ServerHost.Switch] has returned successfully, so the engine is already
// pointed at the new endpoint and nothing here can fail: every statement below describes a world
// that is already true. from is the label the footer used for the server being left, captured by
// the caller before the Options move.
//
// The heartbeat state is replaced WHOLESALE rather than patched, and that is what makes the switch
// need no unwinding. The fresh generation retires the old chain, so every beat and tick still in
// flight against the old server lands inert ([Model.heartbeatLive]). The offering empties with the
// server that advertised it. The offline debounce returns to its cold-start posture, which is the
// honest one here — nothing has been observed of this server yet, so its first failed beat is
// believed at once rather than debounced against evidence gathered about a different machine. The
// one fact carried across is switched, which keeps the seed that follows from being read as a
// launch (see [Model.foldBeat]).
//
// The model is UNBOUND, deliberately: the switch guesses nothing about what the new server serves
// (ADR 0024), so the footer says "connecting…", [Model.blockedUpstream] refuses a send, and the
// first beat binds through the ordinary rebind path — one code path with the cold start. ctxUsed
// survives, exactly as it survives a model rebind. The returned Cmd is that first beat, fired NOW
// rather than one Interval from now: the human just acted and should not watch "connecting…" for
// ten seconds.
func (m Model) foldServerSwitch(from string, result ServerSwitchResult, record choiceRecord) (tea.Model, tea.Cmd) {
	m.opts.Endpoint = result.Endpoint
	m.opts.HostAlias = result.HostAlias
	m.opts.ContextWindow = result.ContextWindow
	m.opts.Model = ""
	m.hb = heartbeatState{gen: m.hb.gen, switched: true}
	// The box's facts were frozen when it was seeded; restate it so the top of the scrollback names
	// the server this session is now on rather than the one it launched against (applyRebind's own
	// reason, one level up).
	m.transcript.refreshStartup(newStartupView(m.opts))
	m.transcript.addNote(serverSwitchNote(from, m.opts, record.saved))
	record.warn(&m.transcript)
	m.layout()
	// The fresh generation IS the retirement of the old chain, so the first beat is armed rather than
	// merely issued — in a statement of its own, per [Model.armBeat].
	beat := m.armBeat()
	return m, beat
}

// serverSwitchNote words a committed switch: the server left, by the label the footer called it, and
// the one now on the wire — its alias with the endpoint spelled out beside it, because the alias is
// the human's own word for a URL and the switch is exactly the moment to show which URL it stands
// for. An aliasless server would name the endpoint twice, so it says it once. A switch that was also
// recorded says so at the end of the same line (savedClause, prebound.go).
func serverSwitchNote(from string, to Options, saved bool) string {
	note := "switching server: " + from + " → " + hostDisplay(to)
	if hostDisplay(to) != to.Endpoint {
		note += " (" + to.Endpoint + ")"
	}
	return note + savedClause(saved)
}

// foldBeatFailure folds a beat that could not read the server. Four rules, in order:
//
//   - While an Exchange is in flight the failure is IGNORED — counter, state and words untouched.
//     A streaming reply is stronger evidence that the server is there than a timed-out /v1/models
//     on a single-slot server busy serving that very stream.
//   - While an ACTUATION is in flight it is ignored for the mirror-image reason (ADR 0029 D5): the
//     server is EXPECTED to be down mid-restart, so a beat that cannot read it is evidence of
//     nothing. A beat that LANDS in that shadow is folded normally — a server answering mid-load is
//     harmless news — though the BINDING it observes is stashed for the completion fold rather than
//     driven into an engine the completion may be about to re-point (observeBinding).
//   - Before any beat has ever landed, one failure is enough: a cold start against a server that
//     is not running should say so at once rather than after a debounce it has no evidence for.
//   - Otherwise the crossing waits for offlineFailureThreshold consecutive idle failures.
//
// The crossing is noted exactly once; every further failed beat is silent until a success crosses
// back (foldBeat).
func (m Model) foldBeatFailure(failure string) (Model, bool) {
	if m.busy() || m.actuation.inFlight {
		return m, false
	}
	m.hb.failures++
	m.hb.lastFailure = failure
	if m.hb.offline || (m.hb.everOnline && m.hb.failures < offlineFailureThreshold) {
		return m, false
	}
	m.hb.offline = true
	m.transcript.addNote(offlineNote(failure))
	return m, true
}

// blockedUpstream reports whether there is nothing to send to right now: the heartbeat says the
// server is offline, or no model is bound yet because the first beat has not landed (the async
// cold start). It gates the three paths a HUMAN opens an Exchange with — a message, /continue, and
// /compact — so a send fails loudly at the boundary instead of silently against a dead endpoint.
// Everything else stays live: scrollback, /clear, /sessions, /version, /confine, Shift+Tab.
//
// The interjection auto-flush (flushAfterCompletion, ADR 0025) is a FOURTH path that opens an
// Exchange and deliberately does NOT consult this: it runs inside a natural completion's own fold,
// and foldBeat ignores a failed beat while an Exchange is in flight, so the offline state cannot
// have moved since the Exchange that just completed was itself allowed to start. That is an
// invariant, not a coincidence — if the offline crossing ever becomes able to land mid-Exchange,
// the flush needs this guard too.
//
// With the monitor unwired it is always false: nothing observes the server, so the TUI has no
// standing to refuse anything (and every pre-heartbeat test keeps its behaviour).
func (m Model) blockedUpstream() bool {
	return m.observesUpstream() && (m.hb.offline || m.opts.Model == "")
}

// upstreamBlockNote words the refusal blockedUpstream produced: offline names the endpoint and,
// when the monitor has them, the failure's own words; the pre-bind case says the truth instead —
// the server has not answered YET, which on a cold start is a matter of seconds.
func (m Model) upstreamBlockNote() string {
	if m.prebound() {
		// The blocked-upstream ladder reads a session with no server as one whose first beat has not
		// landed, and would name an endpoint that does not exist. There is no server AT ALL here, so
		// the pre-bound state's own words are the honest answer (prebound.go). This is the note the
		// two Exchange-opening verbs earn; a typed message is answered one step earlier, by the ask
		// itself (submit → preboundRefusal).
		return preboundNotice(m.opts.Prebound)
	}
	if !m.hb.offline {
		return "cannot send — still connecting to " + m.opts.Endpoint
	}
	note := "cannot send — server offline (" + m.opts.Endpoint + ")"
	if m.hb.lastFailure != "" {
		note += ": " + m.hb.lastFailure
	}
	return note
}

// foldRoutingNotice folds one change of the Sub-agent server's routing state into the transcript
// as a single ephemeral note.
//
// It is the composition root's second heartbeat reporting that delegations changed destination
// (ADR 0045 §4). Like a schedule Event it is a record and not a gate — one note, no state
// transition, no engine call — because routing is a fact about the OTHER server and this session's
// conversation carries on regardless.
//
// The note is EPHEMERAL, like the "context: …" line: the routing state is re-derived from live
// beats every time a session starts or resumes, so a stored "routing to grunt" is a claim about a
// server nobody has beaten since — and five resumes would keep five of them (addEphemeralNote).
func (m Model) foldRoutingNotice(msg routingNoticeMsg) (tea.Model, tea.Cmd) {
	m.transcript.addEphemeralNote(msg.note)
	m.refreshViewport()
	return m, nil
}
