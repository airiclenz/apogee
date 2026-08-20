package tui

import (
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// ----------------------------------------------------------------------------
// The actuation latch, the progress narration, and the completion folds (ADR 0029 D5/D6)
// ----------------------------------------------------------------------------
//
// An actuation is one call into the launcher that CHANGES the world — `/model` activating a Launch
// profile, `/unload-model` freeing the session's model, `/stop-server` stopping its server. Three
// facts shape everything below.
//
// It BLOCKS. The facade waits up to ~30 s for health, escalates a stop for another ~20 s, and
// gives a large model load minutes; so the verb runs on a Cmd goroutine and the Update loop keeps
// painting. It must be SERIALIZED per address by the caller — that is the facade's stated
// contract, and the latch here is apogee's whole answer to it: at most one verb is ever in flight,
// so two lifecycle calls can never overlap. And it is NOT a binding: an actuation asks the
// launcher to change the world, and the next Beat is what notices what the world became (ADR 0029
// D1) — the completion folds below therefore state what happened and then hand the display back to
// the heartbeat, exactly as `/server`'s switch does.
//
// The narration is a single channel and a re-arming listen Cmd — the engine-event pump's shape,
// one item at a time into the Update loop. One channel rather than two is what keeps the ORDER
// honest: the progress steps and the completion travel the same queue, so a step can never be
// painted after the completion it preceded.

// actuation is the latch: which launcher verb is in flight, on what, and the pump it is narrated
// through. Its zero value is "nothing in flight", so it lives inline in the value-copied Model like
// picker and sessionBrowser (ADR 0011) — plain values plus a channel, which is a reference header
// and so copies safely.
type actuation struct {
	// inFlight is the latch itself: while it is set, sends and every switching or actuating verb
	// are refused with one note (actuationBlockNote).
	inFlight bool
	// verb is which of the three is running — verbLoad, verbUnload, verbStop — read by the footer
	// and by the completion fold.
	verb string
	// profile is the Launch profile a load is activating; empty for the two verbs that act on the
	// session's server rather than on a named profile.
	profile string
	// gen names this actuation, the heartbeat's own generation guard applied to the pump: an item
	// carrying any other generation is inert, so nothing a released latch left in flight can be
	// folded into the next verb. It is never reset, only incremented.
	gen int
	// events is the pump's channel — the blocking verb's goroutine writes, the listen Cmd reads.
	// nil when nothing is in flight.
	events chan actuationEvent
}

// The three verbs, as the footer spells them and the completion fold switches on them.
const (
	verbLoad   = "load"
	verbUnload = "unload-model"
	verbStop   = "stop-server"
)

// actuationBuffer is how many narration steps the pump can hold before the launcher's callback
// waits for the Update loop. It is generous rather than unbounded because the producer blocking
// briefly is harmless — the drain re-arms on every item — while a dropped step would be a lie about
// what the launcher did.
const actuationBuffer = 64

// actuationEvent is one item off the in-flight verb's channel: a narration step, or the single
// final item that carries the outcome. Exactly one final item is ever sent, and it is sent from a
// defer — so a seam that panics still releases the latch instead of stranding the session.
type actuationEvent struct {
	// step is one line of the launcher's own narration (a progress callback); meaningful only
	// while final is false.
	step string
	// final marks the completion item. Everything below it is meaningful only then.
	final bool
	// load is a completed profile load's answer, and result a completed
	// `/unload-model`/`/stop-server`'s. Which one carries the outcome is decided by the latch's
	// verb, not by inspecting them.
	load   ProfileLoadResult
	result ActuationResult
	// err is what the seam returned. Steps and notices recorded before it still travel, because
	// how far a verb got before it failed is exactly what the human needs.
	err error
}

// actuationMsg carries one actuationEvent into the Update loop, stamped with the latch generation
// it belongs to (the beatMsg posture).
type actuationMsg struct {
	gen int
	ev  actuationEvent
}

// ErrStartupTimeout is the launcher's startup-timeout condition, PROJECTED — the server was
// started but did not report healthy inside the facade's wait. internal/tui never imports the
// launcher (ADR 0029 D1: no facade type or sentinel reaches the renderer), so the composition root
// marks that one error with this sentinel and the completion fold matches it with errors.Is. It is
// the only failure with a coda, because it is the only one that may still come good on its own: the
// launcher deliberately leaves the server running, and the heartbeat binds it if it comes up.
var ErrStartupTimeout = errors.New("the launcher's health wait timed out")

// startupTimeoutCoda is what that sentinel buys the human: the honest statement that the failure
// is not a dead end, made true for free by ADR 0029 D1 — the beat completes every move, including
// one that arrives late.
const startupTimeoutCoda = " — the heartbeat will bind it if it comes up"

// noLauncherNote is the one line every launcher verb owes on a host where the integration is not
// configured — whether the seams were never wired (they are wired together or not at all) or are
// wired and switched off (launcherOff). One sentence answers for all of them, and it names the key
// that turns them on.
const noLauncherNote = "llama-launcher not configured — set llama-launcher: in config.yaml or install the launcher"

// ErrNoLauncher is that same statement as a condition a seam can REPORT, projected like
// [ErrStartupTimeout] so no launcher vocabulary reaches this package. Since `llama-launcher:` became
// live-editable (ADR 0037) the seams are wired for the whole session and the integration is off or
// on inside them, which is a fact only a call can learn: a verb answers with this error while the
// key is unset, and `/model` reads it as "there is no launcher here" and offers the models the
// server itself advertises — exactly what an unwired seam used to mean.
//
// Its text IS noLauncherNote, so a verb that reports it and a verb that was never wired say the one
// sentence to the human.
var ErrNoLauncher = errors.New(noLauncherNote)

// launcherOff reports the integration being switched off on a host that IS wired — the state
// ADR 0037 created by making `llama-launcher:` editable mid-session. It is asked BEFORE the
// actuation latch is taken, because a refusal that travels back through the pump is a refusal the
// footer narrates first: the human watches "unloading…" appear and vanish for a verb that never
// ran, where an unwired seam answered instantly. A host that cannot answer says true
// ([LauncherHost.Enabled]) — being unable to tell is not evidence of anything — so the verb goes
// ahead and reports for itself, and a nil host is not asked at all: its own absence is the same
// sentence, said by the caller.
func (m Model) launcherOff() bool {
	return m.opts.Launcher != nil && !m.opts.Launcher.Enabled()
}

// launcherActs is what the launcher host says the renderer has to know before it acts
// ([LauncherHost.Acts]), with the unwired host answering for a nil one: the zero [LauncherActs] is
// "none of it", so the one gate that reads it reads a flag instead of a nil check and a call.
func (m Model) launcherActs() LauncherActs {
	if m.opts.Launcher == nil {
		return LauncherActs{}
	}
	return m.opts.Launcher.Acts()
}

// stopHeading is the line `/stop-server` puts ABOVE the launcher's recorded steps. The steps are
// terse and subject-less ("Sending stop signal", "Waiting for shutdown"), so without a heading the
// transcript never says what they were done TO — and "what was it stopping" is the whole question a
// human reads this block to answer. It names the launcher's own backend and address rather than the
// session's endpoint, because those are what the verb actually acted on.
//
// A result that names neither earns no heading at all: an empty "stopping" would say less than the
// steps it introduces.
func stopHeading(res ActuationResult) string {
	subject := res.Backend
	switch {
	case subject == "":
		subject = res.Addr
	case res.Addr != "":
		subject += " (" + res.Addr + ")"
	}
	if subject == "" {
		return ""
	}
	return "stopping " + subject
}

// unloadOutcome words the one thing `/unload-model` has to be honest about: whether freeing the
// model also took the server with it. On a MANAGED backend the model is baked into the process
// arguments, so an unload IS a stop and the session's server is now gone — a fact the human needs
// before wondering why the beat went quiet — while an external backend keeps serving and can be
// loaded again without a launch. The backend is named when the launcher said which one, because
// WHICH server just went down is the next thing asked.
func unloadOutcome(res ActuationResult) string {
	if !res.ServerStopped {
		return "model unloaded — server still up"
	}
	if res.Backend == "" {
		return "model unloaded — this stopped the server"
	}
	return "model unloaded — this stopped " + res.Backend
}

// actuationBlocked reports whether command is one of the verbs the latch refuses while it is held:
// the ones that open an Exchange (a typed message is the third path and is gated in submit) and the
// ones that switch or actuate the session's server. Both sets are read off the "/" table's own flags
// (commandSpec.opensExchange, commandSpec.touchesServer) rather than named here, so a verb cannot be
// added to the namespace and forgotten by the latch — which used to fail silently, the verb running
// into a server mid-restart. Everything else — scrollback, /clear, /sessions, /version, /confine —
// stays live, because none of them touches the server being worked on.
func actuationBlocked(command string) bool {
	spec, ok := commandByName(command)
	return ok && (spec.opensExchange || spec.touchesServer)
}

// actuationBlockNote words that refusal. A load names the profile it is waiting on — that is the
// fact the human wants, and it is the one the footer is showing at the same moment — while the two
// verbs that act on the session's own server have nothing to name but themselves.
func (m Model) actuationBlockNote() string {
	if m.actuation.verb == verbLoad {
		return "profile load in flight — " + stripEscapes(m.actuation.profile)
	}
	return m.actuation.verb + " in flight"
}

// actuationLabel is what the footer's model slot says while the latch is held, and "" when it is
// not. It takes precedence over "connecting…" because it is the more specific truth: the session is
// not merely waiting for a server, it is waiting for THIS.
func (m Model) actuationLabel() string {
	if !m.actuation.inFlight {
		return ""
	}
	if m.actuation.verb == verbLoad && m.actuation.profile != "" {
		return "loading " + stripEscapes(m.actuation.profile) + "…"
	}
	return m.actuation.verb + "…"
}

// holdActuation takes the latch for verb and opens a fresh pump for it. The generation advances
// with every take, so anything still in flight from the last one lands inert.
func (m Model) holdActuation(verb, profile string) Model {
	m.actuation = actuation{
		inFlight: true,
		verb:     verb,
		profile:  profile,
		gen:      m.actuation.gen + 1,
		events:   make(chan actuationEvent, actuationBuffer),
	}
	return m
}

// startProfileLoad is the accept path of `/model`'s launcher offering, shared by the picker's ⏎ and
// "/model <profile>" (picker.go: pickLaunchProfile). It closes the overlay, takes the latch, and
// puts the blocking verb on a Cmd goroutine with its narration pumped back one step at a time.
// Nothing is bound here and nothing is claimed: the completion fold decides whether the session has
// to follow the profile, and the beat after it is what binds.
func (m Model) startProfileLoad(name string) (tea.Model, tea.Cmd) {
	m.picker = picker{}
	host := m.opts.Launcher
	if host == nil {
		return m.pickerNote(noLauncherNote) // unreachable through the command; the seam is checked first
	}
	m = m.holdActuation(verbLoad, name)
	m.layout() // the footer's model slot now says "loading <name>…"
	return m, m.actuationCmds(func(progress func(string)) actuationEvent {
		result, err := host.Load(name, progress)
		return actuationEvent{load: result, err: err}
	})
}

// endpointActuation is the host's own verb for one of the two acts that address the server this
// session is ON rather than a named profile — `/unload-model`'s and `/stop-server`'s halves of
// ADR 0029 D3. It answers nil for a host that is not there at all, which is the absent-seam shape
// the caller's refusal already reads, and for a verb that is neither of the two, which no caller
// passes.
func (m Model) endpointActuation(verb string) func(endpoint string) (ActuationResult, error) {
	if m.opts.Launcher == nil {
		return nil
	}
	switch verb {
	case verbUnload:
		return m.opts.Launcher.Unload
	case verbStop:
		return m.opts.Launcher.Stop
	}
	return nil
}

// startServerActuation is the same entry point for the two verbs that act on the server this
// session is talking to rather than on a named profile (`/unload-model`, `/stop-server`). It takes
// the verb alone and resolves the act from it (endpointActuation), because the two acts are members
// of one host now rather than two fields a caller can hand over. The endpoint is read HERE, on the
// Update loop, so the verb acts on the server the session is on at the moment the human asked — not
// on wherever it may have moved to by the time the call returns.
func (m Model) startServerActuation(verb string) (tea.Model, tea.Cmd) {
	// Both shapes of "there is no launcher here" are answered on this keypress and without the latch:
	// a host that was never wired, and one that is wired but switched off (launcherOff). They say the
	// same sentence because they are the same fact to the human.
	act := m.endpointActuation(verb)
	if act == nil || m.launcherOff() {
		return m.pickerNote(noLauncherNote)
	}
	endpoint := m.opts.Endpoint
	m = m.holdActuation(verb, "")
	m.layout()
	return m, m.actuationCmds(func(func(string)) actuationEvent {
		result, err := act(endpoint)
		return actuationEvent{result: result, err: err}
	})
}

// actuationCmds is the pump's two halves, batched: the producer that runs the blocking verb, and
// the first listen Cmd. Both close over the channel and the generation by value, so no pointer into
// the value-copied Model crosses a goroutine (the beatCmd posture).
func (m Model) actuationCmds(run func(progress func(string)) actuationEvent) tea.Cmd {
	gen, events := m.actuation.gen, m.actuation.events
	return tea.Batch(actuationRun(events, run), actuationListen(gen, events))
}

// actuationRun drives one blocking launcher verb on a Cmd goroutine, forwarding every progress
// callback onto the channel as it happens and closing with exactly one final item.
//
// The final send is DEFERRED, which is what makes the latch impossible to strand: a seam that
// returns normally, one that returns an error, and one that panics all reach the same send, so the
// completion fold always runs and always releases. The Cmd's own Msg is nil — everything this
// goroutine has to say travels on the channel, in order.
func actuationRun(events chan<- actuationEvent, run func(progress func(string)) actuationEvent) tea.Cmd {
	return func() tea.Msg {
		final := actuationEvent{final: true}
		defer func() {
			if r := recover(); r != nil {
				final = actuationEvent{final: true, err: fmt.Errorf("the launcher panicked: %v", r)}
			}
			events <- final
		}()
		out := run(func(step string) { events <- actuationEvent{step: step} })
		out.final = true
		final = out
		return nil
	}
}

// actuationListen takes ONE item off the pump and hands it to the Update loop, which re-arms it for
// the next one until the final item arrives. Reading one at a time is what lets each step be
// painted as it happens rather than in a batch at the end.
func actuationListen(gen int, events <-chan actuationEvent) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return nil // no producer can close this channel; the guard costs nothing
		}
		return actuationMsg{gen: gen, ev: ev}
	}
}

