package tui

import (
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
)

// ----------------------------------------------------------------------------
// The Model (phase-2 detail plan §4 P2.2; ADR 0011)
// ----------------------------------------------------------------------------

// uiState is the model's explicit lifecycle state. Every keypress and seam Msg is
// interpreted against it, and it is the single source of truth for what the View renders
// and which keys are live. The four states are exactly those the plan calls for.
type uiState int

const (
	stateIdle             uiState = iota // awaiting input; the worker is not running
	stateRunning                         // a worker drives an Exchange; typing stages an interjection
	stateAwaitingApproval                // a tool call is blocked on the human's decision
	stateAwaitingAsk                     // an ask_user question is blocked on the human's typed answer
	stateErrored                         // the worker returned a loop-level error
)

// Model is the Bubble Tea model: a thin renderer over the agent's Events that owns the
// input box, the transcript viewport, and the status line. It holds the narrow [Engine]
// and the display [Options] but no agent logic — it folds the five seam Msgs and
// keypresses into state and renders, and drives the engine only by launching the worker
// (C1/C5; ADR 0011). It is a value type with value-receiver Bubble Tea methods, copied on
// every Update, per the framework's idiom.
type Model struct {
	// Wiring resolved by the composition root and handed in at construction.
	parent   context.Context // the program's context; the worker derives from it (C4)
	eng      Engine
	opts     Options
	sessions SessionHost   // persists the session per-Turn, at idle, and on quit; nil ⇒ off
	notify   func(tea.Msg) // sends a Msg into the running program from the worker goroutine (the per-Turn snapshot)

	// Session-save single-flight (the per-Turn pipeline). A save runs off the Update loop on a
	// Cmd goroutine; saveBusy marks one in flight so a later snapshot coalesces into pendingSave
	// (latest-wins) rather than overlapping. saveFailing tracks the last save's success so only
	// the ok→fail and fail→ok transitions are noted — a save failure must never spam the
	// transcript or interrupt the conversation.
	saveBusy    bool
	saveFailing bool
	pendingSave *savePayload

	// sessionBrowser is the /sessions history-browser overlay's state (sessions.go): the loaded
	// session metas, the current selection, the current-workspace ⇄ all toggle, and any inline
	// delete-confirm or rename edit. Its zero value is "closed", so it lives inline in the
	// value-copied Model like autocompleteState (ADR 0011). It is driven only at idle.
	sessionBrowser sessionBrowser

	// promptEditor owns the chat input cluster — the textarea, the autocomplete overlay (+ its
	// skillRegion edge-trigger), the workspace file cache, and the prompt drag-selection
	// (prompteditor.go). It is embedded ANONYMOUSLY so its fields and its
	// self-contained methods promote onto the Model (m.input, m.autocomplete, m.caretTo(...) all
	// resolve through it): the value-copied Model idiom and every existing call site stay unchanged
	// while the input state gains its own home. Model state the editor does not own — theme,
	// width/height, opts, lifecycle — stays on the Model rather than be duplicated onto the editor.
	promptEditor

	// Sub-models (Bubbles widgets). The input textarea lives on the embedded promptEditor above.
	viewport viewport.Model

	// spin is the status-line spinner's animation state — the style, the colour flag, the frame
	// counter, and the tick-chain generation (spinner.go). Plain ints in a plain value, so it is
	// safe inside the value-copied Model, and every frame is a pure function of the counter rather
	// than of an RNG handle a copy would share (ADR 0011).
	spin spinnerAnim

	// hb is the upstream heartbeat's state — the tick chain's generation, the offline debounce,
	// and what the last landed beat observed (see heartbeatState). Plain values and one slice, so
	// it is safe inside the value-copied Model (ADR 0011). It stays zero when Options.Heartbeat is
	// unwired, and every reader treats that zero value as "no monitor, nothing to say".
	hb heartbeatState

	// Lifecycle.
	state      uiState
	cancel     context.CancelFunc // non-nil while a worker runs; the stop key calls it (C4)
	pending    *approvalReqMsg    // the in-flight Approval while awaitingApproval (P2.4 acts on it)
	pendingAsk *askReqMsg         // the in-flight ask_user question while awaitingAsk (P3.11)
	askSel     int                // the highlighted ask_user choice index while awaitingAsk (D5); bare int, no no-copy type (ADR 0011)
	lastErr    error              // the error behind stateErrored, shown in the status line
	lastCtrlC  time.Time          // when the last Ctrl+C landed; a second within the window quits
	quitting   bool               // a quit requested while busy; the exit waits for the worker's terminal Msg (C4)

	// The interjection queue — what the human typed while the model worked (ADR 0025). It is
	// kept in two reconciled copies, which is what lets one goroutine own each half:
	//
	// box is the running Exchange's mailbox, held BY POINTER because it carries a mutex and the
	// Model is value-copied on every Update (doc.go's no-copy invariant). It is created fresh
	// per Exchange, handed to that Exchange's worker, and cleared at the terminal fold: non-nil
	// means "a worker is draining this", nil means there is nothing to deliver into right now
	// (idle, or the /compact worker, which drives no Exchange).
	//
	// pendingInterjections is the display copy and the queue of record — a plain slice, safe in
	// the copied Model. Rows are appended here and pushed to the box together; the worker's
	// interjectedMsg then removes by id exactly the rows that were COMMITTED, so a row that did
	// not land stays queued rather than vanishing. What is left when the Exchange ends is ruled on
	// there: a natural completion flushes it into a new Exchange, a stop or a fault holds it for
	// the next ⏎ (flushAfterCompletion / noteHeldQueue). Rows are session-ephemeral: sessions
	// record what was committed (ADR 0022), never what is still waiting to be sent.
	//
	// interjectSeq mints those ids — a plain counter, incremented on the Update goroutine and
	// never reset, so an id names one row for the life of the session and a delivery report can
	// never be reconciled against a row that merely reused a number.
	box                  *interjectBox
	pendingInterjections []queuedInterjection
	interjectSeq         int

	// act is the live activity the status line renders while a worker runs — thinking,
	// responding, a named tool, retrying, compacting, stopping (activity.go). It is derived
	// from the Event stream (foldActivity) plus the transitions no Event announces, and it is
	// deliberately NOT a uiState: the lifecycle machine above is untouched by it (ADR 0011).
	act activity

	// Live stats folded from the engine's UsageEvent (server token accounting). ctxUsed is
	// the latest top-level (Depth 0) total-token count, driving the context-usage gauge;
	// genStart marks when the current Turn began streaming content (set on its first token,
	// cleared on a re-stream or once usage lands) and tokPerSec is the last completion's
	// throughput, timed against the Update clock. All value/zero-safe in the copied Model.
	ctxUsed   int
	genStart  time.Time
	tokPerSec float64

	// transcriptSel is the transcript viewport's screen-space drag-selection (mouse.go), anchored
	// in content coordinates into m.lines; the zero value is "no selection". A re-render keeps it
	// exactly while the lines it spans are unchanged (refreshViewport's keep-if-unchanged rule), so
	// it outlives the stream appending below it and drops the moment its own ground moves — its
	// content-anchored coords therefore never index lines that say something else. It and sel never
	// coexist (region arbitration).
	transcriptSel transcriptSel
	// flash is a transient status-line note (e.g. "copied 12 chars") shown after a mouse copy and
	// cleared by flashClearMsg after flashDuration.
	flash string

	// Content & layout.
	transcript    transcript
	th            theme // the palette and reusable styles, built once at construction
	width, height int
	ready         bool // a WindowSizeMsg has sized the layout at least once
	// detached says the human scrolled off the tail: refreshViewport holds the scroll position
	// where they put it instead of following new content, until a re-arm event (submit, /clear,
	// /continue, a flushed interjection, a resumed session) clears it. The zero value is
	// attached — a fresh Model follows the tail with no construction help. A plain bool by
	// ADR 0011: the Model is copied by value on every Update.
	detached bool

	// Last render output, stashed by refreshViewport for View's sticky-header overlay: the
	// physical lines the viewport holds and the line range of every user block.
	lines      []string
	userBlocks []userBlock
}

// newModel builds the initial idle Model. parent is the program context the worker derives
// its cancellable child from (so a program-wide shutdown also cancels an in-flight
// Exchange — C4). The input box is focused here, not in Init: Init takes a COPY of the Model
// and returns only a Cmd, so the focus *state* has to be set on the stored widget. The caret it
// is focused for is the real terminal cursor in Options.CursorShape (newPromptEditor).
func newModel(parent context.Context, eng Engine, opts Options, notify func(tea.Msg)) Model {
	th := newTheme()

	vp := viewport.New()
	vp.SoftWrap = true // wrap long transcript lines to the viewport width

	m := Model{
		parent:       parent,
		eng:          eng,
		opts:         opts,
		sessions:     opts.Sessions,
		notify:       notify,
		promptEditor: newPromptEditor(opts.CursorShape),
		viewport:     vp,
		spin:         newSpinnerAnim(opts.Spinner, opts.SpinnerColor),
		th:           th,
		state:        stateIdle,
	}

	// Arm the heartbeat's tick chain when the monitor is wired: generation 1 is the chain Init's
	// first beat belongs to. An unwired seam leaves it at 0 — the generation no beat ever carries,
	// so a stray Msg cannot be folded into a Model that monitors nothing.
	if opts.Heartbeat != nil {
		m.hb.gen = 1
	}

	// Seed the one-time start-up box as entries[0]. Seeding it here (rather than on the first
	// WindowSizeMsg) makes it a normal transcript entry: it renders fresh at the live width on
	// every repaint, so it reflows on resize with no "already shown" guard, and /clear re-seeds it
	// through the same helper (startNewSession) to reprint a fresh-launch view.
	m.transcript.addStartup(newStartupView(opts))
	// A resumed run then repaints the stored scrollback beneath that box and relights the gauge
	// (a no-op on a fresh start, when opts.Resumed is nil).
	m.replayResumed(opts.Resumed)
	// Construction is the session's first boundary, so the workspace context files the engine
	// loaded are reported here — last, so the notice closes the opening frame rather than
	// separating the box from the scrollback it belongs to.
	m.noteContextFiles()
	return m
}

// replayResumed repaints a resumed session's scrollback beneath the fresh start-up box: it relights
// the context gauge from the stored fill, decodes the stored transcript blob, and appends its
// committed entries closed by a "resumed: <title>" note. A decode error or a legacy record whose
// blob is empty (no scrollback was recorded) is never fatal — the view is left fresh and an honest
// note says the model still remembers even though the scrollback could not be repainted. A session
// restored mid-task (r.InExchange, set by the binary from the resumed Agent) closes with the
// interrupted note, telling the human /continue picks the work back up — the same note the /sessions
// browser appends on a mid-Exchange resume. A nil payload (a fresh start) is a no-op. It runs at
// construction, before any WindowSizeMsg, so the replayed entries are present the first time the
// viewport lays out.
func (m *Model) replayResumed(r *ResumedSession) {
	if r == nil {
		return
	}
	m.ctxUsed = r.CtxUsed // relight the gauge near the session's last observed fill
	entries, err := decodeTranscript(r.Transcript)
	if err != nil || len(entries) == 0 {
		m.transcript.addNote("resumed: " + r.Title + " (no scrollback recorded — the model still remembers)")
	} else {
		m.transcript.replay(entries)
		m.transcript.addNote("resumed: " + r.Title)
	}
	if r.InExchange {
		m.transcript.addNote(interruptedNote)
	}
}

// noteContextFiles records what the session that just started loaded from the workspace: one
// note naming every context file and its size, a note of its own for each file that is present
// but unreadable (the loud skip), and — when the standing system content has outgrown the window
// share allocated to it — the warning that says so. It is called at each of the three boundaries
// the host owns, immediately after the boundary reseeded the view: construction, /clear|/new
// (startNewSession), and a /sessions restore. Each of those re-read the files, so the notice
// always describes the bytes the NEXT request will actually carry.
//
// A repo with none of the configured names is the common case, and it stays completely silent —
// no note at all, not an empty one. The warning is advisory: nothing is capped or truncated, and
// a window nobody could name yet reports a zero share, which never warns (Oversize).
//
// The name traces to config and the error text to the filesystem, so both are escape-stripped on
// the way to the terminal exactly as the start-up box strips its host and model.
func (m *Model) noteContextFiles() {
	report := m.eng.ContextFilesReport()
	if len(report.Files) == 0 {
		return
	}

	loaded := make([]string, 0, len(report.Files))
	unreadable := make([]string, 0, len(report.Files))
	for _, f := range report.Files {
		name := stripEscapes(f.Name)
		if f.Err != "" {
			unreadable = append(unreadable, "context: "+name+" unreadable — "+stripEscapes(f.Err))
			continue
		}
		loaded = append(loaded, name+" ("+formatBytes(f.Bytes)+")")
	}

	if len(loaded) > 0 {
		m.transcript.addNote("context: " + strings.Join(loaded, ", "))
	}
	for _, note := range unreadable {
		m.transcript.addNote(note)
	}
	if report.Oversize() {
		m.transcript.addNote("standing system content ~" + formatTokensFine(report.StandingTokens) +
			" tokens exceeds its Budget share (~" + formatTokensFine(report.SystemShare) +
			") — trim context files or the system prompt")
	}
}

