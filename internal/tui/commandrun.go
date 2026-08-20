package tui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Command running and refusal (ADR 0043)
// ----------------------------------------------------------------------------
//
// The two refusals a typed line can meet before anything runs, the gate they hang off, and the
// three drivers that DO run: the Exchange launch both send paths share, the session reset /clear
// means, and the /command switchboard itself. Lifted out of model.go as one concern: what a
// recognised verb does and what an unrunnable one is answered with are the same question. The
// parse that classifies the line stays in command.go; [Model.submit] stays in model.go with the
// input concern it belongs to.

// refuseUnknownSlash answers the sole-token typo guard (parseInput's kindUnknownSlash): a note
// naming the word that resolved to nothing, and the line left exactly where it was. It is the
// blockedUpstream refusal posture — the human typed something they meant as an invocation, so the
// honest answer is to say it did not land and hand the text back for a one-character fix, never to
// forward "/skills" to the model as if it were prose.
//
// Both ⏎ paths share it, because the guard is about what the WORD names, not about what the model
// is doing: at idle (submit) and while a worker runs (stageInterjection) alike, nothing is sent,
// nothing is staged, and no worker is disturbed — hence the nil Cmd.
func (m Model) refuseUnknownSlash(parsed parsedInput) (tea.Model, tea.Cmd) {
	m.transcript.addNote(unknownSlashNote(parsed.text))
	m.refreshViewport()
	return m, nil
}

// commandRunnable reports whether parsed's verb may be driven in the state the Model is in RIGHT
// NOW. It is the one gate the two invocation routes share — ⏎ on a whole-input line
// (stageInterjection) and a dropdown accept (acceptAutocomplete) — so the menu's "— idle only" tag,
// the refusal note and what actually happens are three views of a single rule.
//
// At a quiescent boundary every verb is runnable. While a worker owns the engine (m.busy() — the
// same predicate that decides whether Esc stops something) only the reporting lines are:
// parsedInput.safeWhileRunning owns which those are, and it is deliberately asked about the parsed
// LINE rather than the bare verb, because "/confine" and "/confine off" are the same verb and only
// one of them is a report.
func (m Model) commandRunnable(parsed parsedInput) bool {
	return !m.busy() || parsed.safeWhileRunning()
}

// refuseIdleOnlyCommand answers an idle-only command invoked while a worker works: the note that
// says commands run at idle, and NOTHING else moved. The draft stays exactly as it was — the verb
// token included, because it was never consumed — so the human can press ⏎ on the very same line the
// moment the Exchange ends; it is refuseUnknownSlash's posture, applied to a word that resolves fine
// and merely came too early.
//
// The overlay closes, because the accept key was answered: every other branch of acceptAutocomplete
// either closes it or deliberately re-derives it, and an open menu still highlighting the row that
// was just refused would only invite the same refusal again. Typing on re-opens it, tag and all.
func (m Model) refuseIdleOnlyCommand() (tea.Model, tea.Cmd) {
	m.transcript.addNote(commandsAtIdleNote)
	m.dismissAutocomplete()
	m.layout()
	return m, nil
}

// launchExchange starts the worker over one Exchange and moves the Model into stateRunning: a
// fresh mailbox for what the human types while it runs, the worker Cmd and the CancelFunc the stop
// key calls (C4), the queue legend on the emptied box, the opening "thinking" phrase, and the
// spinner tick — batched as the one Cmd the caller returns.
//
// It is the tail the two send paths share — a typed submit and an interjection flush — so a
// message the queue sends enters exactly the state a typed one does. Everything upstream of it
// stays the caller's: the parse, the upstream and InExchange guards, the transcript block, and
// what happens to the editor (a flush at a natural completion deliberately leaves a half-typed
// line alone).
//
// The mailbox is fresh per Exchange: this worker is the only one that will ever drain it, and it
// dies with the Exchange (finishWorker clears it), so a row can never be delivered into an
// Exchange other than the one it was typed during. The activity phrase is set here because the
// request is away and nothing has come back yet — "thinking" holds until the first Event
// re-derives it (activity.go).
func (m Model) launchExchange(in domain.UserInput) (tea.Model, tea.Cmd) {
	m.box = newInterjectBox()
	cmd, cancel := startExchange(m.parent, m.eng, in, m.box, m.notify, m.flushEvents)
	m.cancel = cancel
	m.state = stateRunning
	m.setPlaceholder(runningPlaceholder) // the empty box now invites a queued message, not a send
	m.setActivity(actThinking, "", 0, "")
	m.reasoning.reset() // a new Exchange never inherits the last one's reasoning (reasoning.go)
	tick := m.spin.arm()
	return m, tea.Batch(cmd, tick)
}