// actuationLive reports whether gen belongs to the actuation currently holding the latch — the
// heartbeatLive guard, applied to the pump, so a released latch can never be re-entered by an item
// that was already on its way.
func (m Model) actuationLive(gen int) bool {
	return m.actuation.inFlight && gen == m.actuation.gen
}

// foldActuation folds one pump item: a narration step becomes a transcript note and re-arms the
// listener, the final item completes the verb.
func (m Model) foldActuation(msg actuationMsg) (tea.Model, tea.Cmd) {
	if !m.actuationLive(msg.gen) {
		return m, nil
	}
	if msg.ev.final {
		return m.foldActuationDone(msg.ev)
	}
	// The launcher's own words about a machine's processes and files, so escape-stripped on the
	// way to the terminal exactly as a model id or a context file's name is.
	m.transcript.addNote(stripEscapes(msg.ev.step))
	m.refreshViewport()
	return m, actuationListen(msg.gen, m.actuation.events)
}

// foldActuationDone completes a verb: the latch is released FIRST — before any of the statements
// below, so every path out of here leaves the session actuable again — and then what happened is
// told, in the order it happened.
//
// A profile load that has to MOVE the session commits the move HERE and then takes the `/server`
// fold whole ([Model.foldServerSwitch]): the display adopts the new server, the old heartbeat chain
// retires, and the fresh chain's first beat — fired now rather than one Interval from now — binds
// what the profile actually loaded. Committing it here rather than inside the seam is the whole
// point of [ProfileLoadResult.Move]: re-pointing the engine is an Update-goroutine act, and the
// blocking load that resolved it ran on a Cmd goroutine where a heartbeat-driven rebind would race
// it. A load that moved nothing says so and fires the same immediate beat, which is what completes
// it: the ordinary rebind path then words the change ("model changed: A → B"), because that IS what
// happened to a session whose server swapped its model underneath it.
//
// A failure is the launcher's own words. The one that earns a coda is the health-wait timeout: the
// server was left running, so the heartbeat binding it later is a real outcome rather than
// consolation.
func (m Model) foldActuationDone(ev actuationEvent) (tea.Model, tea.Cmd) {
	verb, profile := m.actuation.verb, m.actuation.profile
	m.actuation = actuation{gen: m.actuation.gen} // released on EVERY path, generation kept

	// The launcher's notices (config warnings, the drift notice a non-restarting activation emits)
	// and a stop's recorded steps are the same kind of thing: what the library did, in its words.
	// They travel even beside an error.
	for _, notice := range ev.load.Notices {
		m.transcript.addNote(stripEscapes(notice))
	}
	if verb == verbStop {
		// The heading goes FIRST, so the steps beneath it have a subject. It is written here rather
		// than when the verb was asked for because only the launcher can say which server answers at
		// the session's address — and it says so in the result.
		if heading := stopHeading(ev.result); heading != "" {
			m.transcript.addNote(stripEscapes(heading))
		}
	}
	for _, step := range ev.result.Steps {
		m.transcript.addNote(stripEscapes(step))
	}

	if verb == verbLoad && ev.err == nil && ev.load.Move != nil {
		// The move the load RESOLVED, committed on the Update goroutine — the boundary Agent.Rebind
		// and Agent.SwitchUpstream both state as their synchronization, and the reason the seam hands
		// back a call rather than performing one (ADR 0029 D2, [ProfileLoadResult.Move]).
		from := hostDisplay(m.opts) // the label the footer used for the old server, captured before it moves
		switched, moveErr := ev.load.Move()
		if moveErr == nil {
			// One of the two commits that remember the profile, and it is stated BEFORE the move's own
			// line rather than after it: the pointer lands on the entry this session is LEAVING (the
			// actuating one), so the recording belongs with the load's narration above rather than with
			// the switch below, whose subject is the server the session is arriving on.
			m.recordLoadedProfile(profile)
			// The /server fold repaints on its way out (it restates the start-up box and lays out), so
			// the notices above are on screen with the move rather than one frame behind it. It also
			// replaces the whole heartbeat state, which discards any rebind stashed under the latch —
			// an observation of the server being LEFT is not news about the one being joined.
			// Nothing is RECORDED here: a profile's server is an address the launcher chose, not an
			// entry of `servers:`, so there is no name a next session could start on (ADR 0036
			// decision 2). The zero record is that silence — no clause on the note, no warning.
			return m.foldServerSwitch(from, switched, choiceRecord{})
		}
		// The profile is loaded but the session could not follow it there, so it stays exactly where
		// it was and the failure is told like every other one below.
		ev.err = fmt.Errorf("profile %s loaded, but the session could not follow it: %w", profile, moveErr)
	}

	// Every path from here leaves the session on the server it was already talking to, which makes
	// this fold the quiescent boundary a binding change observed under the latch has been waiting
	// for: observeBinding STASHES one rather than driving Agent.Rebind beside a move this completion
	// may be about to make. It is finishWorker's posture, one level across, and it runs BEFORE the
	// completion's own words because the beat that saw it landed before the completion did.
	m.applyPendingRebind()

	if ev.err != nil {
		note := stripEscapes(ev.err.Error())
		if errors.Is(ev.err, ErrStartupTimeout) {
			note += startupTimeoutCoda
		}
		m.transcript.addNote(note)
		m.refreshViewport()
		return m, nil
	}
	if verb != verbLoad {
		// `/stop-server` has already said everything it did — the heading and the steps beneath it — while
		// `/unload-model` owes one more sentence, because its two outcomes look identical in the steps and
		// only one of them leaves the session without a server. Then the display's next move belongs
		// to the heartbeat (a stopped server crosses offline the ordinary way, which is now honest —
		// the latch is released and the downtime is real).
		if verb == verbUnload {
			m.transcript.addNote(stripEscapes(unloadOutcome(ev.result)))
		}
		m.refreshViewport()
		return m, nil
	}
	m.transcript.addNote("profile " + stripEscapes(profile) + " loaded — waiting for the beat")
	// The other commit: the load landed on the very server this session is on, so the profile it now
	// serves is the one the actuating entry should come back on (see the move's own record above).
	m.recordLoadedProfile(profile)
	m.refreshViewport()
	// Armed in a statement of its own: the immediate beat opens a FRESH chain, and the generation
	// bump has to land on the Model this returns (the spinnerAnim.arm convention, [Model.armBeat]).
	beat := m.armBeat()
	return m, beat
}

