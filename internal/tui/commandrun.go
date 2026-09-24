package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Command running and refusal (ADR 0043)
// ----------------------------------------------------------------------------
//
// The refusal a typed line can meet before anything runs, the gate that decides whether a line
// runs now or is queued for the next idle, the queue's drain, and the drivers that DO run: the
// Exchange launch both send paths share, the session reset /clear means, the /command dispatch
// (its gates, then the row's own run), the adapters a commandSpecs row names its verb through, and
// the verbs with no file of their own (/continue, /compact, /version, /help). Lifted out of
// model.go as one concern: what a recognised verb does and what an unrunnable one is answered with
// are the same question. The parse that classifies the line and the table that declares each verb
// stay in command.go; [Model.submit] stays in model.go with the input concern it belongs to.

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
	return m, nil
}

// commandRunnable reports whether parsed's verb may be driven in the state the Model is in RIGHT
// NOW. It is the one gate the two invocation routes share — ⏎ on a whole-input line
// (stageInterjection) and a dropdown accept (acceptAutocomplete) — so the menu's "— runs at idle"
// tag, the queue a line goes into and what actually happens are three views of a single rule.
//
// At a quiescent boundary every verb is runnable. While a worker owns the engine (m.busy() — the
// same predicate that decides whether Esc stops something) only the reporting lines are:
// parsedInput.safeWhileRunning owns which those are, and it is deliberately asked about the parsed
// LINE rather than the bare verb, because "/confine" and "/confine off" are the same verb and only
// one of them is a report.
func (m Model) commandRunnable(parsed parsedInput) bool {
	return !m.busy() || parsed.safeWhileRunning()
}

// queueCommand stages an idle-only command invoked while a worker works: the parsed line joins
// deferredCommands, to run FIFO at the next idle through the ordinary command path
// (runDeferredCommands), and the band above the box paints it as a "queued command: /verb" row. The
// engine is not told — a queued command is the host's own bookkeeping until it runs (ADR 0031). The
// caller has already put the box where it belongs, exactly as runCommand's callers have: a
// whole-input line emptied it (stageCommand), a dropdown accept cut only the verb token out of the
// draft (acceptAutocomplete), so the rest of a half-written message stays verbatim.
//
// A line that could not run even at idle — a parse error — is not queued: runCommand's own usage
// note answers it right here, as it would at idle, because deferring it would only defer the same
// refusal to a moment the human is no longer looking at the line.
func (m Model) queueCommand(parsed parsedInput) (tea.Model, tea.Cmd) {
	if parsed.err != nil {
		return m.runCommand(parsed)
	}
	m.deferredCommands = append(m.deferredCommands, parsed)
	m.layout() // the band above the box gains a row
	return m, nil
}

// runDeferredCommands drives the commands queued while a worker worked, oldest first, through the
// ordinary command path — the same runCommand a line typed at idle reaches, so a queued /clear
// resets the session exactly as a typed one does. It is called at every transition into idle: the
// natural completions (drainThenFlush), the stop (foldCancelled) and the errored → idle ⏎
// dismissal, and it runs BEFORE any held or staged message is sent from that idle, so a queued
// /clear clears before a queued message lands (ADR 0025 D7 and D10, amended 2026-09-14).
//
// The drain stops at the first verb that leaves the Model busy — /compact starts its worker,
// /continue opens an Exchange — because the next verb would be driven against a worker that owns
// the engine: never two workers on one Agent. What is left queued waits for that worker's own
// terminal fold, which drains again, so a /clear queued behind a /compact still runs, in order,
// once the compaction lands. A deferred quit runs nothing: the queued commands are
// session-ephemeral like the staged rows (ADR 0025), and a program that is exiting has no session
// to run them in.
func (m Model) runDeferredCommands() (Model, tea.Cmd) {
	var cmds []tea.Cmd
	for len(m.deferredCommands) > 0 && !m.busy() && !m.quitting {
		parsed := m.deferredCommands[0]
		m.deferredCommands = m.deferredCommands[1:]
		if len(m.deferredCommands) == 0 {
			m.deferredCommands = nil
		}
		next, cmd := m.runCommand(parsed)
		m = next.(Model)
		cmds = append(cmds, cmd)
	}
	m.layout() // the band above the box loses the rows that ran
	return m, tea.Batch(cmds...)
}