// startNewSession closes the current session into history and resets the TUI to a fresh one. /clear and
// its alias /new both route here — "start a new session" is exactly what they mean.
//
// It flushes the outgoing conversation through the SessionHost (its last state, post-turn notes and
// all) so it lands in the history browser, then rotates the host so the next Turn's save mints a fresh
// session id rather than clobbering the one just closed, then drops the engine's conversation memory
// (ClearContext), wipes the transcript scrollback, and re-seeds the one-time start-up box so the view
// is byte-identical to a fresh launch at this window size. This IS the session-system wrap the reset
// seam was built for; without a wired host it degrades to the pure view/engine reset it always was.
//
// Ordering: the save is scheduled BEFORE ClearContext so the snapshot it carries reflects the
// conversation being closed, not an emptied one. An interrupted session (InExchange) is then aborted
// between the save and the clear — ClearContext refuses mid-Exchange with ErrInputPending, so the save
// keeps its mid-task state in history and the abort lets the clear accept the boundary. Rotate is
// queued only AFTER ClearContext succeeds — a refused clear leaves the old session open and its id
// live, so no rotate happens on the error path. On success Rotate is unconditional and idempotent on
// an already-inactive session, so a stale active id can never leak into the fresh conversation even
// when the outgoing view held nothing worth saving.
//
// Both of those go through the record-write queue rather than straight at the host, and the rotate
// rides BEHIND the flush there for a reason the synchronous form could not honour: a save already in
// flight (or waiting) when /clear lands would otherwise reach an already-rotated host and mint a
// SECOND id for the outgoing conversation — a duplicate record that the fresh session then keeps
// updating as its own.
//
// Reached only from runCommand at stateIdle (no worker owns the engine), so ClearContext and the
// Snapshot the flush takes are safe. On a ClearContext error the view is left untouched and the failure
// is noted — a fresh-looking view must never lie about an engine that still remembers the old
// conversation; the save already on the queue is harmless (the session was closing anyway).
func (m Model) startNewSession() (tea.Model, tea.Cmd) {
	cmd := m.saveAtIdle() // flush the outgoing session into history before it closes (queued, gated)
	if m.eng.InExchange() {
		// A session interrupted mid-task cannot be cleared — ClearContext refuses mid-Exchange with
		// ErrInputPending — so scrap the open Exchange first. The save above already captured its
		// mid-task state into history (where it stays resumable); this only drops the live engine's
		// copy, exactly as a plain submit on an interrupted session does.
		m.eng.AbortExchange()
	}
	if err := m.eng.ClearContext(); err != nil {
		m.transcript.addNote("could not clear context: " + err.Error())
		m.layout()
		return m, cmd // the flush above still runs: the queue must not be left holding a dispatched write
	}
	// Close the outgoing session so the next Turn's save mints a fresh id. Queued, so it can never
	// overtake the flush above; a no-op when there is no host. At most one of the two Cmds is
	// non-nil — the queue dispatches one write at a time — so this can only ever REPLACE a nil.
	if rotate := m.scheduleWrite(recordWrite{kind: writeRotate}); rotate != nil {
		cmd = rotate
	}
	m.transcript.reset()
	m.transcript.addStartup(newStartupView(m.opts))
	// The clear above was a session boundary, so the engine re-read the workspace context files:
	// the fresh view says what the NEW session is carrying (which is why the notice is reprinted
	// rather than assumed unchanged — the repo's AGENTS.md may have moved since launch).
	m.noteContextFiles()
	// A held interjection queue deliberately SURVIVES the reset (ADR 0025): staged rows are
	// outgoing input, not context — the human wrote them and has not unwritten them — so /clear
	// drops what the model remembers and leaves what is still waiting to be sent.
	m.detached = false // re-arm follow-the-tail: the fresh transcript opens at its tail like a launch
	m.ctxUsed = 0      // the gauge and throughput fall with the discarded conversation…
	m.tokPerSec = 0    // …the same reason compactDoneMsg zeroes them on a fold
	m.genStart = time.Time{}
	m.flash = "" // drop any transient copy note; a new session shows nothing stale
	// The Rotate queued above opens a fresh Session record, and a fresh record names itself: unlatch
	// the naming call, forget that the CLOSED session was named by hand, and drop any title still
	// waiting for an id — it was stashed for the session that just went into history.
	m.autoTitleFired = false
	m.titleTouched = false
	m.pendingTitle = ""
	m.sessionName = "" // the session /clear opens is unnamed until it names itself
	m.layout()
	return m, cmd
}