// ----------------------------------------------------------------------------
// Recording the loaded profile — the `launch-profile:` key (remember-model)
// ----------------------------------------------------------------------------

// launchProfileSavedNote is what a RECORDED load says: the launcher-fronted entry this session
// actuates through now names this profile, so apogee brings that server back on it. It names the key
// the way [savedChoiceClause] and [modelSavedNote] do, because the key is what the human will find in
// config.yaml — and it names no SERVER, because the entry the pointer landed on is the one the
// session was actuating through and not necessarily the one it ends up talking to (a load that moves
// the session leaves it somewhere the `servers:` list may not describe at all).
const launchProfileSavedNote = "launch-profile: saved — apogee loads it at the next start"

// recordLoadedProfile offers a committed load to the recording seam and states what came back: the
// line above when the pointer landed, the failed-write footnote when the splice could not, and
// nothing whatsoever for the skips the binary makes silently (the toggle off, an actuating entry it
// cannot identify).
//
// It takes a pointer receiver for [choiceRecord.warn]'s reason — a Model is copied by value on every
// Update (ADR 0011), so the notes have to land on the CALLER's own copy — and it lays nothing out:
// both call sites are mid-fold and repaint on their way out.
func (m *Model) recordLoadedProfile(profile string) {
	record := recordLaunchProfile(m.opts.Launcher, profile)
	if record.saved {
		m.transcript.addNote(launchProfileSavedNote)
	}
	record.warn(&m.transcript)
}