// blackenInput gives the textarea the black interior the layout calls for: the base, text,
// cursor line, and placeholder all sit on a black background so the box reads as one solid
// field inside its dark-gray border, on any terminal theme.
func blackenInput(ta *textarea.Model) {
	s := ta.Styles()
	for _, st := range []*textarea.StyleState{&s.Focused, &s.Blurred} {
		st.Base = st.Base.Background(colBlack)
		st.Text = st.Text.Background(colBlack)
		st.CursorLine = st.CursorLine.Background(colBlack)
		st.Placeholder = st.Placeholder.Background(colBlack)
		st.EndOfBuffer = st.EndOfBuffer.Background(colBlack)
	}
	ta.SetStyles(s)
}

// steadyCursor retires the textarea's simulated cursor in favour of the terminal's own: the widget
// stops painting a blinking block into its content (SetVirtualCursor(false)) and [Model.View] hands
// Bubble Tea a real cursor at the caret instead, styled from here — which is what makes the caret
// the one the human's terminal draws rather than an imitation of it (ISSUES: "I don't want anything
// blinking"). shape is the `cursor-shape` config key's selection ([Options.CursorShape]); Blink is
// false with no key of its own, because nothing in apogee blinks. Color is left nil deliberately: a
// colour set here would override the cursor colour the terminal is configured with, and the shape is
// the only axis this feature claims.
func steadyCursor(ta *textarea.Model, shape tea.CursorShape) {
	ta.SetVirtualCursor(false)
	s := ta.Styles()
	s.Cursor = textarea.CursorStyle{Shape: shape, Blink: false}
	ta.SetStyles(s)
}

// Init fires the first heartbeat, and that is the whole of its work. The window is sized by the
// first WindowSizeMsg the program sends, the input's focus STATE is set at construction (newModel —
// Init holds a copy, so it could not set it here), and the caret needs no start-up Cmd either: the
// virtual cursor is retired, so there is no blink to schedule and nothing to animate (steadyCursor,
// View). The spinner likewise ticks only while running. The beat goes out immediately rather than
// after one Interval, because startup discovery IS the first beat (the binary no longer probes
// before painting): the sooner it lands, the sooner the footer stops saying "connecting…". With the
// monitor unwired beatCmd is nil, so an unwired TUI starts no chain at all — exactly as before.
func (m Model) Init() tea.Cmd {
	return m.beatCmd()
}

// ----------------------------------------------------------------------------
// Update — fold the five seam Msgs + keypresses (C1–C4)
// ----------------------------------------------------------------------------

// Update folds exactly the messages ADR 0011 defines — the five worker→Update Msgs
// (eventMsg / approvalReqMsg / exchangeDoneMsg / cancelledMsg / errMsg), keypresses, the
// window size, and the spinner tick — and nothing else. It mutates the local copy and
// returns it. It contains no agent logic: a submit launches the worker (C1), the seam Msgs
// move the state machine and render, and the stop key cancels (C4).
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.sel = promptSel{} // the box moved/reflowed: its stored visual coords are stale
		m.layout()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.PasteMsg:
		// Bracketed paste (default-on in bubbletea v2) is an edit, so it must run the same
		// post-edit refresh a keypress does — without it a multi-line paste renders unwrapped
		// until the next key, the autocomplete overlay is computed from stale input, and a live
		// drag-selection's cached cell offsets no longer match the value (a later copy would take
		// the wrong runes). It lands wherever the box is editable — including while a worker runs,
		// where the pasted text is staged as an interjection (ADR 0025) — and is dropped at the
		// inert states, exactly as keys are. Mirrors handleKey's edit path.
		if !m.inputEditable() {
			return m, nil
		}
		m.sel = promptSel{} // the value is about to change; drop the selection before its coords go stale
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.state == stateIdle || m.state == stateRunning {
			m = m.recomputeAutocomplete() // re-derive the overlay from the pasted-into input (reloads on /skill open)
		}
		m.layout() // re-flow: the box auto-grows as the pasted text wraps to more rows
		return m, cmd

	case ctrlCResetMsg:
		// The Ctrl+C quit window elapsed without a second press: disarm the gesture so the
		// "press ctrl+c again to quit" hint clears (handleKey's ctrl+c case).
		m.lastCtrlC = time.Time{}
		return m, nil

	case eventMsg:
		m = m.foldEvent(msg.Event)
		m.refreshViewport()
		return m, nil

	case approvalReqMsg:
		// The worker's Approver hands the gate to the Update loop; this case records the
		// request and switches state. View renders the prompt and handleApprovalKey replies
		// on msg.Reply (the C3 rendezvous; P2.4).
		m.state = stateAwaitingApproval
		m.pending = &msg
		return m, nil

	case askReqMsg:
		// The worker's Asker hands a free-text question to the Update loop; record it, switch
		// state, and re-focus the (emptied) input so the human types the answer. View renders
		// the question; submitAnswer replies on msg.Reply when the human submits (P3.11).
		m.state = stateAwaitingAsk
		m.pendingAsk = &msg
		m.askSel = 0 // first choice pre-selected while the input is empty (D5); no-op when there are no choices
		m.input.Reset()
		// The box is borrowed for the answer, so ⏎ sends rather than queues: the legend must say so
		// for as long as the question stands (submitAnswer swaps it back when the answer is away).
		m.setPlaceholder(idlePlaceholder)
		m.sel = promptSel{} // the input was emptied for the answer; drop any stale selection
		m.layout()
		return m, m.input.Focus()

	case presentedMsg:
		// The worker's Presenter finished walking the ladder and hands the Update loop rung 0
		// itself: the transcript entry carrying the document's path (ADR 0019 §2). It asks for
		// nothing back — unlike the two rendezvous Msgs above, a presentation is a record, not a
		// gate — so it neither moves the state machine nor interrupts the running Turn.
		m.transcript.addPresented(msg)
		m.refreshViewport()
		return m, nil

	case exchangeDoneMsg:
		// The Exchange reached its quiescent boundary under its own power. That is the NATURAL
		// completion the interjection contract releases a queue on (ADR 0025): anything the human
		// typed while the model worked and the worker never got to deliver — rows staged while it
		// wrote its final answer, with no further boundary to land at — opens the next Exchange
		// from here, without a second keypress.
		cmd := m.finishWorker(stateIdle)
		return m.flushAfterCompletion(cmd)

	case cancelledMsg:
		// The worker cancelled at a quiescent boundary and has returned, so the engine is the
		// Update loop's to touch again (C1). Discard the interrupted Exchange so the engine
		// leaves its open-Exchange state: without this the Agent stays inExchange after a cancel
		// and the next /clear or message is rejected with ErrInputPending — the post-Esc wedge.
		// The visible transcript is untouched (the "cancelled" note and any streamed partial
		// stay in scrollback); only the model's memory drops the scrapped Exchange.
		//
		// A stop is NOT a completion, so a staged queue is held rather than flushed: Esc stops
		// everything, including what was waiting to go out (ADR 0025). The note says so once, and
		// ⏎ on the empty box is what sends it afterwards.
		m.eng.AbortExchange()
		m.transcript.addNote("cancelled")
		cmd := m.finishWorker(stateIdle)
		m.noteHeldQueue()
		m.refreshViewport()
		return m, cmd

	case errMsg:
		// A loop-level fault also returns the engine to the Update loop (C1), so — exactly as on
		// a cancel — discard the interrupted Exchange. Today Step never returns a mid-Exchange
		// error (a fault surfaces as an ErrorEvent at a boundary), so this is a no-op guard, not a
		// live path; but the moment Step *can* fault mid-Exchange, the engine would stay inExchange
		// and the next /clear or message would be rejected with ErrInputPending — the same post-Esc
		// wedge cancelledMsg already prevents. AbortExchange is a safe no-op at a quiescent boundary.
		//
		// A fault is not a completion either, so — exactly as on a cancel — a staged queue is held
		// and noted rather than flushed: sending the human's remarks into the wreckage of a failed
		// Exchange is not what they asked for (ADR 0025). Dismissing the error takes one ⏎ and
		// sending the held queue the next.
		m.eng.AbortExchange()
		m.lastErr = msg.Err
		m.transcript.addError("loop", msg.Err.Error(), 0)
		cmd := m.finishWorker(stateErrored)
		m.noteHeldQueue()
		m.refreshViewport()
		return m, cmd

	case compactDoneMsg:
		// The /compact worker returned (startCompact). On success the history shrank, so reset the
		// gauge to hidden — the next Turn's UsageEvent re-measures the smaller fill (foldStats). A
		// skip (conversation too small to fold) touched nothing, so leave the gauge as it was and
		// say so plainly rather than claiming a compaction. A failure surfaces its reason as a note.
		// Either way the worker is done: return to idle.
		//
		// A compaction that LANDED is a natural completion, so it flushes like an Exchange does: a
		// row typed while /compact ran had no Exchange to be interjected into (the /compact worker
		// drives none, so it carries no mailbox) and has been waiting for exactly this boundary.
		// Only a stop or a fault holds — a cancelled compaction returns cancelledMsg, not this Msg.
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

	case interjectedMsg:
		// The worker committed staged rows into the open Exchange at the boundary it just passed
		// (ADR 0025). Move exactly those rows out of the queue and into the transcript, where they
		// now belong: the model has read them, so this is the point in the scrollback at which they
		// happened. Rows the report does not name stay queued.
		m.foldInterjected(msg.items)
		return m, nil

	case turnSnapshotMsg:
		// The worker snapshotted the engine after a completed Turn (driveExchange). Persist it
		// asynchronously through the SessionHost; the worker keeps stepping, so this never stalls
		// the conversation, and the transcript is consistent with the snapshot (the Turn's Events
		// were delivered before this Msg — see turnSnapshotMsg's doc).
		return m, m.persist(msg.Sess)

	case saveDoneMsg:
		// An async save returned. Clear the single-flight flag, note the ok↔fail transition once,
		// and dispatch any save that coalesced while this one ran (saveComplete).
		return m, m.saveComplete(msg.Err)

	case sessionListMsg:
		// Sessions.List() returned off the Update loop: open (or refresh) the /sessions browser
		// over the metas, or note the empty/error case with no overlay (sessions.go).
		return m, m.foldSessionList(msg)

	case sessionLoadedMsg:
		// Sessions.Load(id) returned: restore the record into the live engine and repaint its
		// scrollback, or note the failure with the view left untouched (sessions.go).
		return m, m.resumeLoaded(msg)

	case spinnerTickMsg:
		// Keep the chain alive only while running, and only for the current generation: dropping
		// the tick when idle lets the chain die naturally, and dropping a tick from a previous arm
		// means a re-arm (an approval answered, an ask replied to) cannot leave two chains running
		// and double the frame rate (spinner.go).
		if m.state != stateRunning || msg.gen != m.spin.gen {
			return m, nil
		}
		m.spin.frame++
		return m, m.spin.tick()

	case beatMsg:
		// One observation of the Upstream landed (beatCmd). Fold what it says, then re-arm the
		// chain from HERE — Interval after the beat landed, never on a fixed clock — so the next
		// beat cannot overlap this one. A beat from a retired chain is inert, like a spinner tick.
		if !m.heartbeatLive(msg.gen) {
			return m, nil
		}
		next, noted := m.foldBeat(msg.beat)
		if noted {
			// Only a beat that MOVED something — the offline state, or a binding — repaints: a beat
			// that changed nothing has nothing to draw, so re-rendering the whole transcript every
			// ten seconds would be work for its own sake. (It no longer costs a live drag-selection:
			// a repaint that appends a note leaves the spanned lines alone, and refreshViewport's
			// keep-if-unchanged rule keeps the selection through it. Economy, not correctness.)
			next.refreshViewport()
		}
		return next, next.beatTick()

	case heartbeatTickMsg:
		// The interval since the last landed beat has elapsed: issue the next one. Same generation
		// guard as the spinner chain — a tick from a retired chain schedules nothing.
		if !m.heartbeatLive(msg.gen) {
			return m, nil
		}
		return m, m.beatCmd()

	case tea.MouseWheelMsg:
		// The wheel scrolls the transcript in every state — unlike the ordinary keyboard path,
		// which is state-gated (idle/ask/running feed the input; only PgUp/PgDn scroll from the
		// keyboard everywhere). Mouse reporting is enabled in View
		// (MouseModeCellMotion); the viewport's own Update turns the wheel into a scroll.
		return m.scrollViewport(msg)

	case tea.MouseClickMsg:
		// A left-click starts a selection — in the prompt (positioning the caret) or the
		// transcript, by which rectangle it lands in (mouse.go).
		return m.handleMouseClick(msg)

	case tea.MouseMotionMsg:
		// A drag (button held) extends whichever selection is live — prompt or transcript (mouse.go).
		return m.handleMouseMotion(msg)

	case tea.MouseReleaseMsg:
		// Releasing after a drag copies the live selection — prompt or transcript — to the
		// clipboard (mouse.go).
		return m.handleMouseRelease(msg)

	case flashClearMsg:
		// The transient mouse-copy note has lingered long enough; clear it (mouse.go).
		m.flash = ""
		return m, nil

	default:
		// Other Bubble Tea Msgs belong to the widgets; route them to the focused input so it
		// sees its own (a focus/blur report, say). The cursor is no longer among them: with the
		// virtual cursor retired no blink Msg is ever scheduled, and the real cursor is placed
		// by View rather than driven by a Msg chain.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// ctrlCQuitWindow is how long after one Ctrl+C a second press still quits. A lone Ctrl+C no