// launchExchange starts the worker over one Exchange and moves the Model into stateRunning
// through the one launch verb (enterRunning, model.go): a fresh mailbox for what the human types
// while it runs, the worker Cmd and the CancelFunc the stop key calls (C4), the state the emptied
// box derives its queue legend from, the opening "thinking" phrase, and the spinner tick — batched as the one Cmd
// the caller returns.
//
// It is the tail the two send paths share — a typed submit and an interjection flush — so a
// message the queue sends enters exactly the state a typed one does. Everything upstream of it
// stays the caller's: the parse, the upstream and InExchange guards, the transcript block, and
// what happens to the editor (a flush at a natural completion deliberately leaves a half-typed
// line alone).
//
// The mailbox is fresh per Exchange: this worker is the only one that will ever drain it, and it
// dies with the Exchange (finishWorker clears it), so a row can never be delivered into an
// Exchange other than the one it was typed during.
func (m Model) launchExchange(in domain.UserInput) (tea.Model, tea.Cmd) {
	box := newInterjectBox()
	cmd, cancel := startExchange(m.parent, m.eng, in, box, m.notify, m.flushEvents)
	batch := m.enterRunning(cmd, cancel, box, actThinking)
	return m, batch
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
//
// A pre-bound session takes the view-only half instead (resetSessionView): there is no engine to
// flush from, no Exchange to abort and no context to clear, and the unbound holder answers all three
// with errNoServerBound — which used to surface as a "could not clear context" note over a view the
// reset never reached. Options.Prebound is left standing, so the reason, its start-up box and the
// picker the next send re-opens all survive the reset.
//
// The one pre-bound session that does NOT take it is a resumed one (--resume/--continue): the record
// is seeded into the Agent the LATER bind builds, so a fresh-looking view there would be lying about
// an engine that comes back remembering the whole resumed conversation — the same lie the error path
// above refuses to tell. With a resume pending the branch is skipped and today's refusal note stands.
func (m Model) startNewSession() (tea.Model, tea.Cmd) {
	if m.prebound() && m.opts.Resumed == nil {
		// Nothing to flush, abort or clear while no engine exists, so the reset is the view's alone.
		m.resetSessionView()
		return m, nil
	}
	cmd := m.saveAtIdle() // flush the outgoing session into history before it closes (queued, gated)
	if m.eng.InExchange() {
		// A session interrupted mid-task cannot be cleared — ClearContext refuses mid-Exchange with
		// ErrInputPending — so scrap the open Exchange first. The save above already captured its
		// mid-task state into history (where it stays resumable); this only drops the live engine's
		// copy. /clear is the ONE close that still aborts rather than settles: the human asked for
		// the conversation to be gone, finished Turns included, where a cancel or a fresh message on
		// the interrupted session keeps them (Engine.SettleExchange).
		m.eng.AbortExchange()
	}
	if err := m.eng.ClearContext(); err != nil {
		m.transcript.addNote("could not clear context: " + err.Error())
		return m, cmd // the flush above still runs: the queue must not be left holding a dispatched write
	}
	// Close the outgoing session so the next Turn's save mints a fresh id. Queued, so it can never
	// overtake the flush above; a no-op when there is no host. At most one of the two Cmds is
	// non-nil — the queue dispatches one write at a time — so this can only ever REPLACE a nil.
	if rotate := m.scheduleWrite(recordWrite{kind: writeRotate}); rotate != nil {
		cmd = rotate
	}
	m.resetSessionView()
	return m, cmd
}

// resetSessionView wipes what the closed conversation owned out of the view and re-seeds the
// one-time start-up box, so the frame is byte-identical to a fresh launch at this window size. It
// touches no engine and queues no write — both halves of /clear end here: the bound one once the
// engine has accepted the clear, and the pre-bound one, which has no engine to accept it and for
// which this is the whole of the reset.
func (m *Model) resetSessionView() {
	// The suggestion band's spent set falls with the conversation it was advising (suggestband.go):
	// the skills it named in the closed session are advice the human has not been given in this one.
	// A refused clear returns before this runs, so the set survives exactly as long as the session it
	// belongs to does.
	m.spentSkills = nil
	m.transcript.reset()
	m.transcript.addStartup(newStartupView(m.opts))
	// A bound reset's clear was a session boundary, so the engine re-read the workspace context files:
	// the fresh view says what the NEW session is carrying (which is why the notice is reprinted
	// rather than assumed unchanged — the repo's AGENTS.md may have moved since launch). A pre-bound
	// session has no engine to ask and the unbound holder's empty report adds nothing.
	m.noteContextFiles()
	// The conversation the reset threw away took every run inside it, so no open run view still names
	// entries the transcript holds: the stack falls whole and the paint is re-rooted at the top level
	// (runview.go states the rule once, for every reset that goes through it).
	m.reseatViewStack()
	// A held interjection queue deliberately SURVIVES the reset (ADR 0025): staged rows are
	// outgoing input, not context — the human wrote them and has not unwritten them — so /clear
	// drops what the model remembers and leaves what is still waiting to be sent.
	m.detached = false // re-arm follow-the-tail: the fresh transcript opens at its tail like a launch
	// The gauge, the generation clock and the throughput fall with the discarded conversation — the
	// same reason compactDoneMsg zeroes the gauge on a fold.
	m.liveStats.reset()
	// So does the CUMULATIVE accounting beside them: the sums belong to the session just closed, and
	// its record took them (saveAtIdle runs BEFORE this reset, so the closing record keeps its own
	// tally). The engine zeroes its tally at the same boundary (ClearContext, like RestoreSession),
	// so the base the fold adds its readings onto is zero from here — a resumed record's offset was
	// that record's, not this session's — and the /usage pane and the first save of the fresh
	// session report only what the fresh session spends. The delegate half falls the same way: the
	// run heads it stood in for went with the transcript above, and a restored record's sum was the
	// closed session's. Until 2026-09-15 the three stood across this boundary, so a fresh session
	// inherited the closed one's spend (the 2026-08-20 deferred defect this closes).
	m.usage = domain.Usage{}
	m.usageBase = domain.Usage{}
	m.delegateUsage = domain.Usage{}
	// The models that answered fall with the tallies they qualify: they were the closed session's
	// answerers, and its record took them with the same saveAtIdle above.
	m.servedModels = nil
	m.flash = "" // drop any transient copy note; a new session shows nothing stale
	// A bound reset queues a Rotate above, which opens a fresh Session record, and a fresh record
	// names itself; a pre-bound one had no session to rotate. Either way: unlatch the naming call,
	// forget that the CLOSED session was named by hand, and drop any title still waiting for an id —
	// it was stashed for the session that just went into history.
	m.autoTitleFired = false
	m.titleTouched = false
	m.pendingTitle.drop()
	m.sessionName = "" // the session /clear opens is unnamed until it names itself
	// The cached boundary belongs to the conversation just closed, and the fresh session's
	// transcript must never be paired with it (progressSave). It is forgotten rather than refreshed:
	// the next worker launch caches the boundary the new session actually starts from.
	m.boundary = domain.Session{}
	m.hasBoundary = false
}

// runCommand drives a recognised local /command. Past its three gates it hands the line to the
// verb's own row: commandSpec.run (command.go) is what the verb DOES, declared beside what the
// parser reads for it, how the menu offers it and which gates it answers to, so there is no
// per-verb switch here to fall out of step with the table. A name the table does not carry drives
// nothing — the table is the authority, as it is for every other reader of a row.
//
// It is reached at stateIdle — where the engine is quiescent and ClearContext/Compact are safe to
// launch — OR, for a reporting line alone, while a worker runs. Its callers own that gate
// ([Model.commandRunnable]); by the time a verb arrives here it is either at a boundary or
// boundary-FREE — a line that is neither is queued (queueCommand) and arrives here at the next
// idle instead. The verbs that can arrive mid-run are boundary-free by inspection: /version and
// /skills' LISTING form are synchronous notes touching no engine at all (/skills export writes a
// file and is idle-only, the /confine split one clause on), and /confine's status form reads
// [Engine.ConfineToWorkspace], which the Agent serves under its own RWMutex precisely so the UI may
// ask while a Step dispatches (agent.go — the SetMode class). /effort belongs to that last class in
// both of its halves: the verb itself only opens a popup, and the accept behind it drives two doors
// the Agent serves under that same RWMutex, writing an override that is read when the NEXT request
// is built — so the Turn already in flight is untouched (ADR 0050). Everything else is idle-only
// and waits in deferredCommands until it may get here.
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
		return m, nil
	}

	// One actuation at a time (ADR 0029 D5). While a launcher verb is in flight the latch refuses
	// every command that would open an Exchange or move the session — the same serialization the
	// facade demands of its caller, and the honest answer for the human: the server is mid-restart,
	// so there is nothing to send to and nothing stable to switch. Everything else stays live.
	if m.actuation.inFlight && actuationBlocked(parsed.command) {
		m.transcript.addNote(m.actuationBlockNote())
		return m, nil
	}

	// /continue and /compact are the two commands that open an Exchange, so they answer to the
	// heartbeat exactly as a typed message does (blockedUpstream). Which verbs those are is the
	// table's own commandSpec.opensExchange, not a name list here: the purely local verbs —
	// /clear, /sessions, /version, /confine, /server — stay live while the server is away (moving to
	// another server is the one useful thing to do with an unreachable one); /model consults the
	// heartbeat itself, because "which models are served" is a question only a reachable server can
	// answer (modelSwitchBlocked owns that ladder).
	if m.blockedUpstream() && parsed.opensExchange() {
		m.transcript.addNote(m.upstreamBlockNote())
		return m, nil
	}

	spec, ok := commandByName(parsed.command)
	if !ok {
		return m, nil
	}
	return spec.run(m, parsed)
}