// runCommand handles a recognised local /command. /continue and /compact open a worker: /continue
// a canned "Please continue" turn, /compact a generative summary call; /clear (and its alias /new)
// resets the session view and reprints the start-up box synchronously and stays idle
// (startNewSession), /settings opens the configuration pane the same synchronous way
// (settings.go), /version records the build version as a note the same synchronous way,
// /skills records the discovered skill catalog the same synchronous way (skills.go), /color-scheme
// lists, switches or exports a palette the same synchronous way (colorscheme.go), and /confine
// reports or swaps Auto's blast radius the same synchronous way (confine.go), /effort reports
// or moves this session's Thinking effort the same synchronous way (effort.go), and /undo previews
// or executes the revert of the last exchange's file writes the same synchronous way (undo.go).
//
// It is reached at stateIdle — where the engine is quiescent and ClearContext/Compact are safe to
// launch — OR, for a reporting line alone, while a worker runs. Its callers own that gate
// ([Model.commandRunnable]); by the time a verb arrives here it is either at a boundary or
// boundary-FREE. The verbs that can arrive mid-run are boundary-free by inspection: /version and
// /skills are synchronous notes touching no engine at all, and /confine's status form reads
// [Engine.ConfineToWorkspace], which the Agent serves under its own RWMutex precisely so the UI may
// ask while a Step dispatches (agent.go — the SetMode class). /effort belongs to that last class in
// EVERY form, not only its reporting one: both doors it drives are served under the Agent's own
// RWMutex, and the override it writes is read when the NEXT request is built, so the Turn already
// in flight is untouched (ADR 0050). Everything else is idle-only and is refused before it gets
// here.
//
// It never touches the editor: the CALLER has already put the box where it belongs, and the two
// callers disagree on purpose. A whole-input invocation arrives from submit, which empties the box
// (the line was nothing but the command). A dropdown accept arrives from acceptAutocomplete, which
// cuts only the accepted "/verb" out and KEEPS the rest of the draft — the whole point of running a
// command at accept.
//
// It takes the whole parsedInput, not just the verb, because a verb with arguments can fail to
// parse: parsed.err is reported as a note (it carries its own usage line) and nothing is driven,
// so a mistyped /confine can never be mistaken for one that took effect.
func (m Model) runCommand(parsed parsedInput) (tea.Model, tea.Cmd) {
	if parsed.err != nil {
		m.transcript.addNote(parsed.err.Error())
		m.layout()
		return m, nil
	}

	// One actuation at a time (ADR 0029 D5). While a launcher verb is in flight the latch refuses
	// every command that would open an Exchange or move the session — the same serialization the
	// facade demands of its caller, and the honest answer for the human: the server is mid-restart,
	// so there is nothing to send to and nothing stable to switch. Everything else stays live.
	if m.actuation.inFlight && actuationBlocked(parsed.command) {
		m.transcript.addNote(m.actuationBlockNote())
		m.layout()
		return m, nil
	}

	// /continue and /compact are the two commands that open an Exchange, so they answer to the
	// heartbeat exactly as a typed message does (blockedUpstream). Which verbs those are is the
	// table's own commandSpec.opensExchange, not a name list here: the purely local verbs below —
	// /clear, /sessions, /version, /confine, /server — stay live while the server is away (moving to
	// another server is the one useful thing to do with an unreachable one); /model consults the
	// heartbeat itself, because "which models are served" is a question only a reachable server can
	// answer (modelSwitchBlocked owns that ladder).
	if m.blockedUpstream() && parsed.opensExchange() {
		m.transcript.addNote(m.upstreamBlockNote())
		m.layout()
		return m, nil
	}

	switch parsed.command {
	case "continue":
		if m.eng.InExchange() {
			// The session was restored mid-task (only ever true right after an interrupted resume —
			// the TUI aborts on every live cancel): /continue resumes the OPEN Exchange rather than
			// opening a new one. Drive Step-only from the boundary (startResume) — no Submit, no new
			// user block; the interrupted note already stands, so the transcript is left untouched.
			m.box = newInterjectBox() // a resumed Exchange is a running one; it takes interjections too
			cmd, cancel := startResume(m.parent, m.eng, m.box, m.notify, m.flushEvents)
			m.cancel = cancel
			m.state = stateRunning
			m.setPlaceholder(runningPlaceholder)
			m.setActivity(actThinking, "", 0, "") // the resumed work is a request in flight (as in submit)
			tick := m.spin.arm()
			return m, tea.Batch(cmd, tick)
		}
		// The canned turn carries no skills: a skill is invoked by naming its /token in a real
		// message, and this turn's text is apogee's own "Please continue", not the human's line.
		// A draft the accept path left standing in the box is still a DRAFT — it carries its own
		// tokens when it is eventually sent, and nothing is silently borrowed from it here.
		m.detached = false // the canned turn re-arms follow-the-tail, exactly as a typed prompt does
		m.transcript.addUser("/continue", nil)
		m.layout()
		m.box = newInterjectBox()
		cmd, cancel := startExchange(m.parent, m.eng,
			domain.UserInput{Text: "Please continue"}, m.box, m.notify, m.flushEvents)
		m.cancel = cancel
		m.state = stateRunning
		m.setPlaceholder(runningPlaceholder)
		m.setActivity(actThinking, "", 0, "") // a canned turn is still a request in flight (as in submit)
		tick := m.spin.arm()
		return m, tea.Batch(cmd, tick)

	case "clear", "new":
		// /new is an alias of /clear: both start a fresh session — wipe the view, reset the engine's
		// memory, and reprint the start-up box (startNewSession).
		return m.startNewSession()

	case "sessions":
		// Open the history-browser overlay: list saved sessions off the Update loop and render the
		// pane above the input (sessions.go). Synchronous and idle-safe like /clear — no worker.
		return m.openSessionBrowser()

	case "settings":
		// Open the configuration pane over the binary's key registry (settings.go). Synchronous and
		// idle-safe like /sessions: it reads one display seam ([SettingsHost.Rows]) and drives no
		// worker, no engine call and no file I/O of its own.
		return m.runSettingsCommand()

	case "usage":
		// Open the per-agent token report (usage.go). The /settings shape with none of its caveats:
		// synchronous, no engine call and no worker — and safe mid-Exchange, because every number it
		// shows is already folded onto this Model.
		return m.runUsageCommand()

	case "inspect":
		// Open the raw-protocol pane over the Inspector's ring (inspector.go). The /usage shape
		// exactly: synchronous, no engine call and no worker, safe mid-Exchange — every record it
		// shows was folded onto this Model when the engine reported it.
		return m.runInspectCommand()

	case "color-scheme":
		// List, switch or export a colour scheme (colorscheme.go, ADR 0040). Synchronous and
		// idle-safe like /settings, whose write and apply seams the switch form reuses in full: no
		// engine call and no worker, only one config key and — for the export — one file.
		return m.runColorScheme(verbArgsOf[colorSchemeArgs](parsed))

	case "rename":
		// Name THIS session: take the argument as the title, or — bare — ask the model for one
		// (autotitle.go). Idle-only because of that bare form: it issues the same out-of-band
		// completion the first prompt fires, and firing one into a live Exchange would contend with
		// the answer being streamed. It drives no worker either way; the generated form answers as a
		// manualTitleMsg.
		return m.runRename(parsed.args)

	case "model":
		// Open the picker over the launcher's Launch profiles when llama-launcher is configured and
		// over what the upstream advertises when it is not, or take the name given as an argument
		// (picker.go). Idle-only either way: the advertised form drives the heartbeat's own rebind
		// path, and the profile form hands a BLOCKING launcher verb to the actuation latch, which the
		// beat after it completes (actuation.go, ADR 0029).
		return m.runModelCommand(parsed.args)

	case "server":
		// Open the server picker over what config.yaml names, or take the server named as an
		// argument (picker.go). Synchronous and idle-safe like /model: the seam it drives mutates
		// the engine and constructs a client, which Agent.SwitchUpstream allows only at a boundary.
		return m.runServerCommand(parsed.args)

	case "unload-model":
		// Free the model of the server this session is talking to. No picker and no argument: the
		// session's own endpoint is the only thing either actuation verb may act on (ADR 0029 D3), and
		// it is read on this loop rather than captured, so the verb acts on where the session is NOW.
		return m.startServerActuation(verbUnload)

	case "stop-server":
		// Stop that server outright. Idle-only like its siblings and latched like a profile load — the
		// call blocks through the launcher's stop escalation — and afterwards the ordinary offline
		// crossing narrates the rest, because the downtime is real (actuation.go, ADR 0029).
		return m.startServerActuation(verbStop)

	case "version":
		// Synchronous like /clear: print the resolved build version (Options.Version, item 1's
		// seam) as a transcript note and stay idle — no upstream call, no worker.
		m.transcript.addNote("apogee " + m.opts.Version)
		m.layout()
		return m, nil

	case "skills":
		// Re-scan the source dirs and print the catalog as a note (skills.go). No upstream call and
		// no worker — it only reports what discovery found — but the walk itself rides a Cmd
		// goroutine like the merged "/" menu's, so the listing lands on that scan's message rather
		// than holding the render loop for the length of a disk walk.
		return m.runSkills()

	case "schedule":
		// List the live Schedules, or put a prompt on a cycle — directly from the argument form,
		// or through the cycle/mode popups (schedule.go). Live mid-run like the reporting verbs:
		// it drives the scheduler library and never this session's engine, and a Firing is a
		// separate headless run over its own Agent (ADR 0033). It takes the line's RAW tail: the
		// prompt is the human's text, and a Firing submits it verbatim.
		return m.runSchedule(parsed.rest)

	case "schedule-stop":
		// Take a Schedule off the clock: the only live one directly, or the one picked from the
		// overlay (schedule.go). Live mid-run for the same reason /schedule is.
		return m.runScheduleStop()

	case "compact":
		// Compaction is a real upstream call (summary generation), so it rides a worker goroutine
		// like /continue rather than blocking the Update loop (ADR 0011). Esc cancels it via
		// stopWorker; the terminal compactDoneMsg records the outcome.
		m.layout() // reflow the input box after the caller emptied it (or cut the accepted verb out)
		// No mailbox: /compact drives no Exchange, so there is nothing to interject INTO. A row
		// staged while it runs stays on the display queue and goes out at the terminal fold.
		m.box = nil
		cmd, cancel := startCompact(m.parent, m.eng)
		m.cancel = cancel
		m.state = stateRunning
		// Typing is live through a compaction too — the row simply waits for the terminal fold
		// (there is no Exchange to interject into), so the legend says "queue" here as well.
		m.setPlaceholder(runningPlaceholder)
		// Compaction emits no Events until it lands, so the phrase is set here or not at all.
		m.setActivity(actCompacting, "", 0, "")
		tick := m.spin.arm()
		return m, tea.Batch(cmd, tick)

	case "confine":
		// Report or swap the blast radius. Synchronous and idle-safe like /clear: no upstream
		// call is involved, only the engine's live flag and (for --save) one config write.
		return m.runConfine(verbArgsOf[confineArgs](parsed))

	case "effort":
		// Report or move this session's Thinking effort (ADR 0050). Synchronous like /confine and
		// safe mid-Exchange for the reason /confine's status form is: the engine door it drives is
		// goroutine-safe and is read when the NEXT request is built, never during the one in flight.
		return m.runEffort(verbArgsOf[effortArgs](parsed))

	case "undo":
		// Preview, or execute, the revert of the last exchange's file writes (undo.go, ADR 0051).
		// Synchronous like /confine — the engine call is a journal read or a batch of restores, no
		// upstream and no worker — but idle-only where /confine's report is not: it WRITES to the
		// workspace, and the group it reverts is the one a running Step is still filling.
		return m.runUndo(verbArgsOf[undoAction](parsed))
	}
	return m, nil
}

// foldCompactDone folds the /compact worker's terminal Msg: note what the fold did, return to
// idle, and flush anything staged while it ran.
//
// On success the history shrank, so reset the gauge to hidden — the next Turn's UsageEvent
// re-measures the smaller fill (foldStats). A skip (conversation too small to fold) touched nothing,
// so leave the gauge as it was and say so plainly rather than claiming a compaction. A failure
// surfaces its reason as a note. Either way the worker is done: return to idle.
//
// A compaction that LANDED is a natural completion, so it flushes like an Exchange does: a row typed
// while /compact ran had no Exchange to be interjected into (the /compact worker drives none, so it
// carries no mailbox) and has been waiting for exactly this boundary. Only a stop or a fault holds —
// a cancelled compaction returns cancelledMsg, not this Msg.
func (m Model) foldCompactDone(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Err != nil:
		m.transcript.addNote("compact: " + msg.Err.Error())
	case msg.Skipped:
		m.transcript.addNote("nothing to compact")
	default:
		m.ctxUsed = 0
		m.transcript.addNote("context compacted")
	}
	cmd := m.finishWorker(stateIdle)
	m.refreshViewport()
	return m.flushAfterCompletion(cmd)
}