// longer ends the program; only a second press inside this window confirms the exit.
const ctrlCQuitWindow = time.Second

// ctrlCResetMsg disarms the Ctrl+C quit gesture once the window elapses, clearing the
// "press ctrl+c again to quit" hint when the human does not follow through.
type ctrlCResetMsg struct{}

// handleKey routes a keypress against the current state. Esc cancels an in-flight worker (and
// is otherwise a no-op); Ctrl+C twice within ctrlCQuitWindow quits. Enter submits at idle — the
// box plus any interjections a stop or an error left held, even on an empty box — answers a
// pending ask, dismisses an error, and while a worker runs STAGES what was typed as an
// interjection, launching nothing, so the single-worker invariant the seam relies on still
// holds. Other keys feed the input wherever the box is editable (idle, ask, running —
// inputEditable), and scroll the transcript at the inert states; PgUp/PgDn scroll in every state,
// which is what a running Exchange leaves the keyboard for reading with (ADR 0025).
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Any keypress drops a mouse drag-selection: typing past it, navigating, or submitting all
	// move on from what was selected, and clearing here (before the value can change) keeps the
	// selection's cached visual coordinates from ever going stale (mouse.go).
	m.sel = promptSel{}

	// The /sessions browser is a modal overlay (idle only): while open it claims every keypress —
	// selection, resume, delete-confirm, rename edit, and esc to close (sessions.go) — before the
	// normal input routing below, exactly as the autocomplete overlay claims its keys first.
	if m.state == stateIdle && m.sessionBrowser.open {
		return m.sessionBrowserKey(msg)
	}

	// While the autocomplete overlay is open, it claims the navigation, accept, and dismiss keys —
	// including enter and tab — before the normal routing below. Any other key returns
	// handled=false and falls through to edit the input (which re-derives it). It opens at idle,
	// and while running for the "@file" region alone (computeAutocomplete), so an interjection can
	// reference a file as easily as a submitted message can.
	if (m.state == stateIdle || m.state == stateRunning) && m.autocomplete.active {
		if handled, nm, cmd := m.autocompleteKey(msg); handled {
			return nm, cmd
		}
	}

	switch msg.String() {
	case "ctrl+c":
		// A lone Ctrl+C no longer quits — a stray hit must never drop the human out of a
		// session. The first press arms the gesture (and shows the hint via statusRight); only
		// a second press inside ctrlCQuitWindow confirms the quit. The tick disarms it if no
		// second press follows (ctrlCResetMsg).
		now := time.Now()
		if !m.lastCtrlC.IsZero() && now.Sub(m.lastCtrlC) <= ctrlCQuitWindow {
			return m.quit()
		}
		m.lastCtrlC = now
		return m, tea.Tick(ctrlCQuitWindow, func(time.Time) tea.Msg { return ctrlCResetMsg{} })
	case "esc":
		// Esc never ends the program; it only cancels an in-flight worker. At idle/errored it
		// is a no-op (use Ctrl+C twice to exit), so a reflexive Esc never quits.
		if m.busy() {
			m.stopWorker()
		}
		return m, nil
	case "enter":
		switch m.state {
		case stateIdle:
			return m.submit()
		case stateRunning:
			// The single-worker invariant stands — this launches nothing. What the human typed is
			// STAGED as an interjection and delivered into the running Exchange at the next
			// between-Steps boundary (ADR 0025).
			return m.stageInterjection()
		case stateAwaitingAsk:
			// Submit the typed answer back to the blocked ask_user tool (P3.11).
			return m.submitAnswer()
		case stateErrored:
			// Dismiss the error and return to idle so the next message can be sent. With a queue
			// held by the fault (ADR 0025) that is deliberately TWO presses: this one clears the
			// error, the next sends what was held. An error the human has not finished reading
			// must not be answered by firing off their queued remarks.
			m.lastErr = nil
			m.state = stateIdle
			return m, nil
		default:
			return m, nil // no-op while awaiting approval (a/d/s own the keyboard)
		}
	case "shift+tab":
		// Cycle the autonomy mode one rung up the privilege ladder (wraps Auto → Plan). Live in
		// every state (idle, running, awaiting approval/answer): SetMode is goroutine-safe, so the
		// change can race-free overlap a running Step and takes effect on the next tool call.
		next := domain.NextMode(m.opts.Mode)
		m.eng.SetMode(next)
		m.opts.Mode = next // the footer renders the mode from opts.Mode (footerContent)
		m.layout()
		return m, nil
	}

	// PageUp/PageDown scroll the transcript in every state. They have no meaning in the input
	// box, so intercept them before the state-gated routing below — the wheel does the same via
	// MouseWheelMsg (Update); the other scroll keys stay state-gated so typing is unaffected.
	switch msg.String() {
	case "pgup", "pgdown":
		return m.scrollViewport(msg)
	}

	// A live approval prompt claims the decision keys. This branch must precede the scroll
	// fall-through below, which would otherwise swallow a/d/s as viewport scroll keys.
	if m.state == stateAwaitingApproval {
		return m.handleApprovalKey(msg)
	}

	// While awaiting an ask_user answer that offers choices, ↑/↓ move the choice highlight — but
	// ONLY while the input box is empty (D5). The moment the human types, this guard fails and the
	// arrows fall through to the textarea below (cursor duty), so multi-line free-text editing is
	// never stolen; deleting back to empty restores the highlight. Non-wrapping, clamped to the
	// choice range.
	if m.state == stateAwaitingAsk && m.pendingAsk != nil &&
		len(m.pendingAsk.Request.Choices) > 0 && m.input.Value() == "" {
		switch msg.String() {
		case "up":
			if m.askSel > 0 {
				m.askSel--
			}
			return m, nil
		case "down":
			if m.askSel < len(m.pendingAsk.Request.Choices)-1 {
				m.askSel++
			}
			return m, nil
		}
	}

	// Wherever the box is editable the keys type into it: idle (a message to send), awaiting an
	// ask_user answer (the borrowed answer box), and — the deliberate behavioural change this
	// feature ships — while a worker runs, where ⏎ stages the message as an interjection
	// (inputEditable; ADR 0025). Enter is handled above; everything else is an edit.
	if m.inputEditable() {
		if m.input.Value() == "" && msg.String() == "backspace" {
			// Backspace on an empty input un-does the last thing staged: it lifts the newest queued
			// interjection back into the box. Nothing else is staged beside the text any more — a
			// skill is a /token IN it, deleted like any other word.
			if popped, ok := m.popInterjection(); ok {
				return popped, nil
			}
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.state == stateIdle || m.state == stateRunning {
			m = m.recomputeAutocomplete() // re-derive the overlay from the edited input (reloads on /skill open)
		}
		m.layout() // re-flow: the input box auto-grows as the message wraps to more rows
		return m, cmd
	}
	// At the inert states — errored (⏎ dismisses) and a live approval that fell past its decision
	// keys — the keys scroll the transcript instead, and where the scroll leaves the view sets the
	// follow policy (scrollViewport): off the bottom holds the position, back at the bottom follows
	// the tail again. While running the transcript scrolls by PgUp/PgDn (intercepted above) and the
	// mouse wheel, the keyboard being the input's now.
	return m.scrollViewport(msg)
}

// scrollViewport routes a key or wheel event to the viewport and re-derives attachment from
// where that scroll left the view: the scroll position IS the policy. At the very bottom means
// follow — refreshViewport keeps the view on generated output as it streams; anywhere else means
// hold exactly there, so reading history is not yanked back by the streaming reply. Detach is
// therefore not a latch: scrolling up detaches, scrolling back down to the bottom resumes
// following, and on a transcript that fits the window there is nothing to detach from (AtBottom
// is always true there). This is the ONLY site that sets detached — the funnel every user scroll
// goes through — which is what keeps a programmatic reposition inside refreshViewport from ever
// reading as a human scroll.
func (m Model) scrollViewport(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	m.detached = !m.viewport.AtBottom()
	return m, cmd
}

// approvalKeys maps a decision keypress to the ApprovalDecision it sends. The set mirrors the
// awaitingApproval hint legend (a allow · d deny · s allow-session).
var approvalKeys = map[string]domain.ApprovalDecision{
	"a": domain.ApprovalAllow,
	"d": domain.ApprovalDeny,
	"s": domain.ApprovalAllowForSession,
}

// handleApprovalKey resolves a pending Approval while awaitingApproval. A decision key sends
// its verdict back over the rendezvous reply channel (buffered cap 1, so the send never
// blocks — messages.go) and returns the model to running so the worker's blocked Step
// resumes; the spinner tick is re-armed because the chain died when the prompt went up. Any
// other key scrolls the transcript so the human can review context before ruling. The
// decision's transcript record arrives for free as the loop's observational ApprovalEvent
// (C3; P2.3), so this renders the prompt's resolution, not the record.
func (m Model) handleApprovalKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if decision, ok := approvalKeys[msg.String()]; ok && m.pending != nil {
		m.pending.Reply <- decision
		m.pending = nil
		m.state = stateRunning
		tick := m.spin.arm()
		return m, tick
	}
	return m.scrollViewport(msg)
}

// submit parses the input through the chat mini-language and routes it: a recognised
// /command goes to runCommand; anything else is a message sent to the agent (with its @file
// references extracted for the loop to resolve). It records the user message, switches to
// running, stores the worker's CancelFunc (C4), and batches the worker Cmd with the spinner
// tick. Only reachable from stateIdle, so the single-worker invariant holds.
//
// A queue held over from a stop or an error is itself something to send (ADR 0025), so ⏎ here is
// never merely "send the box": the held rows go out with it as ONE message — them first, oldest
// first, the box's own text last, because it is the newest thing the human wrote. That is also why
// an EMPTY box is not always a no-op: with rows held, ⏎ is what sends them.
func (m Model) submit() (tea.Model, tea.Cmd) {
	parsed := m.promptEditor.submitParse(m.knownSkillID)
	if parsed.kind == kindCommand {
		return m.runCommand(parsed)
	}
	held := len(m.pendingInterjections) > 0
	// Nothing to send only when there is neither text NOR a held row. A message that is only a skill
	// token ("/grill-me") HAS text — the token itself — so it sends, which is the owner's edge
	// default: "just run the skill".
	if parsed.text == "" && !held {
		return m, nil
	}
	if m.blockedUpstream() {
		// There is nothing to send to. Refuse at the boundary and say why — and leave the typed
		// message exactly where it is: the human wrote it, the server's absence is not their
		// mistake, and re-typing it once the server comes back would be the actual insult. Nothing
		// else about the state moves (no worker, no reset, and a held interjection queue stays
		// held), so ⏎ once the heartbeat recovers sends the very same message.
		m.transcript.addNote(m.upstreamBlockNote())
		m.refreshViewport()
		return m, nil
	}
	if m.eng.InExchange() {
		// The session was restored mid-task and the human typed a fresh message instead of
		// /continue: their new intent supersedes the stale half-Exchange (Esc-parity). Scrap the
		// open Exchange so the Submit below is accepted — a Submit while InExchange is rejected with
		// ErrInputPending — and note the discard so the dropped work is never a silent loss.
		m.eng.AbortExchange()
		m.transcript.addNote("discarded the interrupted work — continuing fresh from your message")
	}
	in := domain.UserInput{Text: parsed.text, FileRefs: parsed.fileRefs, SkillIDs: parsed.skillIDs}
	if held {
		// The held rows and the box become one unmarked user message (joinedInterjections), which
		// is what keeps the derived Exchange opening trivially correct: exactly one of them opens
		// the Exchange, whether the human sent one message or five.
		in = m.joinedInterjections(parsed)
		m.pendingInterjections = nil
	}
	m.promptEditor.reset() // empties the textarea and closes the overlay
	m.detached = false     // a fresh prompt re-arms follow-the-tail: sending means "done reading history"
	m.transcript.addUser(in.Text, m.skillDisplayNames(in.SkillIDs))
	m.layout() // the emptied input box shrinks back; the new prompt pins to the top
	return m.launchExchange(in)
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
	cmd, cancel := startExchange(m.parent, m.eng, in, m.box, m.notify)
	m.cancel = cancel
	m.state = stateRunning
	m.setPlaceholder(runningPlaceholder) // the empty box now invites a queued message, not a send
	m.setActivity(actThinking, "", 0)
	tick := m.spin.arm()
	return m, tea.Batch(cmd, tick)
}