// commandRun is the shape of commandSpec.run: the Model the verb acts on and the whole parsed
// line, answered the way every Update path answers. A row never writes one by hand — it names the
// verb's own method through the adapter that reads the part of the line that verb takes
// ([bareVerb], [tokenVerb], [restVerb], [typedVerb], [actuationVerb]), so each method keeps the
// signature its own argument shape asks for and the table stays one line per verb.
type commandRun func(Model, parsedInput) (tea.Model, tea.Cmd)

// bareVerb adapts a verb that reads nothing off its line — the rows without takesArgs, whose
// surplus tokens are ignored as they always were, and /effort, whose level grammar went with the
// picker (commandSpecs).
func bareVerb(run func(Model) (tea.Model, tea.Cmd)) commandRun {
	return func(m Model, _ parsedInput) (tea.Model, tea.Cmd) { return run(m) }
}

// tokenVerb adapts a verb that reads its argument tokens (parsedInput.args) and declares no grammar
// of its own: /rename's words and the one optional name of /model, /server and /sub-agents-server.
func tokenVerb(run func(Model, []string) (tea.Model, tea.Cmd)) commandRun {
	return func(m Model, parsed parsedInput) (tea.Model, tea.Cmd) { return run(m, parsed.args) }
}

// restVerb adapts a verb that reads the line's RAW tail (parsedInput.rest) rather than its tokens:
// /schedule, whose prompt must reach the model spaced and lined as it was typed.
func restVerb(run func(Model, string) (tea.Model, tea.Cmd)) commandRun {
	return func(m Model, parsed parsedInput) (tea.Model, tea.Cmd) { return run(m, parsed.rest) }
}