// recordLaunchProfile persists the profile a load just committed as the one the actuating entry comes
// back on, through the seam the binary backs. It is [recordServerChoice] one class across: the
// renderer offers every committed load and believes the answer, because which entry may carry this
// key — and whether the toggle allows writing it at all — are questions only the binary can settle.
//
// A failed write is a warning and nothing more: the launcher has already loaded the profile, and the
// recording is best-effort persistence of something that is already true. An unwired host records
// nothing and says nothing, which is the pre-remember-model behaviour every hand-built Options has.
func recordLaunchProfile(host LauncherHost, profile string) choiceRecord {
	if host == nil || profile == "" {
		return choiceRecord{}
	}
	saved, err := host.RecordProfile(profile)
	if err != nil {
		return choiceRecord{warning: "could not record the launch profile: " + stripEscapes(err.Error())}
	}
	return choiceRecord{saved: saved}
}

// ----------------------------------------------------------------------------
// The start-up restore — the `launch-profile:` boot half (remember-model)
// ----------------------------------------------------------------------------
//
// A session whose server is launcher-fronted and whose entry names a `launch-profile:` opens by making
// the world serve that profile again — but only while nothing is serving already, and only while the
// launcher still defines it. Both are facts about the LAUNCHER's world, so the whole decision belongs
// to the binary ([LauncherHost.Restore]) and exactly one of three answers lands here: load this, say
// this, do nothing.
//
// The actuation itself is the ordinary one. A restore reaches [Model.startProfileLoad] exactly as
// `/model`'s accept does, which is the whole reason this feature adds no binding machinery: the latch
// serializes it against every other launcher verb, a beat that lands in its shadow is STASHED rather
// than driven into the engine (observeBinding), and the completion fold commits a move and arms the
// beat that binds (ADR 0029 D5). Nothing here has to reason about the first heartbeat at all.