// skillDisplayNames resolves the invoked skill IDs to their display names (falling back to the
// raw ID when the catalog can't resolve it), for the chips rendered on the sent user block. A
// nil/empty input yields nil, so the block carries no chip row.
func (m Model) skillDisplayNames(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		name := id
		if m.opts.Skills != nil {
			if sk, ok := m.opts.Skills.Get(id); ok {
				name = sk.DisplayName
			}
		}
		// The display name is untrusted (repo-supplied SKILL.md front-matter), so escape-strip it
		// here too (the transcript boundary strips again — cheap defense in depth).
		names = append(names, stripEscapes(name))
	}
	return names
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
// Ordering: the save runs BEFORE ClearContext so the snapshot reflects the conversation being closed,
// not an emptied one. An interrupted session (InExchange) is then aborted between the save and the
// clear — ClearContext refuses mid-Exchange with ErrInputPending, so the save keeps its mid-task
// state in history and the abort lets the clear accept the boundary. Rotate runs only AFTER
// ClearContext succeeds — a refused clear leaves the old session open and its id live, so no rotate
// happens on the error path. On success Rotate is unconditional and idempotent on an already-inactive
// session, so a stale active id can never leak into the fresh conversation even when the outgoing view
// held nothing worth saving.
//
// Reached only from runCommand at stateIdle (no worker owns the engine), so ClearContext and the
// Snapshot the flush takes are safe. On a ClearContext error the view is left untouched and the failure
// is noted — a fresh-looking view must never lie about an engine that still remembers the old
// conversation; the already-completed save is harmless (the session was closing anyway).
func (m Model) startNewSession() (tea.Model, tea.Cmd) {
	m.saveSession() // flush the outgoing session into history before it closes (best-effort, gated)
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
		return m, nil
	}
	if m.sessions != nil {
		m.sessions.Rotate() // the next Turn's save mints a fresh id; a no-op when nothing was saved
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
	m.layout()
	return m, nil
}

// runCommand handles a recognised local /command from the idle state. /continue and /compact
// open a worker: /continue a canned "Please continue" turn, /compact a generative summary
// call; /clear (and its alias /new) resets the session view and reprints the start-up box
// synchronously and stays idle (startNewSession), /version records the build version as a note the same
// synchronous way, /skills records the discovered skill catalog the same synchronous way
// (skills.go), and /confine reports or swaps Auto's blast radius the same
// synchronous way (confine.go). The input box and the autocomplete overlay are cleared either way. Reached
// only from submit (stateIdle), so the engine is quiescent — no worker owns it — and
// ClearContext/Compact are safe to launch here.
//
// It takes the whole parsedInput, not just the verb, because a verb with arguments can fail to
// parse: parsed.err is reported as a note (it carries its own usage line) and nothing is driven,
// so a mistyped /confine can never be mistaken for one that took effect.
func (m Model) runCommand(parsed parsedInput) (tea.Model, tea.Cmd) {
	m.input.Reset()
	m.autocomplete = autocompleteState{}

	if parsed.err != nil {
		m.transcript.addNote(parsed.err.Error())
		m.layout()
		return m, nil
	}

	// /continue and /compact are the two commands that open an Exchange, so they answer to the
	// heartbeat exactly as a typed message does (blockedUpstream). The purely local verbs below —
	// /clear, /sessions, /version, /confine — stay live while the server is away.
	if m.blockedUpstream() && (parsed.command == "continue" || parsed.command == "compact") {
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
			cmd, cancel := startResume(m.parent, m.eng, m.box, m.notify)
			m.cancel = cancel
			m.state = stateRunning
			m.setPlaceholder(runningPlaceholder)
			m.setActivity(actThinking, "", 0) // the resumed work is a request in flight (as in submit)
			tick := m.spin.arm()
			return m, tea.Batch(cmd, tick)
		}
		// The canned turn carries no skills: a skill is invoked by naming its /token in a real
		// message, and "/continue" is the whole input here by construction (the whole-input command
		// rule), so there is no token to carry.
		m.detached = false // the canned turn re-arms follow-the-tail, exactly as a typed prompt does
		m.transcript.addUser("/continue", nil)
		m.layout()
		m.box = newInterjectBox()
		cmd, cancel := startExchange(m.parent, m.eng,
			domain.UserInput{Text: "Please continue"}, m.box, m.notify)
		m.cancel = cancel
		m.state = stateRunning
		m.setPlaceholder(runningPlaceholder)
		m.setActivity(actThinking, "", 0) // a canned turn is still a request in flight (as in submit)
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

	case "version":
		// Synchronous like /clear: print the resolved build version (Options.Version, item 1's
		// seam) as a transcript note and stay idle — no upstream call, no worker.
		m.transcript.addNote("apogee " + m.opts.Version)
		m.layout()
		return m, nil

	case "skills":
		// Synchronous like /version: re-scan the source dirs and print the catalog as a note
		// (skills.go). No upstream call, no worker — it only reports what discovery found.
		return m.runSkills()

	case "compact":
		// Compaction is a real upstream call (summary generation), so it rides a worker goroutine
		// like /continue rather than blocking the Update loop (ADR 0011). Esc cancels it via
		// stopWorker; the terminal compactDoneMsg records the outcome.
		m.layout() // reflow the emptied input box back to one row
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
		m.setActivity(actCompacting, "", 0)
		tick := m.spin.arm()
		return m, tea.Batch(cmd, tick)

	case "confine":
		// Report or swap the blast radius. Synchronous and idle-safe like /clear: no upstream
		// call is involved, only the engine's live flag and (for --save) one config write.
		return m.runConfine(parsed.confine)
	}
	return m, nil
}

// submitAnswer sends the typed answer back to the blocked ask_user tool over the rendezvous
// reply channel (buffered cap 1, so the send never blocks — messages.go) and returns to
// running so the worker's blocked Step resumes; the spinner tick is re-armed because the
// chain died when the question went up. The input box is emptied (it was borrowed for the
// answer). An empty answer is allowed — the human may legitimately reply with nothing — so
// the answer round-trips whatever was typed. Only reachable from stateAwaitingAsk.
func (m Model) submitAnswer() (tea.Model, tea.Cmd) {
	if m.pendingAsk == nil {
		return m, nil
	}
	// With choices offered and an empty input the answer is the highlighted choice — the SAME
	// escape-stripped label the popup showed, not the raw domain string (D5/D9). Otherwise it is
	// the trimmed typed text (an empty free-text answer stays allowed when no choices are offered).
	choices := m.pendingAsk.Request.Choices
	var answer string
	if len(choices) > 0 && m.input.Value() == "" {
		sel := min(max(m.askSel, 0), len(choices)-1) // defensive clamp; routing keeps it in range
		answer = stripEscapes(choices[sel])
	} else {
		answer = strings.TrimSpace(m.input.Value())
	}
	m.pendingAsk.Reply <- domain.AskAnswer{Text: answer}
	m.pendingAsk = nil
	m.input.Reset()
	m.state = stateRunning
	m.setPlaceholder(runningPlaceholder) // the box is the human's own again — ⏎ queues from here
	m.layout()
	tick := m.spin.arm()
	return m, tick
}

// stopWorker cancels the in-flight worker. The worker honours the cancel at the next
// quiescent boundary and returns a cancelledMsg, which clears the state; until then the
// model stays running. A cancelled approval gate unblocks the same way (C3/C4).
//
// It switches the status line to "stopping" for that window: the cancel is not instant, the
// worker keeps emitting events until it unwinds, and the human needs to see their stop was
// registered. The phrase is sticky (foldActivity refuses to overwrite it) until finishWorker
// clears it.
func (m *Model) stopWorker() {
	if m.cancel != nil {
		m.cancel()
	}
	m.setActivity(actStopping, "", 0)
}

// finishWorker returns the model to a terminal state once the worker's terminal Msg
// arrives: it cancels and clears the CancelFunc, any pending Approval or ask_user
// question, and the Exchange's interjection mailbox. The new state is idle for a completed
// or cancelled Exchange, errored for a loop fault. The returned Cmd is tea.Quit when a busy quit was deferred (see quit); otherwise, when
// the Exchange settled at idle, it is the final per-session save (saveAtIdle) — the Model owns
// the engine again at this boundary, so it takes its own Snapshot — else nil.
//
// It is also where a binding change the heartbeat observed mid-Exchange is applied
// (applyPendingRebind): the Model owning the engine again is exactly the precondition
// Agent.Rebind states, so the deferred apply and the idle Snapshot share one boundary.
//
// It CALLS the CancelFunc before clearing it: a completed Exchange leaves its worker's
// cancellable child context un-cancelled otherwise, leaking one context (and its goroutine's
// timer resources) per completed exchange for the life of the session. Cancelling a context
// whose work already finished is the documented, idempotent way to release it.
//
// It also clears the generation clock: a cancelled or faulted stream emits no terminal
// UsageEvent, so foldStats never zeroes genStart, and a stale start would time the *next*
// turn's tok/s from the dead one. A normal completion has already zeroed it in foldStats, so
// this is a harmless no-op there and the safety net for every abnormal terminal path.
func (m *Model) finishWorker(next uiState) tea.Cmd {
	if m.cancel != nil {
		m.cancel() // release the worker's child context — un-cancelled it leaks per exchange
	}
	m.cancel = nil
	m.pending = nil
	m.pendingAsk = nil
	// The Exchange is over, so its mailbox has no reader left: drop it, and with it any row the
	// worker had not drained by the time it unwound. Nothing is lost — the display queue
	// (pendingInterjections) is the queue of record and still holds every undelivered row. What
	// happens to those rows next is the CALLER's ruling, not this one's: a natural completion
	// flushes them into a new Exchange (flushAfterCompletion), a stop or a fault holds them for
	// the next ⏎ (noteHeldQueue) — ADR 0025.
	m.box = nil
	m.setPlaceholder(idlePlaceholder) // nothing is running: ⏎ sends again
	m.genStart = time.Time{}
	// The worker has unwound, so the activity is over — including a sticky "stopping", which
	// only this path clears (activity.go). Idle renders an empty left slot.
	m.act = activity{}
	m.state = next
	if m.quitting {
		// A quit was requested while busy (quit deferred the exit to here); the worker has now
		// returned its terminal Msg, so its goroutine has unwound and the teardown cannot race it.
		// A binding change captured during that Exchange is deliberately dropped: the session is
		// ending, and rebinding an engine nobody will send to would only delay the exit.
		return tea.Quit
	}
	// The engine is the Update loop's again, which is the boundary a binding change captured
	// mid-Exchange has been waiting for (ADR 0024). It runs BEFORE the idle save so the session
	// record is stamped with the model that is now bound, notes and all.
	m.applyPendingRebind()
	if next == stateIdle {
		// A completed or cancelled Exchange settled at idle: persist the final conversation state
		// (the per-Turn saves captured each Turn; this catches the closing boundary, including a
		// cancel's post-AbortExchange rollback and any note the terminal handler just added).
		return m.saveAtIdle()
	}
	return nil
}

// quit ends the program. On a clean quit — idle or errored, where the worker has already
// returned — it first snapshots the conversation to the host saver; the Agent is
// single-goroutine, so reading it from the Update goroutine is only safe once no worker
// owns it (the busy() states). While busy it cancels the in-flight worker and DEFERS the
// exit: returning tea.Quit here would let runRoot's deferred mcpClient.Close()/agent.Close()
// run while the worker is still inside Step (benign while Close is a no-op, a use-after-close
// the moment it gains its teardown — cmd/apogee/wire.go). Arming quitting instead makes the
// worker's single terminal Msg fire tea.Quit via finishWorker, once the goroutine has
// unwound. Snapshotting the last boundary mid-run stays deferred (plan §6.1; handoff 16).
//
// Staged interjections do not hold the exit up and are not sent on the way out: they are
// session-ephemeral by decision (ADR 0025 — a session records what was committed), so a quit
// requested with rows queued simply exits, and the deferred exit beats the terminal fold's flush
// (flushAfterCompletion).
func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.busy() {
		m.stopWorker()
		m.quitting = true
		return m, nil
	}
	m.saveSession()
	return m, tea.Quit
}