// typedVerb adapts a verb that declares a grammar of its own (commandSpec.parseArgs): it reads the
// opaque parse back as the type the verb's method takes ([verbArgsOf]), so the row's parseArgs
// (through [verbGrammar]) and its run are the write and read sides of the same value. T is inferred
// from the method; a row whose two sides named different types would read the zero value — the
// bare form — on every line, which is why the pair sits side by side on the one row.
func typedVerb[T any](run func(Model, T) (tea.Model, tea.Cmd)) commandRun {
	return func(m Model, parsed parsedInput) (tea.Model, tea.Cmd) { return run(m, verbArgsOf[T](parsed)) }
}

// actuationVerb adapts the two verbs that act on the server this session is talking to —
// /unload-model frees its model, /stop-server stops it outright (actuation.go, ADR 0029). No picker
// and no argument: the session's own endpoint is the only thing either verb may act on (ADR 0029
// D3), and [Model.startServerActuation] reads it on this loop rather than capturing it, so the verb
// acts on where the session is NOW. Both are idle-only and latched like a profile load — the stop
// blocks through the launcher's escalation — and after a stop the ordinary offline crossing
// narrates the rest, because the downtime is real.
func actuationVerb(verb string) commandRun {
	return func(m Model, _ parsedInput) (tea.Model, tea.Cmd) { return m.startServerActuation(verb) }
}