// restoreMsg carries the start-up restore check's answer into the Update loop. It arrives off a Cmd
// goroutine like a beat and carries no generation guard, because there is exactly one of it in a
// session: nothing issues a second, so there is no retired chain a stale one could belong to.
type restoreMsg struct {
	restore ProfileRestore
	err     error
}

// restoreCmd asks the binary what this start-up owes a recorded `launch-profile:`, off the Update
// loop — the seam reads a config file and probes for servers. It captures the host by value, so no
// pointer into the value-copied Model crosses the goroutine (the beatCmd posture), and returns nil
// for a host that does not answer this check at all ([LauncherActs.CanRestore], which a nil host
// answers false), which is what lets [Model.Init]'s tea.Batch collapse to exactly what it collapsed
// to before this existed.
func (m Model) restoreCmd() tea.Cmd {
	if !m.launcherActs().CanRestore {
		return nil
	}
	host := m.opts.Launcher
	return func() tea.Msg {
		restore, err := host.Restore()
		return restoreMsg{restore: restore, err: err}
	}
}

// foldRestore folds that answer: the binary's line when it has one, then the load when it asked for
// one. A failed check is stated exactly like a refused restore, because to the human it IS one — the
// recorded profile is not coming back this session, and the launcher's own words say why.
//
// The load is guarded by [Model.restorable] rather than performed unconditionally: the answer was
// decided against the session as it stood when the check went out, and a human is free to have moved
// it since. Both guards protect something real — taking the latch twice would strand the verb already
// holding it, and closing an open picker would pull an overlay out from under the keypress that opened
// it — so a session that has moved on simply keeps what it chose, silently.
func (m Model) foldRestore(msg restoreMsg) (tea.Model, tea.Cmd) {
	note := msg.restore.Note
	if msg.err != nil {
		note = msg.err.Error()
	}
	if note != "" {
		// The binary's own words about a machine's servers and config, escape-stripped on the way to
		// the terminal exactly as the launcher's narration steps are.
		m.transcript.addNote(stripEscapes(note))
		m.refreshViewport()
	}
	if msg.err != nil || msg.restore.Load == "" || !m.restorable() {
		return m, nil
	}
	return m.startProfileLoad(msg.restore.Load)
}

// restorable reports whether the session is still in the state the restore was decided FOR: no
// launcher verb in flight, no picker open over the transcript, and a launcher host to actuate
// through.
func (m Model) restorable() bool {
	return m.opts.Launcher != nil && !m.actuation.inFlight && !m.picker.open
}