// saveSession best-effort persists the conversation through the SessionHost — the synchronous flush
// that captures any post-last-turn transcript changes (notes, /confine output) the per-Turn saves did
// not. Two callers use it: a clean quit, and startNewSession closing the outgoing session on /clear|
// /new (which then Rotates so the next Turn opens a fresh id). It is a no-op without a wired host or
// when the transcript holds no conversation (only the seeded start-up box, or nothing at all) — nothing
// worth resuming. Both Snapshot and the Save are best-effort: a quit must never fail and a session
// close must never block, so an error is swallowed rather than interrupting. Both callers guarantee no
// worker is running, so calling Snapshot here respects the Agent's single-goroutine contract (C1).
// Unlike the per-Turn path this is synchronous — the outgoing session must reach disk before the caller
// Rotates or the program exits, and there is no Update loop left (or wanted) to deliver a saveDoneMsg
// to.
func (m Model) saveSession() {
	if m.sessions == nil || !m.transcript.hasConversation() {
		return
	}
	sess, err := m.eng.Snapshot()
	if err != nil {
		return
	}
	p, ok := m.snapshotPayload(sess)
	if !ok {
		return
	}
	_ = m.sessions.Save(p.sess, p.transcript, p.title, p.userMsgs, p.ctxUsed)
}

// ----------------------------------------------------------------------------
// Per-Turn session save pipeline (session-system plan §4)
// ----------------------------------------------------------------------------

// savePayload is one assembled save: the engine snapshot plus the derived metadata and the
// encoded transcript blob the SessionHost persists. It is built at a boundary the Model owns
// (a per-Turn snapshot or an idle boundary) and then either dispatched immediately or coalesced
// (pendingSave) behind an in-flight save.
type savePayload struct {
	sess       domain.Session
	transcript []byte
	title      string
	userMsgs   int
	ctxUsed    int
}

// snapshotPayload assembles a savePayload around a captured engine snapshot: it encodes the
// current transcript (transcriptcodec.go), derives the browsable title from the first user
// message, counts the user messages, and reads the live context fill. A transcript that fails to
// encode yields ok=false so the caller drops the save rather than persisting a half-record.
func (m Model) snapshotPayload(sess domain.Session) (savePayload, bool) {
	blob, err := encodeTranscript(&m.transcript)
	if err != nil {
		return savePayload{}, false
	}
	return savePayload{
		sess:       sess,
		transcript: blob,
		title:      sessionTitle(m.transcript.firstUserText()),
		userMsgs:   m.transcript.userMessageCount(),
		ctxUsed:    m.ctxUsed,
	}, true
}

// persist builds a savePayload around sess and schedules it, gated on there being a wired host
// and a conversation worth resuming. It is the entry the per-Turn snapshot (turnSnapshotMsg) and
// the idle finishers both funnel through, so the "worth saving?" gate lives in one place. It
// returns the Cmd to run (nil when nothing was scheduled).
func (m *Model) persist(sess domain.Session) tea.Cmd {
	if m.sessions == nil || !m.transcript.hasConversation() {
		return nil
	}
	p, ok := m.snapshotPayload(sess)
	if !ok {
		return nil
	}
	return m.scheduleSave(p)
}

// saveAtIdle persists the current conversation taking the Model's OWN engine Snapshot — valid
// because every caller is a terminal boundary at which the worker has returned and the Update
// loop owns the engine again (C1). Best-effort like persist: a Snapshot error, an unwired host,
// or an empty transcript simply schedules nothing.
func (m *Model) saveAtIdle() tea.Cmd {
	if m.sessions == nil || !m.transcript.hasConversation() {
		return nil
	}
	sess, err := m.eng.Snapshot()
	if err != nil {
		return nil
	}
	return m.persist(sess)
}

// scheduleSave dispatches p through the SessionHost, coalescing when a save is already in flight:
// the newest payload replaces any waiting one (latest-wins — an older intermediate Turn is
// superseded by the newer snapshot) and is dispatched when the in-flight save's saveDoneMsg
// lands. The persistence I/O runs off the Update loop on the returned Cmd's goroutine, so a
// per-Turn save never stalls the conversation. Returns nil when the save was coalesced.
func (m *Model) scheduleSave(p savePayload) tea.Cmd {
	if m.saveBusy {
		m.pendingSave = &p
		return nil
	}
	m.saveBusy = true
	return m.saveCmd(p)
}

// saveCmd builds the Cmd that persists one payload and reports back as saveDoneMsg. It captures
// the SessionHost and the payload by value, so the closure holds no pointer into the value-copied
// Model.
func (m Model) saveCmd(p savePayload) tea.Cmd {
	sessions := m.sessions
	return func() tea.Msg {
		return saveDoneMsg{Err: sessions.Save(p.sess, p.transcript, p.title, p.userMsgs, p.ctxUsed)}
	}
}

// saveComplete folds a finished save: it clears the in-flight flag, notes the ok↔fail transition
// exactly once (on the ok→fail edge and the fail→ok recovery edge, then swallows the error — a
// save failure must never interrupt the conversation), and dispatches any payload that coalesced
// while the save ran (latest-wins single-flight).
func (m *Model) saveComplete(err error) tea.Cmd {
	m.saveBusy = false
	switch {
	case err != nil && !m.saveFailing:
		m.saveFailing = true
		m.transcript.addNote("session save failed: " + err.Error() + " — will keep retrying")
		m.refreshViewport()
	case err == nil && m.saveFailing:
		m.saveFailing = false
		m.transcript.addNote("session saving recovered")
		m.refreshViewport()
	}
	if m.pendingSave != nil {
		p := *m.pendingSave
		m.pendingSave = nil
		return m.scheduleSave(p)
	}
	return nil
}

// sessionTitleMax is the longest a derived session title runs before word-boundary truncation
// (apogee-code's MAX_TITLE_LENGTH).
const sessionTitleMax = 50

// sessionTitle derives a browsable one-line title from the first user message's text, ported from
// apogee-code's generateTitle: the first line, returned as-is when it fits, otherwise truncated to
// sessionTitleMax runes at the last word boundary past 60% (falling back to a hard cut) and closed
// with an ellipsis. A message that is empty or opens a code fence has no useful title, so it falls
// back to a dated "Session <date>" — every stored session still gets a human label.
func sessionTitle(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || strings.HasPrefix(trimmed, "```") {
		return "Session " + time.Now().Format("2006-01-02")
	}
	firstLine := trimmed
	if i := strings.IndexByte(trimmed, '\n'); i >= 0 {
		firstLine = trimmed[:i]
	}
	runes := []rune(firstLine)
	if len(runes) <= sessionTitleMax {
		return firstLine
	}
	truncated := string(runes[:sessionTitleMax])
	if lastSpace := strings.LastIndex(truncated, " "); lastSpace > sessionTitleMax*6/10 {
		truncated = truncated[:lastSpace]
	}
	return truncated + "…"
}

// busy reports whether a worker is in flight (running, blocked on an Approval, or blocked on
// an ask_user question) — the states in which a free submit is refused and the stop key
// cancels instead of quitting.
func (m Model) busy() bool {
	return m.state == stateRunning ||
		m.state == stateAwaitingApproval ||
		m.state == stateAwaitingAsk
}

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
	// lastFailure is the most recent failure's words, quoted in the offline note and in the
	// refusal of a send. "" once a beat lands.
	lastFailure string
	// models is what the server last advertised — the data layer a future model picker reads.
	// Nothing renders it today; it is kept current so the picker needs no new plumbing.
	models []heartbeat.ModelSummary
	// observedModel and observedWindow are what the last beat that mattered reported. They are the
	// baseline a model/window change is measured against; the rebind orchestration owns them.
	observedModel  string
	observedWindow int
	// pendingRebind is a captured change waiting for the engine to be quiescent — set when a beat
	// lands while a worker owns the engine, applied in finishWorker. Latest-wins: a second change
	// during the same Exchange replaces the first, so only the newest reality is ever bound. nil ⇒
	// nothing is deferred. A pointer into a value-copied Model is safe because it is only ever
	// replaced, never written through (the pendingSave posture, ADR 0011).
	pendingRebind *rebindIntent
	// lastRebindFailed is the model id whose rebind last failed, so a refusal is noted once per
	// distinct target instead of once every Interval. "" once a rebind succeeds.
	lastRebindFailed string
}