// runContinue drives /continue: the canned "Please continue" turn, or — on a session restored
// mid-task — the resumption of the Exchange that was left open. It opens an Exchange, so it rides a
// worker and answers to the heartbeat and the actuation latch like a typed message (runCommand's
// gates, read off commandSpec.opensExchange).
//
// InExchange is only ever true right after an interrupted resume — the TUI aborts on every live
// cancel — and then the verb resumes the OPEN Exchange rather than opening a new one: Step-only from
// the boundary (startResume), no Submit and no new user block, because the interrupted note already
// stands and the transcript is left untouched.
//
// The canned turn carries no skills: a skill is invoked by naming its /token in a real message, and
// this turn's text is apogee's own "Please continue", not the human's line. A draft the accept path
// left standing in the box is still a DRAFT — it carries its own tokens when it is eventually sent,
// and nothing is silently borrowed from it here. The order is the typed prompt's: follow-the-tail
// re-armed, then the user block, then the launch.
func (m Model) runContinue() (tea.Model, tea.Cmd) {
	if m.eng.InExchange() {
		box := newInterjectBox() // a resumed Exchange is a running one; it takes interjections too
		cmd, cancel := startResume(m.parent, m.eng, box, m.notify, m.flushEvents)
		batch := m.enterRunning(cmd, cancel, box, actThinking) // the resumed work is a request in flight (as in submit)
		return m, batch
	}
	m.detached = false // the canned turn re-arms follow-the-tail, exactly as a typed prompt does
	m.transcript.addUser("/continue", nil)
	box := newInterjectBox() // the canned turn is a launch like any other (launchExchange)
	cmd, cancel := startExchange(m.parent, m.eng,
		domain.UserInput{Text: "Please continue"}, box, m.notify, m.flushEvents)
	batch := m.enterRunning(cmd, cancel, box, actThinking) // a canned turn is still a request in flight (as in submit)
	return m, batch
}

// runCompact drives /compact. Compaction is a real upstream call (summary generation), so it rides
// a worker goroutine like /continue rather than blocking the Update loop (ADR 0011). Esc cancels it
// via stopWorker; the terminal compactDoneMsg records the outcome ([Model.foldCompactDone]).
//
// No mailbox: /compact drives no Exchange, so there is nothing to interject INTO. A row staged while
// it runs stays on the display queue and goes out at the terminal fold. The Bridge is told the same
// (the nil box enterRunning installs): there is no Exchange for the seam to pre-empt in. Typing is
// live through a compaction too — the row simply waits for the terminal fold — so the legend,
// derived from stateRunning at paint, says "queue" here as well; and compaction emits no Events
// until it lands, so the phrase the verb sets is the one that stands until then.
func (m Model) runCompact() (tea.Model, tea.Cmd) {
	m.layout() // reflow the input box after the caller emptied it (or cut the accepted verb out); the verb lays nothing out (enterRunning)
	cmd, cancel := startCompact(m.parent, m.eng)
	batch := m.enterRunning(cmd, cancel, nil, actCompacting)
	return m, batch
}

// runVersion drives /version: the resolved build version (Options.Version) as a transcript note.
// Synchronous like /clear and safe mid-run — no upstream call, no worker, no engine.
func (m Model) runVersion() (tea.Model, tea.Cmd) {
	m.transcript.addNote("apogee " + m.opts.Version)
	return m, nil
}

// runHelp drives /help: every verb of the registry with its summary, then the key legend (help.go),
// as a transcript note. Synchronous like /version — no upstream call, no worker. The legend reads the
// editor's key-disambiguation flag so it names the newline chord the box itself advertises.
func (m Model) runHelp() (tea.Model, tea.Cmd) {
	m.transcript.addNote(helpNote(m.keyDisambiguation))
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
// A compaction that LANDED is a natural completion, so it drains and flushes like an Exchange does:
// the commands queued while /compact ran go first (runDeferredCommands), then a row typed while it
// ran — which had no Exchange to be interjected into (the /compact worker drives none, so it carries
// no mailbox) and has been waiting for exactly this boundary — goes out. Only a stop or a fault holds
// — a cancelled compaction returns cancelledMsg, not this Msg.
func (m Model) foldCompactDone(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Err != nil:
		m.transcript.addNote("compact: " + msg.Err.Error())
	case msg.Skipped:
		m.transcript.addNote("nothing to compact")
	default:
		m.ctxUsed = 0
		// Under its own kind, so the record can find the fold (transcript.addCompacted). The
		// maintenance reading the summary call emitted has already folded through foldStats, which
		// skipped its note because this worker was running — this is the one note /compact leaves.
		m.transcript.addCompacted(runRef{})
	}
	cmd := m.finishWorker(stateIdle)
	return m.drainThenFlush(cmd)
}