// rebindIntent is one captured binding change: the model the last beat observed the server to be
// serving and the window it reported for it. It is what the deferred apply stashes and what the
// [Options.Rebind] seam is called with — plain values, so the value-copied Model carries it safely.
// It is the OBSERVATION, not the binding: what the binary makes of it (a pinned window outranking
// the observed one) comes back in the [RebindResult].
type rebindIntent struct {
	model  string
	window int
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

// heartbeatLive reports whether gen belongs to the current tick chain of a WIRED monitor — the
// guard both heartbeat Msgs pass through. An unwired Model (gen 0) folds nothing, so a stray beat
// can never flip a TUI that monitors nothing into an offline state it can never leave.
func (m Model) heartbeatLive(gen int) bool {
	return m.opts.Heartbeat != nil && gen == m.hb.gen
}

// beatCmd runs one observation off the Update loop and reports it as a beatMsg stamped with the
// current generation. It captures the program context, so a shutdown cancels a beat still in
// flight, and the seam func by value — no pointer into the value-copied Model (the saveCmd
// posture). It returns nil when the monitor is unwired, which is what makes Init's tea.Batch
// collapse to the focus Cmd alone.
func (m Model) beatCmd() tea.Cmd {
	observe := m.opts.Heartbeat
	if observe == nil {
		return nil
	}
	ctx, gen := m.parent, m.hb.gen
	return func() tea.Msg {
		return beatMsg{gen: gen, beat: observe(ctx)}
	}
}

// beatTick schedules the next beat one heartbeat.Interval after the beat that just landed. Timing
// the wait from the landing (rather than from a fixed clock) is what makes overlap impossible: the
// observation and its wait are strictly sequential, whatever the server's latency.
func (m Model) beatTick() tea.Cmd {
	gen := m.hb.gen
	return tea.Tick(heartbeat.Interval, func(time.Time) tea.Msg { return heartbeatTickMsg{gen: gen} })
}

// foldBeat folds one landed observation into the heartbeat state and reports whether it changed
// what the view shows (so the caller repaints only when there is something new to see). Two things
// can move: the offline state, and — through [Model.observeBinding] — the bindings themselves.
//
// A beat that crosses back online AND rebinds says so once, not twice: "connected: <model>" is the
// stronger statement, and it already implies the server answered. The recovery note is for the
// ordinary case, where the server came back serving exactly what it served before and there is
// nothing else to report.
func (m Model) foldBeat(beat heartbeat.Beat) (Model, bool) {
	if !beat.Reachable {
		return m.foldBeatFailure(beat.Failure)
	}
	m.hb.failures = 0
	m.hb.everOnline = true
	m.hb.lastFailure = ""
	m.hb.models = beat.AvailableModels // the future picker's data layer (decision 4); nothing renders it
	crossed := m.hb.offline
	m.hb.offline = false

	m, rebound := m.observeBinding(beat)
	if crossed && !rebound {
		m.transcript.addNote(onlineNote)
	}
	return m, crossed || rebound
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
func (m Model) observeBinding(beat heartbeat.Beat) (Model, bool) {
	if m.opts.Rebind == nil {
		return m, false // a display-frozen heartbeat: nothing to apply a change through
	}
	changed := beat.ActiveModel != m.hb.observedModel ||
		(beat.ContextWindow > 0 && beat.ContextWindow != m.hb.observedWindow)
	if !changed {
		return m, false
	}
	m.hb.observedModel, m.hb.observedWindow = beat.ActiveModel, beat.ContextWindow
	intent := rebindIntent{model: beat.ActiveModel, window: beat.ContextWindow}
	if m.busy() {
		// A worker owns the engine, and Agent.Rebind is idle-only by construction. Stash the intent
		// for finishWorker rather than refuse it: the human is mid-answer, and the switch they made
		// upstream should land the moment the Exchange closes (latest-wins, so a second switch during
		// the same Exchange simply supersedes this one).
		m.hb.pendingRebind = &intent
		return m, false
	}
	return m.applyRebind(intent)
}

// applyRebind re-resolves the bindings for one captured change through the [Options.Rebind] seam and
// folds the answer into the display: the model and window the binary actually BOUND, the start-up
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
	rebind := m.opts.Rebind
	if rebind == nil {
		return m, false
	}
	oldModel, oldWindow := m.opts.Model, m.opts.ContextWindow

	result, err := rebind(intent.model, intent.window)
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
	if note := rebindNote(oldModel, oldWindow, m.opts.Model, m.opts.ContextWindow); note != "" {
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

// applyPendingRebind binds a change that was captured while a worker owned the engine. The terminal
// fold IS the boundary Agent.Rebind demands — the same one AbortExchange and the idle save already
// use — and the worker's terminal Msg travelling through the Bubble Tea channel is what establishes
// the happens-before in both directions, which is why the engine's per-model bindings need no lock
// (ADR 0024). A no-op when nothing was deferred.
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
// words), a model change, and a window-only change. It returns "" when nothing VISIBLE moved, which
// is the pinned-window case: the server switched to a differently-sized window, the pin outranked
// it, and the binding the human can see is unchanged — a note there would describe a change that
// did not happen. The window clause is carried only when the BOUND window moved, so a pin never
// narrates the change it suppressed.
//
// Both ids are rendered the way the footer and the start-up box render them (displayModel), so the
// note and the chrome beside it can never name the same model two different ways.
func rebindNote(oldModel string, oldWindow int, newModel string, newWindow int) string {
	switch {
	case oldModel == "":
		note := "connected: " + displayModel(newModel)
		if newWindow > 0 {
			note += ", context " + formatTokens(newWindow)
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
// than as the empty string formatTokens yields — "context  → 16k" reads as a rendering bug, which is
// the very thing the gauge's clamp was fixed for.
func windowWord(n int) string {
	if n <= 0 {
		return "unknown"
	}
	return formatTokens(n)
}

// foldBeatFailure folds a beat that could not read the server. Three rules, in order:
//
//   - While an Exchange is in flight the failure is IGNORED — counter, state and words untouched.
//     A streaming reply is stronger evidence that the server is there than a timed-out /v1/models
//     on a single-slot server busy serving that very stream.
//   - Before any beat has ever landed, one failure is enough: a cold start against a server that
//     is not running should say so at once rather than after a debounce it has no evidence for.
//   - Otherwise the crossing waits for offlineFailureThreshold consecutive idle failures.
//
// The crossing is noted exactly once; every further failed beat is silent until a success crosses
// back (foldBeat).
func (m Model) foldBeatFailure(failure string) (Model, bool) {
	if m.busy() {
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
	return m.opts.Heartbeat != nil && (m.hb.offline || m.opts.Model == "")
}

// upstreamBlockNote words the refusal blockedUpstream produced: offline names the endpoint and,
// when the monitor has them, the failure's own words; the pre-bind case says the truth instead —
// the server has not answered YET, which on a cold start is a matter of seconds.
func (m Model) upstreamBlockNote() string {
	if !m.hb.offline {
		return "cannot send — still connecting to " + m.opts.Endpoint
	}
	note := "cannot send — server offline (" + m.opts.Endpoint + ")"
	if m.hb.lastFailure != "" {
		note += ": " + m.hb.lastFailure
	}
	return note
}

// ----------------------------------------------------------------------------
// Layout
// ----------------------------------------------------------------------------

// The fixed chrome heights below the transcript. One blank gap row separates the transcript
// from the chrome (layout.md); the ▔ top-edge hairline is one row; the status line is one row;
// the footer is three rows (its top divider, its content line, its bottom rule). The input box
// and the viewport take what remains — the box grows with its content, the viewport gets the rest.
const (
	ruleHeight   = 1 // the ▔ top-edge hairline that caps the bottom chrome, above the status line
	statusHeight = 1
	gapHeight    = 1
	footerHeight = 3 // divider + content + bottom rule

	scrollbarWidth = 1 // the transcript's right-hand scroll-bar gutter (always reserved)

	minInputRows = 1
	maxInputRows = 10 // past this the textarea scrolls internally rather than growing further
	borderFrame  = 2  // the input border's left + right columns
	inputPadding = 2  // the input border's left + right padding columns
)

// layout sizes the viewport and input box to the current window. The input box auto-grows
// with its content (clamped), and the viewport gets the height left after the gap row, the
// ▔ top-edge hairline, the status row, the input box (its content rows plus a top border — the
// divider below it belongs to the footer), and the footer. A floor of one row keeps the
// viewport valid on a tiny window.
func (m *Model) layout() {
	m.viewport.SetWidth(max(1, m.width-scrollbarWidth)) // reserve the scroll-bar gutter column
	m.input.SetWidth(m.inputInnerWidth())
	before := m.input.Height()
	m.input.SetHeight(m.inputRows())
	if m.input.Height() != before {
		m.reseatInput() // the box grew/shrank: re-clamp a scroll offset SetHeight left stale (ISSUES #2)
	}

	inputBoxHeight := m.input.Height() + 1 // content rows + top border (no bottom — it is the footer's divider)
	vpHeight := m.height - ruleHeight - statusHeight - gapHeight - inputBoxHeight - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.SetHeight(vpHeight)
	m.refreshViewport()
}

// transcriptWidth is the column budget the transcript body wraps to: the viewport's own width
// less the right gutter (bodyRightGutter), floored at one column so a window too narrow to hold
// the gutter still wraps rather than going zero or negative. The viewport keeps its full width —
// only the content is narrower — so the prompt-offset arithmetic (wrappedOffset) and the mouse
// mapping still measure against the viewport, and the gutter shows up as unpainted columns.
func (m Model) transcriptWidth() int {
	return max(1, m.viewport.Width()-bodyRightGutter)
}

// inputInnerWidth is the textarea's text width: the window less the border and padding
// columns, floored at one so a very narrow window does not produce a zero-width box.
func (m *Model) inputInnerWidth() int {
	return max(1, m.width-borderFrame-inputPadding)
}

// inputRows is the textarea's height in visual rows at the current window width — the editor's
// own rows() clamp, fed the width the Model derives (inputInnerWidth). The box grows as the human
// types a multi-line message and stops growing at the cap, where the textarea scrolls internally.
func (m *Model) inputRows() int {
	return m.promptEditor.rows(m.inputInnerWidth())
}

// refreshViewport re-renders the transcript into the viewport and, unless the human has
// scrolled away (detached), ends the repaint at the tail so generated output stays in view as
// it streams. The last user prompt still opens at the top of the visible area: the trailing-blank
// padding below a reply shorter than a screen puts the bottom ON the prompt row, and once the
// reply outgrows the screen the same call follows the tail while applyStickyHeader overlays the
// owning prompt at row 0. While detached the offset is left exactly where the human put it;
// submit re-arms following. The body is rendered to transcriptWidth — the viewport's width less
// the right gutter — while the prompt offset still measures against the viewport's own width,
// which is what soft-wraps the stored lines.
//
// It is also where a live transcript drag-selection lives or dies, by the keep-if-unchanged rule
// (transcriptSel.spanUnchanged, mouse.go): the predicate is evaluated against the OUTGOING lines
// before the fresh ones replace them, so a selection over settled text survives the repaint a
// streamed token causes and one whose own lines were rewritten (the streaming tail, a rewrap) is
// dropped instead of left pointing at text that has moved.
func (m *Model) refreshViewport() {
	rendered := m.transcript.renderView(m.th, m.transcriptWidth())
	if !m.transcriptSel.spanUnchanged(m.lines, rendered.lines) {
		m.transcriptSel = transcriptSel{} // the ground under the span moved: let go (mouse.go)
	}
	m.lines = rendered.lines // stashed for the sticky-header overlay (View)
	m.userBlocks = rendered.userBlocks
	if m.detached {
		m.viewport.SetContentLines(rendered.lines)
		// Content that SHRANK under the held offset is clamped back to the bottom by
		// SetContentLines; the human is looking at the tail again, so following resumes and the
		// invariant "detached ⇔ off the bottom" stays total. Growth never lands here — the offset
		// is untouched and the new tail is below it. This is the one place a repaint touches the
		// flag, and it only ever CLEARS it: no repaint may fake a human scroll.
		if m.viewport.AtBottom() {
			m.detached = false
		}
		return
	}
	lines := rendered.lines
	if rendered.lastUserStart >= 0 {
		// Pad with trailing blank rows so the viewport can scroll the prompt all the way to the top
		// even when the reply beneath it is shorter than a screen; otherwise the bottom is clamped
		// to maxYOffset (totalRows-height) and the prompt sits mid-screen.
		off := wrappedOffset(rendered.lines[:rendered.lastUserStart], m.viewport.Width())
		if need := off + m.viewport.Height(); len(lines) < need {
			lines = append(lines, make([]string, need-len(lines))...)
		}
	}
	m.viewport.SetContentLines(lines)
	m.viewport.GotoBottom() // on padded short content the bottom IS the prompt row; otherwise the real tail
}

// ----------------------------------------------------------------------------
// View
// ----------------------------------------------------------------------------

// View stacks the transcript, a single blank line, the ▔ top-edge hairline, the status line,
// the bordered input box, and the footer bar, filling the alternate screen (layout.md). Before
// the first WindowSizeMsg there is no geometry to lay out, so it shows a minimal placeholder. The
// approval or ask prompt, when one is pending, sits between the transcript and the blank line; the
// viewport is shrunk on this local copy to make room (View has a value receiver, so the
// stored layout is untouched). It also places the frame's cursor: the real terminal one, at the
// prompt caret, wherever the box is editable (promptCursor).
func (m Model) View() tea.View {
	if !m.ready {
		return tea.NewView("apogee — starting…")
	}

	var prompt string
	if m.state == stateAwaitingApproval && m.pending != nil {
		prompt = m.approvalPrompt(m.pending.Request)
	}
	if m.state == stateAwaitingAsk && m.pendingAsk != nil {
		prompt = m.askPrompt(m.pendingAsk.Request)
	}
	// The autocomplete overlay (idle, and the "@file" region while running) and the staged
	// interjection rows sit just above the input box. They, and the approval/ask prompt, each steal
	// rows from the transcript viewport, so shrink it by their combined height before rendering.
	dropdown := m.renderAutocomplete()
	queued := m.renderPendingInterjections()
	browser := m.renderSessionBrowser()
	shrink := 0
	if prompt != "" {
		shrink += lipgloss.Height(prompt)
	}
	if browser != "" {
		shrink += lipgloss.Height(browser)
	}
	if dropdown != "" {
		shrink += lipgloss.Height(dropdown)
	}
	if queued != "" {
		shrink += lipgloss.Height(queued)
	}
	if shrink > 0 {
		h := m.viewport.Height() - shrink
		if h < 1 {
			h = 1
		}
		m.viewport.SetHeight(h)
	}

	// Draw the transcript with its sticky header, then hang the scroll-bar gutter off its right
	// edge. The bar's height matches the viewport's current height (already shrunk above when an
	// overlay is shown), so the two columns line up row-for-row.
	body := m.applyStickyHeader(m.viewport.View())
	body = m.highlightTranscript(body) // overlay any transcript drag-selection on the composed rows
	body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.renderScrollbar(m.viewport.Height()))
	rows := []string{body}
	if prompt != "" {
		rows = append(rows, prompt)
	}
	// The /sessions browser overlay sits in the same slot as the approval/ask prompt (they never
	// co-occur — the browser is idle-only, the prompts belong to busy states).
	if browser != "" {
		rows = append(rows, browser)
	}
	// The single blank line between chat content and the bottom chrome (layout.md), then the
	// ▔ top-edge hairline capping the chrome, the status line, the autocomplete overlay (when
	// open), the input box, and the footer.
	rows = append(rows, "", m.topRule(), m.statusLine())
	if dropdown != "" {
		rows = append(rows, dropdown)
	}
	// The staged interjection rows sit closest to the box: they are what ⏎ just put there, and
	// what Backspace on an empty box takes back (ADR 0025).
	if queued != "" {
		rows = append(rows, queued)
	}
	rows = append(rows, m.inputView(), m.footerView())

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, rows...))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion // enable wheel scrolling (Update routes MouseWheelMsg)
	v.Cursor = m.promptCursor()
	return v
}

// promptCursor is the real terminal cursor this frame shows — the caret in the input box — or nil
// to hide it. Visibility follows EDITABILITY (inputEditable: idle, the borrowed ask answer, and
// while a worker runs): at awaitingApproval and errored the keyboard belongs to a/d/s and the
// Enter-dismiss, so a caret there would invite typing that goes nowhere. Hiding is by state rather
// than by Blur() — the textarea stays focused through every state (nothing blurs it), which is
// exactly what lets the box keep its content and its selection while it is inert.
//
// The widget reports the caret in ITS own coordinates (nil when blurred, or while the virtual
// cursor is in use), so the position is translated by the content rectangle's origin — the same
// rectangle a mouse click maps back through (inputContentRect), which is what keeps the drawn caret
// and click-to-position in agreement. The box is bottom-anchored above the footer, so the overlays
// this View stacks above it never move that origin.
func (m Model) promptCursor() *tea.Cursor {
	if !m.inputEditable() {
		return nil
	}
	c := m.input.Cursor()
	if c == nil {
		return nil
	}
	x0, y0, _, _ := m.inputContentRect()
	c.X += x0
	c.Y += y0
	return c
}

// stickyHeaderSpan reports which content lines the sticky-header overlay paints over the top of
// the viewport: the m.lines range [start, start+count), drawn at rows 0..count-1. count is 0 when
// nothing is overlaid — no user block owns the top row, or the incoming prompt has already pushed
// this one fully out — in which case every row shows its natural (scroll-offset) content line.
//
// It is the single source of truth for the overlay's geometry: applyStickyHeader draws from it and
// contentLineAt inverts it, so what the mouse addresses can never drift from what is drawn.
func (m Model) stickyHeaderSpan() (start, count int) {
	if len(m.userBlocks) == 0 {
		return 0, 0
	}
	o := m.viewport.YOffset()
	cur := -1
	for i, b := range m.userBlocks { // the greatest start <= o owns the top of the screen
		if b.start <= o {
			cur = i
		} else {
			break
		}
	}
	if cur < 0 {
		return 0, 0 // the top content sits above the first prompt — nothing to stick
	}
	b := m.userBlocks[cur]
	push := 0
	if cur+1 < len(m.userBlocks) {
		nat := m.userBlocks[cur+1].start - o // the next prompt's natural row within the viewport
		if nat < b.count {
			push = b.count - nat // the incoming prompt is shoving this header up
		}
	}
	if push >= b.count {
		return 0, 0 // this header is fully pushed out; the next one is already the natural top
	}
	return b.start + push, b.count - push // the still-visible (bottom) header rows
}

// contentLineAt maps a viewport row to the index into m.lines of the content ACTUALLY DRAWN there.
// Off the header zone that is just the scroll offset plus the row; within it, it is the overlaid
// header line — the sticky header covers the content the offset would otherwise put there, so
// addressing that content by row would name text the human cannot see. Selection (the mouse maps a
// click through here) and the selection highlight both go through this one mapping, which is what
// keeps copy equal to sight on the header rows (mouse.go).
func (m Model) contentLineAt(row int) int {
	if start, count := m.stickyHeaderSpan(); row >= 0 && row < count {
		return start + row
	}
	return m.viewport.YOffset() + row
}

// applyStickyHeader overlays the user prompt that owns the content at the top of the viewport,
// frozen at row 0, so the prompt the on-screen replies belong to is always visible. As the next
// section's prompt scrolls up into the header zone it pushes the current header off the top — a
// position: sticky hand-off — until the next prompt is itself the natural top line. With the view
// parked on the prompt row this redraws it where it already is, a visual no-op.
func (m Model) applyStickyHeader(view string) string {
	start, count := m.stickyHeaderSpan()
	if count == 0 {
		return view
	}
	viewLines := strings.Split(view, "\n")
	for i, hl := range m.lines[start : start+count] {
		if i < len(viewLines) {
			viewLines[i] = hl
		}
	}
	return strings.Join(viewLines, "\n")
}

// renderScrollbar draws the one-column gutter reserved at the transcript's right edge (layout).
// When the content overflows the viewport it shows a thumb sized to the visible fraction and
// positioned by the scroll percent; when everything fits it is blank, so the bar appears only
// while there is something to scroll. The returned block is exactly h rows tall so it lines up
// with the viewport view it is joined to.
func (m Model) renderScrollbar(h int) string {
	if h < 1 {
		return ""
	}
	total := m.viewport.TotalLineCount()
	rows := make([]string, h)
	if total <= h { // nothing to scroll — keep the gutter blank
		for i := range rows {
			rows[i] = " "
		}
		return strings.Join(rows, "\n")
	}
	thumb := max(1, h*h/total)
	pos := int(m.viewport.ScrollPercent() * float64(h-thumb)) // 0 (top) … h-thumb (bottom)
	for i := range rows {
		if i >= pos && i < pos+thumb {
			rows[i] = m.th.scrollThumb.Render("█")
		} else {
			rows[i] = m.th.scrollTrack.Render("│")
		}
	}
	return strings.Join(rows, "\n")
}

// inputView renders the textarea inside the rounded, dark-gray, black-bg border (no bottom
// edge — the footer's top rule is the shared divider). lipgloss.Width sets the box's total
// width including the border and padding, so the box always spans the window and the footer
// below it aligns. The ▔ top-edge hairline that caps the bottom chrome is a separate row above
// the status line (topRule), not part of this box, so the status line reads as sitting directly
// above the input box.
func (m Model) inputView() string {
	return m.th.inputBorder.Width(m.width).Render(m.highlightInput(m.input.View()))
}

// topRule renders the full-width ▔ hairline that marks the top edge of the bottom chrome: a
// dim-gray hairline on a black field (topDivider, dimmer than the footer's chromeRule so it
// recedes). It sits directly below the transcript's one blank gap row and above the status line
// (layout.md), capping the whole bottom section while the status line stays directly above the
// input box.
func (m Model) topRule() string {
	return m.th.topDivider.Render(strings.Repeat("▔", m.width))
}

// footerView renders the footer bar: a thin top divider (the shared border with the input box
// above), the content line, and a thin bottom rule. The content shows the host alias, model,
// and static context window on the left and the autonomy mode on the right — string(Mode), so a
// later rung (ModeAllowEdits, P3.4) appears for free. A window too narrow for a bordered bar
// renders nothing rather than overflowing.
func (m Model) footerView() string {
	w := m.width
	if w < 3 {
		return ""
	}
	rule := func(left, right string) string {
		return m.th.chromeRule.Render(left + strings.Repeat("─", w-2) + right)
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		rule("├", "┤"),
		m.footerContent(w),
		rule("╰", "╯"),
	)
}

// footerContent composes the footer's content line: host ✦ model ✦ ctx on the left, the mode
// marker on the right, between two dark-gray │ borders on a black field. The mode marker takes
// its own per-mode colour (modeColor), so the segments are styled independently and laid out by
// hand — mirroring statusLine — rather than rendered under one style, which would let the mode's
// colour reset bleed the black field. The host falls back to the endpoint when no alias is
// configured, and the context window is omitted when unknown (0).
func (m Model) footerContent(w int) string {
	info := strings.Join(nonEmpty(append([]string{hostDisplay(m.opts)}, m.upstreamSegments()...)...), " "+glyphAssistant+" ")
	offline := ""
	if m.hb.offline {
		offline = " " + glyphAssistant + " " + offlineLabel
	}
	mode := modeLabel(m.opts.Mode)
	bar := m.th.chromeRule.Render("│")
	field := w - 2 // content columns between the two │ borders (footerView guards w >= 3)

	// One-column margins inside the borders; a black-bg gap justifies the mode marker right.
	gap := field - 2 - lipgloss.Width(info) - lipgloss.Width(offline) - lipgloss.Width(mode)
	if gap < 1 {
		// Too narrow for both segments: keep the left info, truncate to the field, pad black.
		body := ansi.Truncate(" "+info+offline, field, "…")
		body += strings.Repeat(" ", max(0, field-lipgloss.Width(body)))
		return bar + m.th.footerText.Render(body) + bar
	}
	left := m.th.footerText.Render(" " + info)
	if offline != "" {
		// Styled independently, like the mode marker: the segment carries the error tone on the
		// footer's own black field, so the state reads at a glance without recolouring the line.
		left += m.th.footerText.Foreground(colError).Render(offline)
	}
	fill := m.th.footerText.Render(strings.Repeat(" ", gap))
	// footerText keeps the black background; only the foreground swaps to the mode's colour.
	right := m.th.footerText.Foreground(modeColor(m.opts.Mode)).Render(mode) + m.th.footerText.Render(" ")
	return bar + left + fill + right + bar
}

// The two words the footer says about the Upstream that are not facts about a model.
const (
	// connectingLabel stands where the model and its window go while a wired heartbeat has not
	// bound a model yet — the honest word for the seconds between the first paint and the first
	// landed beat (startup discovery is now that beat).
	connectingLabel = "connecting…"
	// offlineLabel is the footer's own word for the state a send is refused in.
	offlineLabel = "offline"
)

// upstreamSegments are the footer's upstream facts: the display model and its context window, or
// the single word "connecting…" while a wired heartbeat has not bound a model yet. The two are
// replaced TOGETHER — a context-window pin is not a fact about a model nobody has named yet — and
// with the monitor unwired the pair is rendered exactly as it always was.
func (m Model) upstreamSegments() []string {
	if m.opts.Model == "" && m.opts.Heartbeat != nil {
		return []string{connectingLabel}
	}
	return []string{displayModel(m.opts.Model), formatTokens(m.opts.ContextWindow)}
}

// newStartupView builds the one-time start-up box's facts from the resolved display Options — the
// single source both newModel's seed and /clear's re-seed (startNewSession) read, so the fresh-launch
// box and the post-/clear box can never drift. Version is BaseVersion (the clean release version), not
// the full provenance-tagged Options.Version the footer shows.
func newStartupView(opts Options) startupView {
	return startupView{
		Logo:    strings.TrimRight(apogeeLogo, "\n"),
		Host:    hostDisplay(opts),
		Model:   displayModel(opts.Model),
		Context: formatTokens(opts.ContextWindow),
		Version: opts.BaseVersion,
	}
}

// hostDisplay is the upstream host label the footer and the start-up box both show: the
// configured HostAlias, or the raw endpoint URL when no alias is set. It is the single source of
// that fallback so the box's host and the footer's host can never diverge (they read one function
// of the same Options).
func hostDisplay(opts Options) string {
	if opts.HostAlias != "" {
		return opts.HostAlias
	}
	return opts.Endpoint
}

// modelWeightExt is the set of weight-file extensions displayModel strips. It is a fixed
// whitelist rather than a blind filepath.Ext trim because model ids carry version dots
// ("qwen2.5-coder"), and an unconditional strip would eat the ".5-coder" tail.
var modelWeightExt = map[string]bool{
	".gguf":        true,
	".ggml":        true,
	".bin":         true,
	".safetensors": true,
}

// displayModel renders a model identifier for the footer. A local server often reports its
// active model as a filesystem path (e.g. /models/qwen2.5-coder-7b.gguf); the footer wants just
// the name, so this strips the directory and a known weight-file extension. It is display-only:
// opts.Model stays the canonical id sent to the server on every request (wire.go), so the strip
// never reaches the wire — mirroring modeLabel.
//
// No model at all renders as nothing, so the footer and the start-up box simply omit the segment
// (nonEmpty). The guard is not cosmetic: filepath.Base("") is ".", and with the model bound late
// by the first heartbeat there is now a real window at every cold start in which a lone "." would
// be shown as the model.
func displayModel(s string) string {
	if s == "" {
		return ""
	}
	base := filepath.Base(s)
	if modelWeightExt[strings.ToLower(filepath.Ext(base))] {
		base = base[:len(base)-len(filepath.Ext(base))]
	}
	return base
}

// modeLabel renders an autonomy mode as a human-friendly footer label (spaced, not the
// hyphenated wire form). It is display-only: the --mode flag, config.yaml, and every domain
// string stay canonical ("ask-before"); only the footer reads "ask before".
func modeLabel(m domain.Mode) string {
	switch m {
	case domain.ModePlan:
		return "plan"
	case domain.ModeAskBefore:
		return "ask before"
	case domain.ModeAllowEdits:
		return "allow edits"
	case domain.ModeAuto:
		return "auto"
	default:
		return string(m)
	}
}

// modeColor maps an autonomy mode to its footer-marker colour (the palette in theme.go). An
// unknown mode falls back to the footer's faint tone, so an off-ladder value is never invisible.
func modeColor(m domain.Mode) color.Color {
	switch m {
	case domain.ModePlan:
		return colModePlan
	case domain.ModeAskBefore:
		return colModeAskBefore
	case domain.ModeAllowEdits:
		return colModeAllowEdits
	case domain.ModeAuto:
		return colModeAuto
	default:
		return colFaint
	}
}

// throughputSuffix is the status line's "· N tok/s" readout while a Turn generates, timed off
// the last completion's server-reported token count (foldStats). It renders nothing below one
// token per second (an unmeasured or sub-1 tok/s turn), keeping the black status field clean.
func (m Model) throughputSuffix() string {
	if m.tokPerSec < 1 {
		return ""
	}
	return m.th.statusBar.Render(fmt.Sprintf(" · %.0f tok/s", m.tokPerSec))
}

// statusLine renders what is happening on the left and the live context gauge (or a key hint)
// on the right, justified across the window. While a worker runs the left slot is the live
// activity phrase plus an elapsed clock ("⣻ reading · main.go · 3s") — what the human is
// actually asking, in place of the turn index, which answered none of it. Idle renders nothing
// there (the input box below already invites a message) beyond what is queued; the blocked and
// errored states keep their own words, with no spinner and no clock (nothing is ticking). Any
// staged interjections add their count to whatever the slot holds. The left slot hangs off
// the transcript's own text column: it leads with bodyIndent, so the spinner lines up with the
// body text above it rather than with the ✦/❯ marker column (layout.md). It reads only display
// values off Options and the model's own state — never off the Engine mid-step.
func (m Model) statusLine() string {
	left := m.th.statusBar.Render(bodyIndent)
	switch m.state {
	case stateRunning:
		left += m.spin.view(m.th) + m.th.statusBar.Render(" "+m.runningPhrase(time.Now())) + m.throughputSuffix()
	case stateAwaitingApproval:
		left += m.th.statusBar.Render("approval needed")
	case stateAwaitingAsk:
		left += m.th.statusBar.Render("answer needed")
	case stateErrored:
		left += m.th.statusError.Render("error")
	}
	// What is waiting to go out rides the same slot, in every state: a queue that survives a stop
	// or an error (it does) must keep saying so at idle too, where the slot is otherwise empty.
	left += m.queuedSegment(m.state != stateIdle)
	// Fill the whole width with black-bg cells — segments and the justify gap alike — so
	// the info line reads as one solid black bar joined to the prompt box below it. A plain
	// justify gap would show the terminal's default background through the seam. statusRight
	// returns a fully pre-styled string (the gauge carries its own per-cell backgrounds), so
	// it is concatenated raw rather than re-wrapped, which would clobber those backgrounds.
	right := m.statusRight()
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return ansi.Truncate(left, max(0, m.width), "")
	}
	return left + m.th.statusBar.Render(strings.Repeat(" ", gap)) + right
}

// runningPhrase composes the running left slot's text: the activity phrase and the clock
// counting THIS activity ("thinking · 12s"). An activity with no phrase — the defensive case,
// since every path into stateRunning sets one — degrades to the bare clock rather than to a
// dangling separator. now is passed in so the composition is testable off the wall clock.
func (m Model) runningPhrase(now time.Time) string {
	clock := formatElapsed(m.act.elapsed(now))
	if phrase := m.act.text(); phrase != "" {
		return phrase + " · " + clock
	}
	return clock
}

// statusRight is the status line's right slot: the live context gauge when token usage is
// known, else a state-appropriate key hint. The gauge is empty only until the first UsageEvent
// folds a turn's total into ctxUsed (or after /clear and /compact zero it) — so the hint shows
// before any usage is measured and the gauge takes the slot the moment it is.
func (m Model) statusRight() string {
	// A primed Ctrl+C takes the slot: tell the human a second press inside the window quits.
	if !m.lastCtrlC.IsZero() {
		return m.th.statusBar.Render("press ctrl+c again to quit")
	}
	// A fresh mouse-copy confirmation briefly takes the slot (flashClearMsg clears it).
	if m.flash != "" {
		return m.th.statusBar.Render(m.flash)
	}
	if g := m.contextGauge(); g != "" {
		return g
	}
	switch m.state {
	case stateRunning:
		return m.th.statusBar.Render("esc stop")
	case stateErrored:
		return m.th.statusBar.Render("enter dismiss")
	default:
		return ""
	}
}

// contextGauge renders the live token-usage gauge for the status line. Used is the latest
// top-level UsageEvent's total-token count (foldStats); until the first turn reports usage — or
// on a server that omits it — Used is 0 and the gauge renders nothing, the static window
// showing in the footer instead.
func (m Model) contextGauge() string {
	return contextUsage{Used: m.ctxUsed, Limit: m.opts.ContextWindow}.view(m.th)
}

// contextUsage is the live context-window gauge's data: tokens Used out of the window Limit.
// It is self-hiding — view renders nothing until usage is known.
type contextUsage struct {
	Used  int
	Limit int
}

// gaugeWidth is the bar strip's width in terminal cells. Eighth-block glyphs give eight fill
// levels per cell, so the bar resolves Used/Limit to 1/(gaugeWidth*8) of the window.
const gaugeWidth = 10

// gaugeEighths are the partial-cell glyphs for 1–7 eighths of a filled cell — the sub-cell
// granularity that makes the fill edge advance smoothly (llama-launcher's bar look).
var gaugeEighths = []rune{'▏', '▎', '▍', '▌', '▋', '▊', '▉'}

// view renders the gauge as "<used> <pct>% <bar>", or "" when usage is unknown. The numeric
// prefix is faint-on-black status text; the bar is a solid two-tone strip (renderGaugeBar)
// carrying its own per-cell backgrounds, so the whole string is pre-styled and must be
// concatenated raw by the caller (never re-wrapped in a background style).
//
// The percentage is clamped to 100 because the bar already is (renderGaugeBar): a conversation
// carried across a switch to a smaller window overfills its new limit, and a full bar labelled
// "137%" is a rendering bug, not a reading. The unclamped Used still shows beside it, so an
// over-window fill is visible as the token count rather than as an impossible percentage.
func (c contextUsage) view(th theme) string {
	if c.Used <= 0 || c.Limit <= 0 {
		return ""
	}
	pct := min(c.Used*100/c.Limit, 100)
	prefix := th.statusBar.Render(fmt.Sprintf("%s %d%% ", formatTokens(c.Used), pct))
	return prefix + renderGaugeBar(th, c.Used, c.Limit)
}

// renderGaugeBar paints used/limit as one continuous two-color strip in the llama-launcher
// style: the filled portion as full blocks in the gauge colour, an eighth-block partial cell
// for sub-cell granularity, then the empty remainder as a solid dark-gray track painted
// behind it — so the fill meets the track directly, with no terminal-default gap. Any nonzero
// fraction shows at least a one-eighth sliver; an over-limit Used clamps to a full bar.
// (Ported from llama-launcher internal/launcher/memformat.go writeBar.)
func renderGaugeBar(th theme, used, limit int) string {
	eighths := (used*gaugeWidth*8 + limit/2) / limit // round to the nearest eighth
	if max := gaugeWidth * 8; eighths > max {
		eighths = max
	}
	if used > 0 && eighths == 0 {
		eighths = 1
	}
	full := eighths / 8
	rem := eighths % 8
	empty := gaugeWidth - full
	if rem > 0 {
		empty--
	}

	var b strings.Builder
	if full > 0 {
		b.WriteString(th.gaugeFill.Render(strings.Repeat("█", full)))
	}
	if rem > 0 {
		// The eighth glyph's ink is the fill colour, its paper the track colour.
		b.WriteString(th.gaugeFill.Background(colDarkGray).Render(string(gaugeEighths[rem-1])))
	}
	if empty > 0 {
		b.WriteString(th.gaugeTrack.Render(strings.Repeat(" ", empty)))
	}
	return b.String()
}

// formatTokens renders a token count compactly: bare below 1000, else "<n>k" (32768 → 32k).
// Zero renders as "" so an unknown window is simply omitted.
func formatTokens(n int) string {
	if n <= 0 {
		return ""
	}
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%dk", n/1000)
}

// formatTokensFine renders a token count the way a COMPARISON needs it — one decimal above a
// thousand (4212 → "4.2k"), where formatTokens' whole-thousands rounding would print both sides
// of a narrow overrun identically and read like a bug. The status line keeps the coarse form: a
// window size is a round number, a measured count is not.
func formatTokensFine(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%.1fk", float64(n)/1000)
}

// formatBytes renders a file size for the session notice: bytes below a kibibyte, else KiB or
// MiB to one decimal (3174 → "3.1 KiB"). Binary units, because the number it describes is a
// file's size on disk.
func formatBytes(n int) string {
	const kib = 1024
	switch {
	case n < kib:
		return fmt.Sprintf("%d B", n)
	case n < kib*kib:
		return fmt.Sprintf("%.1f KiB", float64(n)/kib)
	default:
		return fmt.Sprintf("%.1f MiB", float64(n)/(kib*kib))
	}
}

// nonEmpty returns the non-empty arguments in order — the footer's left segment skips an
// absent host, model, or context window rather than rendering a dangling separator.
func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// popupBudget derives the screen-budget caps a pending prompt hands renderPopup so the bordered
// pane never pushes the input box off-screen (D2). rows is the number of selectable choices on
// offer (0 for the choiceless approval prompt); maxRows caps the scrolled row window (≥ 0) and
// maxBody caps the wrapped body block (≥ 1). m.viewport.Height() here is the full, pre-shrink
// layout height — View shrinks a local copy AFTER the prompt renders (verified View slot) — so it
// is the true screen budget: keep ≥ 3 transcript rows visible, spend the chrome (the 2 borders +
// the title + the hint), give the rows priority (they are what the human acts on), and let the
// body keep ≥ 1 row and overflow into the explicit "… (+N more lines)" marker.
func (m Model) popupBudget(rows int) (maxBody, maxRows int) {
	avail := max(6, m.viewport.Height()-3)
	const chrome = 4 // the 2 borders + the title row + the hint row
	maxRows = min(rows, maxAskChoiceRows, max(0, avail-chrome-1))
	maxBody = max(1, avail-chrome-maxRows)
	return maxBody, maxRows
}

// approvalPrompt renders the pending tool call the human must rule on as a bordered popup pane
// above the input box (the shared popup module; D7/D8): the title carries the RAW tool name
// verbatim (not the friendly transcript label — the approval flow is a security surface, so the
// human sees exactly the tool that will run), the body carries a non-empty Reason then the
// pretty-printed Arguments, and the hint carries the decision legend. Every model-authored string
// (tool name, reason, args) is escape-stripped at this call site; stripEscapes removes only the
// ESC byte, so the raw tool name is preserved verbatim. Empty/null arguments add no body, and the
// module wraps the reason so it can never be silently truncated on this security surface. Only the
// top-level (Depth == 0) prompt is rendered this phase.
func (m Model) approvalPrompt(req domain.ApprovalRequest) string {
	var parts []string
	if req.Reason != "" {
		parts = append(parts, "reason: "+stripEscapes(req.Reason))
	}
	if args := prettyJSON(req.Arguments); args != "" {
		parts = append(parts, stripEscapes(args))
	}

	maxBodyRows, _ := m.popupBudget(0)
	spec := popupSpec{
		title:       "approve " + stripEscapes(req.Tool) + "?",
		body:        strings.Join(parts, "\n\n"), // reason and args separated by one blank line; no stray blanks when one is absent
		maxBodyRows: maxBodyRows,
		selected:    -1, // no rows on the approval prompt
		hint:        "a allow · d deny · s allow-session · esc cancel",
	}
	return renderPopup(m.th, spec, m.width)
}

// maxAskChoiceRows caps how many ask_user choice rows the popup shows at once (the
// maxAutocompleteItems convention); a longer set scrolls its window around the selection.
const maxAskChoiceRows = 8

// askPrompt renders the pending ask_user question as a bordered popup pane above the input box
// (the shared popup module): the title, the wrapped question body, then any offered choices as
// selectable rows, and a one-line key hint (P3.11; D5/D6/D8). While the input box is empty and
// choices are offered, ↑/↓ move the highlight and ⏎ sends the highlighted label; the moment the
// human types, the highlight drops (selected −1) and ⏎ sends the typed text — so the answer mode
// is always visible in the chrome. Every model-authored string (question, choices) is
// escape-stripped at this call site. The screen budget is derived from the live layout so a long
// question or a long choice set never pushes the input box off-screen: rows get priority (they
// are what the human acts on), the body keeps ≥ 1 row and overflows into the explicit
// "… (+N more lines)" marker (D2).
func (m Model) askPrompt(req domain.AskRequest) string {
	choicesShown := len(req.Choices) > 0 && m.input.Value() == ""

	selected := -1
	hint := "type your answer below · ⏎ send · ⇧⏎/⌥⏎ newline · esc cancel"
	if choicesShown {
		selected = min(max(m.askSel, 0), len(req.Choices)-1) // clamp: routing keeps it in range, this is defensive
		hint = "↑↓ select · ⏎ send · type for a custom answer · esc cancel"
	}

	// Budget against the live layout so a long question or choice set never pushes the input box
	// off-screen (D2); the rows get priority and the body keeps ≥ 1 row (see popupBudget).
	maxBodyRows, rowsShown := m.popupBudget(len(req.Choices))

	spec := popupSpec{
		title:       "the assistant is asking:",
		body:        stripEscapes(req.Question),
		maxBodyRows: maxBodyRows,
		rows:        stripEscapesAll(req.Choices),
		selected:    selected,
		hint:        hint,
		maxRows:     rowsShown,
	}
	return renderPopup(m.th, spec, m.width)
}
