package tui

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/session"
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

	// Session-record write single-flight (the per-Turn save pipeline AND the /sessions verbs).
	// Every write to the store — Save, Rename, Delete — runs off the Update loop on a Cmd
	// goroutine, and no two of them may overlap: Store.Rename is a read-modify-write of the whole
	// record, so a Save running beside it is rolled back, taking a complete Turn off disk (audit
	// 2026-08-01). writeBusy marks one write in flight; anything scheduled while it runs waits in
	// pendingWrites, which coalesces saves (latest-wins — an older intermediate Turn is superseded
	// by the newer snapshot) and keeps renames and deletes in the order they were asked for.
	// saveFailing tracks the last save's success so only the ok→fail and fail→ok transitions are
	// noted — a save failure must never spam the transcript or interrupt the conversation.
	//
	// pendingWrites is a slice in a value-copied Model, so it is REPLACED and re-sliced, never
	// written through (the pendingRebind posture, ADR 0011).
	writeBusy     bool
	saveFailing   bool
	pendingWrites []recordWrite

	// sessionBrowser is the /sessions history-browser overlay's state (sessions.go): the loaded
	// session metas, the current selection, the current-workspace ⇄ all toggle, and any inline
	// delete-confirm or rename edit. Its zero value is "closed", so it lives inline in the
	// value-copied Model like autocompleteState (ADR 0011). It is driven only at idle.
	sessionBrowser sessionBrowser

	// Session naming (autotitle.go; ADR 0022 addendum). autoTitleFired latches the one
	// cosmetic naming call a Session record gets — set when it fires, and set UP FRONT on a resumed
	// record, which already has a name. titleTouched records that a human named this session (the
	// browser's `r`, either form of /rename), which drops a late-landing automatic title (never
	// clobber). pendingTitle stashes a title that resolved before the first Save minted an id to
	// rename, applied at that save's completion, and pendingSource remembers who asked for it — the
	// stash outlives the never-clobber check, so the flush makes it again (flushPendingTitle).
	// sessionName is the same machinery's DISPLAY side: the name this session is currently known
	// by, and the one the frame renders. It is what the human last saw decided rather than what the
	// store holds — a rename that never reaches disk leaves the frame naming the session the human
	// named — and "" means "not named yet", which the seam that renders it answers for itself. It
	// carries untrusted text (a model's reply, a resumed record's Meta), so that seam sanitizes;
	// nothing else reads it.
	// Five plain values, safe in the value-copied Model (ADR 0011); startNewSession resets the
	// four that carry state, so the session /clear opens names itself afresh.
	autoTitleFired bool
	titleTouched   bool
	pendingTitle   string
	pendingSource  titleSource
	sessionName    string

	// actuation is the launcher latch (actuation.go, ADR 0029 D5): which blocking launcher verb is
	// in flight, on what, and the channel its narration is pumped back through. Its zero value is
	// "nothing in flight", and everything on it is a plain value or a channel — a reference header —
	// so it copies with the Model like the overlays above (ADR 0011). While it is held, sends and
	// every switching or actuating verb are refused, and a failed beat counts toward nothing.
	actuation actuation

	// picker is the shared single-select overlay's state (picker.go): which offering it lists and
	// which row is highlighted. Its rows are derived at render time from the state they describe —
	// the /model picker reads hb.models live, the /server picker opts.Servers — so the value itself
	// is three plain values and its zero value is "closed", the sessionBrowser posture (ADR 0011).
	// It is driven only at idle.
	picker picker

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
	// askDraft holds what was in the input box when an ask_user question BORROWED it: the question
	// empties the box for the answer (D5 pre-selects the first choice on an empty box), and an
	// unsent message the human was part-way through typing is not the question's to throw away.
	// restoreAskDraft hands it back the moment the box stops being borrowed. A plain string, so it
	// rides the value-copied Model (ADR 0011).
	askDraft  string
	lastErr   error     // the error behind stateErrored, shown in the status line
	lastCtrlC time.Time // when the last Ctrl+C landed; a second within the window quits
	quitting  bool      // a quit whose exit is deferred: to the worker's terminal Msg while busy (C4), else to the closing flush reaching disk (quit)

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
	// deliveredInterjections is the third copy, and it exists for exactly one boundary: the rows
	// THIS Exchange has already committed. A stop scraps the Exchange (AbortExchange drops
	// everything committed into it, the interjections with it), so without this the delivered rows
	// would survive only as transcript entries the conversation no longer contains — sent by nobody,
	// held by nobody. The cancel fold puts them back on the queue (restageDelivered) and
	// finishWorker forgets them, so they live no longer than the Exchange that read them.
	//
	// interjectSeq mints those ids — a plain counter, incremented on the Update goroutine and
	// never reset, so an id names one row for the life of the session and a delivery report can
	// never be reconciled against a row that merely reused a number.
	box                    *interjectBox
	pendingInterjections   []queuedInterjection
	deliveredInterjections []queuedInterjection
	interjectSeq           int

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
	transcript transcript
	// workdir is the footer's workspace fact: Options.Workspace respelled for display
	// (workdirDisplay), resolved ONCE at construction rather than per repaint because it reads the
	// home directory off the environment and neither input can change while the session runs — the
	// workspace is fixed at launch and /clear opens the next session in the same one. A plain
	// string, so it rides the value-copied Model (ADR 0011); "" means there is nothing to name and
	// the footer drops the segment.
	workdir       string
	th            theme // the palette and reusable styles, built once at construction
	width, height int
	ready         bool // a WindowSizeMsg has sized the layout at least once
	// detached says the human scrolled off the tail: refreshViewport holds the scroll position
	// where they put it instead of following new content, until a re-arm event (submit, /clear,
	// /continue, a flushed interjection, a resumed session) clears it. The zero value is
	// attached — a fresh Model follows the tail with no construction help. A plain bool by
	// ADR 0011: the Model is copied by value on every Update.
	detached bool

	// Last render output, stashed by refreshViewport for View's sticky-header overlay and the
	// mouse: the physical lines the viewport holds, the line range of every user block, and — one
	// entry per line, the zero value where a line is no click target — what each line is to a
	// motionless click (render.go). lineTargets is the paint's OWN accounting, carried across
	// rather than re-derived, so a click resolves against exactly the lines that were drawn; it is
	// a plain slice of plain values, so ADR 0011's copy rule needs no exception.
	lines       []string
	userBlocks  []userBlock
	lineTargets []lineTarget
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

	// Resolve the root every tool card's paths are printed relative to, before anything can record a
	// call against the transcript (workspacepath.go). It is set on the transcript rather than kept
	// on the Model because the fold that records a call reaches no Model, and reset preserves it —
	// /clear opens a new session in the same workspace.
	m.transcript.ws = newWorkspaceRoot(opts.Workspace)

	// Resolve the footer's spelling of that same workspace once, for the same reason: the home
	// lookup is an environment read, and neither it nor the workspace changes for the life of the
	// session. A failed lookup leaves home "", which simply returns the path unrespelled — a footer
	// that says the long form is right, one that guesses a home is not.
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	m.workdir = workdirDisplay(opts.Workspace, home)

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
//
// All three notices are added EPHEMERALLY (addEphemeralNote): each is derived here from the record
// being resumed, so the next resume derives it again — persisting one would stack another copy into
// the record on every reopen while telling a re-read nothing it does not already know.
func (m *Model) replayResumed(r *ResumedSession) {
	if r == nil {
		return
	}
	// A resumed record already HAS a name — possibly one the human chose — so the automatic naming
	// call is latched off for the life of this session (Ratified design 5). A bare /rename still
	// regenerates on demand: the toggle answers "name my sessions for me", not "never ask".
	m.autoTitleFired = true
	m.nameSession(r.Title) // the window opens under the name this record already carries
	m.ctxUsed = r.CtxUsed  // relight the gauge near the session's last observed fill
	entries, err := decodeTranscript(r.Transcript)
	if err != nil || len(entries) == 0 {
		m.transcript.addEphemeralNote("resumed: " + r.Title + " (no scrollback recorded — the model still remembers)")
	} else {
		m.transcript.replay(entries)
		m.transcript.addEphemeralNote("resumed: " + r.Title)
	}
	if r.InExchange {
		m.transcript.addEphemeralNote(interruptedNote)
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
//
// Every notice is added EPHEMERALLY (addEphemeralNote), for the same reason the resume notices are
// (replayResumed): all of it is re-derived from the live workspace at each boundary — the files are
// re-read and the report recomputed — so persisting it would tell a re-read nothing it will not work
// out again while stacking another copy into the record on every startup and every resume. Context
// files are session-scoped prompt data restored by re-reading (ADR 0026), so the record loses
// nothing by not keeping the notice.
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
		m.transcript.addEphemeralNote("context: " + strings.Join(loaded, ", "))
	}
	for _, note := range unreadable {
		m.transcript.addEphemeralNote(note)
	}
	if report.Oversize() {
		m.transcript.addEphemeralNote("standing system content ~" + formatTokensFine(report.StandingTokens) +
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

	case tea.ModeReportMsg:
		// The terminal answered one of the mode queries bubbletea sends at start-up. Exactly one
		// of them concerns the layout: mode 2027 (Unicode core). bubbletea acts on that answer by
		// switching the PAINTER to ansi.GraphemeWidth and then passes the same message on to
		// Update (bubbletea/v2@v2.0.7/tea.go:786-798), so this is the only place apogee can learn
		// which measure its own output is about to be painted in. The width authority follows it —
		// measuring in a method the painter did not choose is precisely the defect it exists to
		// close (width.go). Nothing else in the program reads this message; before the authority
		// there was no case for it and it fell through to the input widget, which ignores it.
		before := m.th.measure
		m.th.measure = m.th.measure.observe(msg)
		if m.th.measure != before && m.ready {
			m.layout() // the measure moved: re-wrap and repaint everything against the new one
		}
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
		m.dismissAutocomplete() // a stale menu never shares the frame with a decision surface
		m.layout()              // the pane the decision turns on outranks the draft's extra rows
		return m, nil

	case askReqMsg:
		// The worker's Asker hands a free-text question to the Update loop; record it, switch
		// state, and re-focus the (emptied) input so the human types the answer. View renders
		// the question; submitAnswer replies on msg.Reply when the human submits (P3.11).
		m.state = stateAwaitingAsk
		m.pendingAsk = &msg
		m.askSel = 0            // first choice pre-selected while the input is empty (D5); no-op when there are no choices
		m.dismissAutocomplete() // a stale menu never shares the frame with a decision surface
		// The box is BORROWED, not repossessed: whatever the human was part-way through typing is
		// stashed and handed back when the question lets go of it (restoreAskDraft). Emptying it
		// without that stash discarded an unsent message outright.
		m.askDraft = m.input.Value()
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
		//
		// "Held" has to cover what the worker already DELIVERED, too. The AbortExchange above drops
		// those rows with the rest of the scrapped Exchange, so restageDelivered puts them back on
		// the queue — before the note, which is what counts them — rather than leaving them as
		// transcript entries the conversation no longer contains.
		m.eng.AbortExchange()
		m.transcript.addNote("cancelled")
		m.restageDelivered()
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
		// An async save returned. Note the ok↔fail transition once, queue any title waiting for the
		// record this save put on disk, and dispatch the next queued record write (saveComplete).
		return m, m.saveComplete(msg.Err)

	case recordWriteDoneMsg:
		// The other record writes — a Rename, a Delete, or one of the two retargets (Rotate,
		// Activate) — returned. Release the single-flight latch, dispatch whatever waited behind it,
		// and re-list for the browser verbs that repaint over the result (foldRecordWrite).
		return m, m.foldRecordWrite(msg)

	case autoTitleMsg:
		// The out-of-band naming call returned (autotitle.go). Rename the Session record to what it
		// produced, or drop it silently — a failed call, an unusable reply, and a session the human
		// has since named themselves all leave the heuristic title standing. Nothing else moves: the
		// title is not a Turn, so no transcript entry, no event, and no gauge answers to it.
		return m, m.foldAutoTitle(msg)

	case manualTitleMsg:
		// A bare /rename's naming call returned (autotitle.go). This one was asked for, so it answers:
		// the title applies even over a name the human set by hand, and every outcome — the failures
		// included — lands as a note. Still not a Turn: no event, no gauge, no engine.
		return m, m.foldManualTitle(msg)

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
		if m.transcript.hasOpenToolCall() {
			// The frame the status line spins on is also the LIVE STAR's clock: a block still holding
			// an open call paints its header glyph from this frame's parity (layout.md, "The live
			// star"; blockState.star), so the flip needs a repaint the tick did not use to do. It is
			// asked for ONLY while something is open — with nothing live every block paints the same
			// at either phase, and re-rendering the whole scrollback ten to twenty times a second for
			// an identical result would be work for its own sake, and would put the keep-if-unchanged
			// rule (refreshViewport) between the human and every drag-selection they hold through a
			// turn. A selection spanning a header that DOES flip is dropped, which is that same rule
			// doing its ordinary job on a line that changed.
			m.refreshViewport()
		}
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

	case actuationMsg:
		// One item off the in-flight launcher verb's pump (actuation.go): a narration step to note
		// and re-arm on, or the completion that releases the latch. It arrives off a Cmd goroutine
		// like a beat, and carries the same kind of generation guard.
		return m.foldActuation(msg)

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

	// The /model and /server picker is the browser's simpler sibling and claims keys the same way:
	// while it is open (idle only) the selection, the accept and esc are all its own (picker.go).
	if m.state == stateIdle && m.picker.open {
		return m.pickerKey(msg)
	}

	// While the autocomplete overlay is open, it claims the navigation, accept, and dismiss keys —
	// including enter and tab — before the normal routing below. Any other key returns
	// handled=false and falls through to edit the input (which re-derives it). Every region opens
	// in BOTH states (computeAutocomplete), so an interjection reaches a file, a skill and a
	// reporting command as easily as a submitted message does; the per-command while-running policy
	// is applied at accept (commandRunnable), not by hiding the menu.
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
		m.layout() // the pane is gone: a draft the prompt had clamped grows back (draftRowsCeiling)
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
		// A whole-input invocation IS the command line — nothing else was typed — so emptying the
		// box is exactly stripping the verb. runCommand does not touch the editor: its other caller,
		// the dropdown's accept path, cuts the token out and deliberately keeps the rest of the draft.
		m.promptEditor.reset()
		return m.runCommand(parsed)
	}
	if parsed.kind == kindUnknownSlash {
		return m.refuseUnknownSlash(parsed)
	}
	held := len(m.pendingInterjections) > 0
	// Nothing to send only when there is neither text NOR a held row. A message that is only a skill
	// token ("/grill-me") HAS text — the token itself — so it sends, which is the owner's edge
	// default: "just run the skill".
	if parsed.text == "" && !held {
		return m, nil
	}
	if m.actuation.inFlight {
		// A launcher verb owns the server this message would go to (ADR 0029 D5). Refuse it exactly
		// as an offline upstream is refused, and for the same reason: the typed line stays in the box
		// so ⏎ sends it the moment the actuation completes.
		m.transcript.addNote(m.actuationBlockNote())
		m.refreshViewport()
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
	// The first prompt of a fresh Session record also NAMES it: one cosmetic out-of-band completion
	// fired here, in parallel with the Exchange this prompt starts, so a single-slot server answers
	// it between Turns 1 and 2 (autotitle.go). It drives no engine and enters no transcript — it
	// simply rides along in the batch below, and is nil whenever nothing should fire.
	nameCmd := m.maybeAutoTitle(in.Text)
	m.layout() // the emptied input box shrinks back; the repaint follows the tail onto the new prompt
	next, cmd := m.launchExchange(in)
	return next, tea.Batch(cmd, nameCmd)
}

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
// (startNewSession), /version records the build version as a note the same synchronous way,
// /skills records the discovered skill catalog the same synchronous way (skills.go), and /confine
// reports or swaps Auto's blast radius the same synchronous way (confine.go).
//
// It is reached at stateIdle — where the engine is quiescent and ClearContext/Compact are safe to
// launch — OR, for a reporting line alone, while a worker runs. Its callers own that gate
// ([Model.commandRunnable]); by the time a verb arrives here it is either at a boundary or
// boundary-FREE. The three that can arrive mid-run are boundary-free by inspection: /version and
// /skills are synchronous notes touching no engine at all, and /confine's status form reads
// [Engine.ConfineToWorkspace], which the Agent serves under its own RWMutex precisely so the UI may
// ask while a Step dispatches (agent.go — the SetMode class). Everything else is idle-only and is
// refused before it gets here.
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
	// heartbeat exactly as a typed message does (blockedUpstream). The purely local verbs below —
	// /clear, /sessions, /version, /confine, /server — stay live while the server is away (moving to
	// another server is the one useful thing to do with an unreachable one); /model consults the
	// heartbeat itself, because "which models are served" is a question only a reachable server can
	// answer (modelSwitchBlocked owns that ladder).
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
		// message, and this turn's text is apogee's own "Please continue", not the human's line.
		// A draft the accept path left standing in the box is still a DRAFT — it carries its own
		// tokens when it is eventually sent, and nothing is silently borrowed from it here.
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
		return m.startServerActuation(verbUnload, m.opts.UnloadServer)

	case "stop-server":
		// Stop that server outright. Idle-only like its siblings and latched like a profile load — the
		// call blocks through the launcher's stop escalation — and afterwards the ordinary offline
		// crossing narrates the rest, because the downtime is real (actuation.go, ADR 0029).
		return m.startServerActuation(verbStop, m.opts.StopServer)

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
	m.restoreAskDraft() // the question has let go of the box: the message it interrupted comes back
	m.state = stateRunning
	m.setPlaceholder(runningPlaceholder) // the box is the human's own again — ⏎ queues from here
	m.layout()
	tick := m.spin.arm()
	return m, tick
}

// restoreAskDraft hands the input box back the message an ask_user question borrowed it from: the
// unsent draft the human was typing when the question arrived, which the askReqMsg fold stashed
// before emptying the box for the answer. It runs on BOTH ways the box stops being borrowed — the
// answer going out (submitAnswer) and the Exchange ending under the question (finishWorker) — so no
// route leaves the stash behind, and it is a no-op on every path where nothing was borrowed.
//
// Whatever is in the box when it runs is the ANSWER. On the answered path that is already gone
// (submitAnswer resets first). On the abandoned path it is a half-typed answer to a question that no
// longer exists — the human's own keystrokes just the same — so it is KEPT rather than clobbered:
// the draft goes back above it, one line apart, with the caret at the end. Neither half of what was
// typed is discarded on either path, which is the whole point of the stash.
//
// The caller re-lays the frame out: both of them do it a few statements later, for their own
// reasons, and the box has to regrow around the restored draft exactly once.
func (m *Model) restoreAskDraft() {
	draft := m.askDraft
	m.askDraft = ""
	if draft == "" {
		return
	}
	if answer := m.input.Value(); answer != "" {
		draft += "\n" + answer
	}
	m.input.SetValue(draft)
	m.input.MoveToEnd()
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
	// A question that dies with its Exchange — a stop, a fault — lets go of the box exactly as an
	// answered one does, so the message it borrowed the box from comes back here too.
	m.restoreAskDraft()
	// The Exchange is over, so its mailbox has no reader left: drop it, and with it any row the
	// worker had not drained by the time it unwound. Nothing is lost — the display queue
	// (pendingInterjections) is the queue of record and still holds every undelivered row. What
	// happens to those rows next is the CALLER's ruling, not this one's: a natural completion
	// flushes them into a new Exchange (flushAfterCompletion), a stop or a fault holds them for
	// the next ⏎ (noteHeldQueue) — ADR 0025.
	//
	// The rows this Exchange DELIVERED are forgotten here for the same reason: they are undoable
	// only while the Exchange that holds them can still be scrapped. A stop has already put them
	// back on the queue by this point (restageDelivered); past this boundary they are committed
	// history, and a later Exchange's stop must not resurrect them.
	m.box = nil
	m.deliveredInterjections = nil
	m.setPlaceholder(idlePlaceholder) // nothing is running: ⏎ sends again
	m.genStart = time.Time{}
	// The worker has unwound, so the activity is over — including a sticky "stopping", which
	// only this path clears (activity.go). Idle renders an empty left slot.
	m.act = activity{}
	m.state = next
	// The prompt cleared above may have been clamping a multi-line draft to leave itself its four
	// rows (draftRowsCeiling); with it gone the box grows back to what the draft asks for.
	m.layout()
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
// The clean quit's closing flush is DEFERRED too, for a second reason: it is scheduled on the
// record-write queue like every other save rather than written straight at the host. A synchronous
// Save beside an in-flight Rename reads the host's active session before that rename has mirrored
// the new title onto it, so the exit could write the pre-rename title back over the name the human
// just chose (audit 2026-08-01 follow-up). So a quit with anything on the queue arms quitting and
// exits from the fold that finds the queue drained (pumpOrQuit) — which is also the point at which
// the browser writes asked for on the way out have landed. With nothing to flush and an idle queue
// it still exits on the spot.
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
	cmd := m.saveAtIdle() // the closing flush joins the queue; nil when there is nothing worth saving
	if m.writeBusy || len(m.pendingWrites) > 0 {
		m.quitting = true
		return m, cmd
	}
	return m, tea.Quit
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

// persist builds a savePayload around sess and schedules it, gated on there being a wired host and
// a sent prompt (hasPrompt). It is the entry the per-Turn snapshot (turnSnapshotMsg) and the idle
// finishers both funnel through, so the "worth saving?" gate lives in one place. Those callers only
// run inside a Turn, which only a prompt opens, so the gate never fires here in practice — it is
// carried so that every save in the package answers to the same one predicate. It returns the Cmd
// to run (nil when nothing was scheduled).
func (m *Model) persist(sess domain.Session) tea.Cmd {
	if m.sessions == nil || !m.transcript.hasPrompt() {
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
// loop owns the engine again (C1). Three callers share it: the idle finisher (finishWorker), the
// clean quit's closing flush, and the /clear|/new close of the outgoing session. Best-effort like
// persist: a Snapshot error, an unwired host, or a transcript that holds no prompt yet simply
// schedules nothing — a launch that only ran slash commands has produced notes and chrome but
// nothing anyone would resume, so quitting it, or clearing it, must not file a record reading 0
// messages (hasPrompt).
//
// The two closing callers used to call the host synchronously instead, on the reasoning that the
// record must be on disk before the program exits or the host rotates. It bought that at the cost of
// a write outside the queue: a Save beside an in-flight Rename or Delete is exactly the collision the
// queue exists to prevent, and a rotate that overtakes its own flush mints a second id for the
// session it was closing. Both now schedule here and wait for the queue instead (quit,
// startNewSession).
func (m *Model) saveAtIdle() tea.Cmd {
	if m.sessions == nil || !m.transcript.hasPrompt() {
		return nil
	}
	sess, err := m.eng.Snapshot()
	if err != nil {
		return nil
	}
	return m.persist(sess)
}

// ----------------------------------------------------------------------------
// The session-record write queue
// ----------------------------------------------------------------------------
//
// One conversation writes its record three ways — the per-Turn/idle Save, the Rename that carries a
// title (generated or typed), and the browser's Delete — and each of them runs off the Update loop
// on its own Cmd goroutine. They MUST NOT overlap. Save replaces the record wholesale while Rename
// is a read-modify-write of it, so a Rename that read the record before a Save replaced it writes
// the pre-save version back, reverting the session by a whole Turn — engine state and scrollback
// together — and breaking ADR 0022's "a crash loses at most one Turn" (audit 2026-08-01, probe:
// 25% lost updates). Delete has the same shape against a save already in flight.
//
// So every write goes through ONE queue: scheduleWrite/queueWrite put it in, pumpWrites takes
// exactly one out whenever nothing is running, and every fold that finishes a write ends in
// pumpOrQuit. Two rules follow from that, and both were bugs before it existed: a fold must never
// tea.Batch two writes (batch members run on separate goroutines — this is precisely how the title
// flush came to race the coalesced save), and a write scheduled while the latch is held must WAIT
// rather than dispatch.
//
// Rotate and Activate join them although they write no file at all: they RETARGET the stream, moving
// the active session every later Save resolves against. A save that overtakes a Rotate is written
// under the OLD id and a save it overtakes mints a NEW one; a save that a resume's Activate overtakes
// is written into the RESUMED record, replacing the loaded conversation with the outgoing one.
// Ordering against the same stream is the whole of what either needs. The two closing flushes (quit,
// /clear) queue for the same reason rather than writing through, which is what leaves the exit
// waiting on the drain (pumpOrQuit).
//
// internal/session.Store holds a mutex over the file-writing calls, which is the floor under any
// caller that does not come through here. This layer's job is ordering; the store's is atomicity.
// Which of the two OWNS serialization long term is deliberately still open (C7).

// recordWriteKind names which SessionHost call a queued write makes.
type recordWriteKind int

const (
	writeSave     recordWriteKind = iota // SessionHost.Save — the per-Turn and idle snapshots
	writeRename                          // SessionHost.Rename — a generated title, /rename, the browser's `r`
	writeDelete                          // SessionHost.Delete — the browser's delete verb
	writeRotate                          // SessionHost.Rotate — /clear|/new, and deleting the ACTIVE record, retiring its id
	writeActivate                        // SessionHost.Activate — a /sessions resume adopting the loaded record's id
)

// retargets reports whether this kind moves the active session — which record subsequent Saves
// resolve against — rather than writing the current one. Saves on opposite sides of a retarget
// describe DIFFERENT records, which is why they must not coalesce across one (queueWrite).
func (k recordWriteKind) retargets() bool {
	return k == writeRotate || k == writeActivate
}

// recordWrite is one queued write to the session record: what to do, and what the fold that lands
// afterwards must do about it. Plain values only, so the value-copied Model carries it safely
// (ADR 0011).
type recordWrite struct {
	kind recordWriteKind

	// payload is the assembled save (writeSave only).
	payload savePayload

	// id addresses the record a rename or a delete acts on; title is the new name (writeRename).
	id    string
	title string

	// meta is the loaded record's browsable metadata a resume adopts (writeActivate only). A
	// session.Meta is plain values throughout, so the value-copied Model carries it safely.
	meta session.Meta

	// relist re-reads the store once the write lands, so the /sessions overlay repaints over the
	// result — what the browser's own verbs want, and what the QUIET title apply must not have
	// (foldSessionList opens the overlay over every list it folds, so a generated title would pop
	// the browser open partway through the first answer).
	relist bool

	// retryTitle marks a write whose title must not be lost when it fails: the quiet apply path
	// (setSessionTitle). source records who asked for that title, which the never-clobber rule
	// needs when the fold puts it back on the stash.
	retryTitle bool
	source     titleSource
}

// scheduleSave queues p as a record write and returns the Cmd to run — the save pipeline's entry to
// the shared queue. Returns nil when a write was already running (the save waits, coalescing with
// any save already queued: latest-wins).
func (m *Model) scheduleSave(p savePayload) tea.Cmd {
	return m.scheduleWrite(recordWrite{kind: writeSave, payload: p})
}

// scheduleWrite queues w and dispatches it when nothing else is writing, returning the Cmd to run
// (nil when it had to wait). It is the only way a record write is dispatched from an idle latch;
// folds that already hold the latch queue with queueWrite and end in pumpWrites instead.
func (m *Model) scheduleWrite(w recordWrite) tea.Cmd {
	m.queueWrite(w)
	return m.pumpWrites()
}

// queueWrite puts w in the pending queue, coalescing saves: a save already waiting is REPLACED by
// the newer one (latest-wins — the newer snapshot supersedes the older intermediate Turn), while
// renames, deletes and the two retargets each keep their place, being distinct instructions rather
// than restatements of one. Without a wired host there is no record to write, so nothing is queued.
//
// Coalescing stops at a retarget. A save queued BEFORE a Rotate or an Activate belongs to the
// session that was live then, and one queued after belongs to whatever the retarget made live — two
// different records, so the newer is no restatement of the older. Letting it supersede one across
// that line would write the incoming conversation into the outgoing record and lose the outgoing
// one's closing state entirely. Only a save in the queue's LAST segment can be superseded, which is
// the whole queue whenever no retarget is waiting — the ordinary case, unchanged.
//
// The queue is rebuilt rather than written through, so the Model copy this Update started from
// never sees the change (ADR 0011).
func (m *Model) queueWrite(w recordWrite) {
	if m.sessions == nil {
		return
	}
	supersede := -1
	if w.kind == writeSave {
		for i, q := range m.pendingWrites {
			switch {
			case q.kind.retargets():
				supersede = -1 // a new segment opens; nothing before it describes w's record
			case q.kind == writeSave:
				supersede = i
			}
		}
	}
	next := make([]recordWrite, 0, len(m.pendingWrites)+1)
	for i, q := range m.pendingWrites {
		if i == supersede {
			next = append(next, w)
			continue
		}
		next = append(next, q)
	}
	if supersede < 0 {
		next = append(next, w)
	}
	m.pendingWrites = next
}

// pumpWrites dispatches the head of the queue when no write is in flight, returning its Cmd (nil
// when one is already running or nothing is waiting). Every fold that finishes a write ends here,
// which is what keeps exactly one record write outstanding at a time.
func (m *Model) pumpWrites() tea.Cmd {
	if m.writeBusy || len(m.pendingWrites) == 0 {
		return nil
	}
	w := m.pendingWrites[0]
	m.pendingWrites = m.pendingWrites[1:]
	m.writeBusy = true
	return m.writeCmd(w)
}

// writeCmd builds the Cmd that performs one record write off the Update loop and reports it back —
// saveDoneMsg for a save (which carries the fail/recover notes and the title flush), otherwise
// recordWriteDoneMsg. It captures the SessionHost and the write by value, so the closure holds no
// pointer into the value-copied Model.
func (m Model) writeCmd(w recordWrite) tea.Cmd {
	sessions := m.sessions
	if w.kind == writeSave {
		p := w.payload
		return func() tea.Msg {
			return saveDoneMsg{Err: sessions.Save(p.sess, p.transcript, p.title, p.userMsgs, p.ctxUsed)}
		}
	}
	return func() tea.Msg {
		done := recordWriteDoneMsg{write: w}
		switch w.kind {
		case writeRename:
			done.err = sessions.Rename(w.id, w.title)
		case writeDelete:
			done.err = sessions.Delete(w.id)
		case writeRotate:
			sessions.Rotate() // reports nothing: closing a session cannot fail
		case writeActivate:
			sessions.Activate(w.meta) // reports nothing: adopting a loaded id cannot fail
		}
		if w.relist {
			// The re-list rides on the write's own goroutine, so the rows the browser repaints are
			// read AFTER the write that changed them and the fold that lands has everything it needs
			// in one Msg — leaving foldRecordWrite one Cmd to return rather than a tea.Batch, which
			// on this path would be two record writes on two goroutines again.
			metas, err := sessions.List()
			done.list = sessionListMsg{metas: metas, err: err}
		}
		return done
	}
}

// saveComplete folds a finished save: it notes the ok↔fail transition exactly once (on the ok→fail
// edge and the fail→ok recovery edge, then swallows the error — a save failure must never interrupt
// the conversation), queues any session title that was waiting for this record to exist, releases
// the single-flight latch, and dispatches whatever waited behind this save.
//
// The title flush hangs off a SUCCESSFUL save because that is the moment the record is first on
// disk with an id: the naming call (autotitle.go) routinely answers before the first save-complete,
// and Rename — the only writer of a stored title — needs a stored record to rewrite. It runs while
// the latch is still held so that it can only ever QUEUE: batching the title write beside the
// coalesced save is what let a rename roll a Turn off disk.
func (m *Model) saveComplete(err error) tea.Cmd {
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
	if err == nil {
		m.flushPendingTitle()
	}
	m.writeBusy = false
	return m.pumpOrQuit()
}

// foldRecordWrite folds a finished Rename, Delete, Rotate or Activate: it releases the single-flight
// latch, dispatches whatever waited behind it, and re-lists for the browser verbs that asked to
// repaint over the result. All of them are best-effort — a rename that did not stick leaves the old
// title on the re-list, a delete that did not leaves the row, and neither retarget can fail — so
// nothing is said about a failure.
//
// The one failure that is NOT simply swallowed is a quiet title write. Its apply path branches on
// ActiveID(), which the host mints at the START of the first Save, before the atomic write has put
// the file on disk; a title answering in that window — or any time saves have been failing — renames
// a record that is not there, and used to be discarded on the spot. Re-stashing it applies it at the
// next successful save instead (flushPendingTitle), so the window costs a delay rather than the name.
func (m *Model) foldRecordWrite(msg recordWriteDoneMsg) tea.Cmd {
	if msg.err != nil && msg.write.retryTitle {
		m.restashTitle(msg.write.title, msg.write.source)
	}
	if msg.write.relist {
		m.foldSessionList(msg.list) // repaint the overlay over the store as the write left it
	}
	m.writeBusy = false
	return m.pumpOrQuit()
}

// pumpOrQuit ends every fold that finished a record write: it dispatches the next write, and when
// the queue has run dry it fires the exit a clean quit was waiting for (quit). A quit deferred while
// a WORKER runs is deliberately NOT fired here — that one belongs to finishWorker, once the
// goroutine has unwound, because exiting first would let the composition root's teardown race a Step
// still in flight (C4). The busy() guard is what keeps the two apart, including when a human quits
// at idle and then sends one more message while the flush is still on its way to disk.
func (m *Model) pumpOrQuit() tea.Cmd {
	if cmd := m.pumpWrites(); cmd != nil {
		return cmd
	}
	if m.quitting && !m.busy() {
		return tea.Quit
	}
	return nil
}

// restashTitle puts a title that could not be written back on the stash, so the next successful save
// re-applies it rather than losing it. A stash already holding something wins — it is the newer
// instruction — and the never-clobber rule holds here as everywhere else: an AUTOMATIC title is
// dropped rather than re-stashed once a human has named the session.
func (m *Model) restashTitle(name string, src titleSource) {
	if m.pendingTitle != "" {
		return
	}
	if src == titleAutomatic && m.titleTouched {
		return
	}
	m.pendingTitle = name
	m.pendingSource = src
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
// serving, the window it reported for it, and whether the beat that saw it was the session's first
// contact. It is what the deferred apply stashes and what the [Options.Rebind] seam is called with
// — plain values, so the value-copied Model carries it safely. It is the OBSERVATION, not the
// binding: what the binary makes of it (a pinned window outranking the observed one) comes back in
// the [RebindResult].
type rebindIntent struct {
	model  string
	window int
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

// heartbeatLive reports whether gen belongs to the current tick chain of a WIRED monitor — the
// guard both heartbeat Msgs pass through. An unwired Model (gen 0) folds nothing, so a stray beat
// can never flip a TUI that monitors nothing into an offline state it can never leave.
func (m Model) heartbeatLive(gen int) bool {
	return m.opts.Heartbeat != nil && gen == m.hb.gen
}

// beatCmd runs one observation off the Update loop and reports it as a beatMsg stamped with the
// CURRENT generation. It captures the program context, so a shutdown cancels a beat still in
// flight, and the seam func by value — no pointer into the value-copied Model (the saveCmd
// posture). It returns nil when the monitor is unwired, which is what makes Init's tea.Batch
// collapse to the focus Cmd alone.
//
// It CONTINUES a chain and never opens one: Init issues the session's first beat on the generation
// newModel armed, and the tick fold issues the next one of the chain that scheduled the tick.
// Anything else firing an immediate beat wants [Model.armBeat], which retires the running chain
// first — one live chain per session is the invariant, and this func alone cannot keep it.
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

// foldBeat folds one landed observation into the heartbeat state and reports whether it changed
// what the view shows (so the caller repaints only when there is something new to see). Two things
// can move: the offline state, and — through [Model.observeBinding] — the bindings themselves.
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
	m.hb.models = beat.AvailableModels // the /model picker's rows are derived from it (picker.go)
	// A shorter offering must not leave an open picker highlighting a row that no longer exists.
	m.picker.clampSelection(m.pickerCount())
	crossed := m.hb.offline
	m.hb.offline = false

	m, rebound := m.observeBinding(beat, firstContact)
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
//
// firstContact is the pre-fold evidence its caller captured (see [Model.foldBeat]); it is stamped
// into the intent rather than re-derived here, so the deferred path words itself exactly like the
// immediate one — by construction, whenever the change is finally applied.
func (m Model) observeBinding(beat heartbeat.Beat, firstContact bool) (Model, bool) {
	if m.opts.Rebind == nil {
		return m, false // a display-frozen heartbeat: nothing to apply a change through
	}
	changed := beat.ActiveModel != m.hb.observedModel ||
		(beat.ContextWindow > 0 && beat.ContextWindow != m.hb.observedWindow)
	if !changed {
		return m, false
	}
	m.hb.observedModel, m.hb.observedWindow = beat.ActiveModel, beat.ContextWindow
	intent := rebindIntent{model: beat.ActiveModel, window: beat.ContextWindow, quietSeed: firstContact}
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

// foldServerSwitch folds a COMMITTED server switch (`/server`, picker.go) into the display. It is
// reached only after [Options.SwitchServer] has returned successfully, so the engine is already
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
func (m Model) foldServerSwitch(from string, result ServerSwitchResult) (tea.Model, tea.Cmd) {
	m.opts.Endpoint = result.Endpoint
	m.opts.HostAlias = result.HostAlias
	m.opts.ContextWindow = result.ContextWindow
	m.opts.Model = ""
	m.hb = heartbeatState{gen: m.hb.gen, switched: true}
	// The box's facts were frozen when it was seeded; restate it so the top of the scrollback names
	// the server this session is now on rather than the one it launched against (applyRebind's own
	// reason, one level up).
	m.transcript.refreshStartup(newStartupView(m.opts))
	m.transcript.addNote(serverSwitchNote(from, m.opts))
	m.layout()
	// The fresh generation IS the retirement of the old chain, so the first beat is armed rather than
	// merely issued — in a statement of its own, per [Model.armBeat].
	beat := m.armBeat()
	return m, beat
}

// serverSwitchNote words a committed switch: the server left, by the label the footer called it, and
// the one now on the wire — its alias with the endpoint spelled out beside it, because the alias is
// the human's own word for a URL and the switch is exactly the moment to show which URL it stands
// for. An aliasless server would name the endpoint twice, so it says it once.
func serverSwitchNote(from string, to Options) string {
	note := "switching server: " + from + " → " + hostDisplay(to)
	if hostDisplay(to) != to.Endpoint {
		note += " (" + to.Endpoint + ")"
	}
	return note
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
// the footer is ONE frameless content line — it has no divider above it and no bordered rule below
// it, because the prompt box closes its own frame; and the ▁ bottom-edge hairline is one row,
// closing the screen under the footer. The two hairlines are one role in two positions, which is
// why they are the same height under two names rather than one shared constant: what stands ABOVE
// the input box and what stands BELOW it are different questions, and the mouse mapping asks the
// second one (mouse.go). The input box and the viewport take what remains — the box grows with its
// content, the viewport gets the rest.
const (
	topRuleHeight    = 1 // the ▔ top-edge hairline that caps the bottom chrome, above the status line
	statusHeight     = 1
	gapHeight        = 1
	footerHeight     = 1 // one frameless content line, the status line's posture one row below the box
	bottomRuleHeight = 1 // the ▁ bottom-edge hairline that closes the screen under the footer

	// frameFixedRows is what every frame spends below the transcript whatever is open: the blank gap
	// row, both hairlines, the status line and the footer. The input box sits on top of that (its
	// content rows plus its own two border rows) and the viewport gets what is left (layout).
	frameFixedRows = gapHeight + topRuleHeight + statusHeight + footerHeight + bottomRuleHeight

	scrollbarWidth = 1 // the transcript's right-hand scroll-bar gutter (reserved while the bar is shown)

	minInputRows    = 1
	maxInputRows    = 10 // past this the textarea scrolls internally rather than growing further
	inputBorderRows = 2  // the box's own top AND bottom borders — the frame it closes itself
	borderFrame     = 2  // the input border's left + right columns
	inputPadding    = 2  // the input border's left + right padding columns

	// frameFloorRows is the shortest frame Apogee can compose: the blank gap row, the ▔ hairline,
	// the status line, the input box at its one-row floor plus its top and bottom borders, the
	// footer's single line, and the ▁ hairline under it — eight rows. Every one of them is something
	// layout.md forbids giving way — the box and the footer are the frame's floor, and the box keeps
	// one content row so what the human is typing is on the screen — so there is nothing left to shed
	// below this and the number is a FLOOR, not a budget. The transcript is already gone at it
	// (transcriptBudget reaches zero), which is what makes the arithmetic tight: at exactly this many
	// rows the frame is the chrome and nothing else.
	//
	// Below it the frame composes its floor and the terminal has fewer rows than that — the one
	// window size at which the frame does not fit, stated in layout.md rather than papered over with
	// a clip, because a clip would make the frame-height property trivially true at every size and
	// hide the next real overflow (docs/plans, FOLLOW-UP-K).
	frameFloorRows = frameFixedRows + inputBorderRows + minInputRows
)

// layout sizes the viewport and input box to the current window. The input box auto-grows with its
// content, clamped by its own taste and by what the frame can pay for (draftRowsCeiling), and the
// viewport gets the height left after the gap row, the ▔ top-edge hairline, the status row, the
// input box (its content rows plus its own top and bottom borders), the footer's single line and the
// ▁ bottom-edge hairline under it. A floor of one row keeps the WIDGET valid on a tiny window; the
// frame spends transcriptBudget instead, which does not have that floor and so cannot compose a row
// the window never paid for.
func (m *Model) layout() {
	// The scroll-bar gutter column is reserved only while the bar is shown (ui.show-scrollbar,
	// inverted into Options.HideScrollbar): a hidden bar gives the column back to the body rather
	// than eating it invisibly. The setting is fixed for the process lifetime, so this cannot
	// re-wrap the transcript mid-run — see bodyRightGutter (theme.go).
	width := m.width
	if !m.opts.HideScrollbar {
		width -= scrollbarWidth
	}
	m.viewport.SetWidth(max(1, width))
	m.input.SetWidth(m.inputInnerWidth())
	before := m.input.Height()
	m.input.SetHeight(m.inputRows())
	if m.input.Height() != before {
		m.reseatInput() // the box grew/shrank: re-clamp a scroll offset SetHeight left stale (ISSUES #2)
	}

	// The WIDGET floors at one row: a zero-height viewport is not a usable scroll surface. That floor
	// is a lie about the frame on a window too short to pay for it, so the frame spends
	// transcriptBudget instead — see there.
	m.viewport.SetHeight(max(1, m.transcriptBudget()))
	m.refreshViewport()
}

// inputBoxRows is the screen rows the input box occupies: its content rows and the two border rows
// of the rounded frame it closes itself (╭─╮ above, ╰─╯ below).
func (m Model) inputBoxRows() int {
	return m.input.Height() + inputBorderRows
}

// transcriptBudget is the FRAME's row budget for the transcript block: the window less the fixed
// chrome below it and the input box above it, floored at ZERO. Everything that spends the frame's
// rows — View, the mouse mapping ([Model.transcriptRows]) and the surface allocation
// ([Model.frameRowPlan]) — reads it, so the rows the transcript is drawn on and the rows the frame
// believes it has are one number.
//
// It is deliberately NOT m.viewport.Height(). layout() floors the WIDGET at one row because a
// zero-height viewport is not a scrollable surface, and on a window too short to pay for that row
// the frame composed it anyway — one row past the terminal's last line, with the footer off the
// alternate screen. The widget keeps its floor; the frame stops believing it.
func (m Model) transcriptBudget() int {
	return max(0, m.height-frameFixedRows-m.inputBoxRows())
}

// transcriptWidth is the column budget the transcript body wraps to: the viewport's own width
// less the right gutter (bodyRightGutter), floored at one column so a window too narrow to hold
// the gutter still wraps rather than going zero or negative. The viewport keeps its full width —
// only the content is narrower — so the mouse mapping still measures against the viewport, and
// the gutter shows up as unpainted columns.
func (m Model) transcriptWidth() int {
	return max(1, m.viewport.Width()-bodyRightGutter)
}

// inputInnerWidth is the textarea's text width: the window less the border and padding
// columns, floored at one so a very narrow window does not produce a zero-width box.
func (m *Model) inputInnerWidth() int {
	return max(1, m.width-borderFrame-inputPadding)
}

// inputRows is the textarea's height in visual rows at the current window width — the editor's
// own rows() clamp, fed the width the Model derives (inputInnerWidth), and then the FRAME's
// (draftRowsCeiling). The box grows as the human types a multi-line message and stops growing at
// the cap, where the textarea scrolls internally.
func (m *Model) inputRows() int {
	return min(m.promptEditor.rows(m.inputInnerWidth()), m.draftRowsCeiling())
}

// draftRowsCeiling is the tallest the input box may grow in THIS frame, and it is the one place the
// box gives way to anything. Two bounds meet here, and the tighter one wins.
//
// The FRAME's bound binds always: past it the transcript's row budget goes negative, layout()'s
// widget floor invents the row back, and the composed frame is one row taller than the terminal for
// every further row the draft grew — which is what an unbounded draft did on every window under
// eighteen rows, prompt or no prompt (FOLLOW-UP-K). The transcript may go to nothing for a draft —
// it is first in the give-way order — but nothing past it may.
//
// The PROMPT's bound binds while a DECISION SURFACE is up — the approval or the ask prompt, the
// pane the run itself is blocked on and the only pane whose keys are live in a state where the box
// is inert or borrowed — and leaves that pane its irreducible four rows. Both floor at the box's
// own one row.
//
// "The input box never gives way" (layout.md) is about the BOX, not about every row a draft grew it
// to: the box is always on the frame and always at least one row of it, it always keeps the row the
// caret is on (layout's reseatInput re-clamps the widget's scroll onto it), and the draft itself is
// never touched — the rows it cannot draw are counted out on the box's own top border
// ([Model.inputElisionEdge]), the way a pane counts out prose it cannot seat. What a three-line
// draft may not do is push the pane the human is being asked to rule on off the frame entirely —
// which is exactly what it did between 12 and 13 rows, where the frame's allocation found under
// four rows to give and drew no prompt at all, while a/d/s stayed live and the status line's
// "approval needed" was the whole of what the frame said about the decision.
//
// The prompt bound is for the modal prompt ALONE. The dropdown, the /sessions browser and the
// picker are all further down the give-way order than the prompt, and none of them can be open
// beside a draft the human is still growing: the browser and the picker claim every keypress while
// they are open (handleKey), and the dropdown is a completion of the very draft it would be
// shrinking. They still fit, because the frame bound above is not about panes at all.
func (m Model) draftRowsCeiling() int {
	ceiling := m.height - frameFixedRows - inputBorderRows
	if m.openPanes().has(panePrompt) {
		// The prompt needs popupChrome of the transcript's budget to be seated at all (frameRowPlan),
		// so the rows the draft may keep are what is left after it.
		ceiling = min(ceiling, m.height-frameFixedRows-popupChrome-inputBorderRows)
	}
	return max(minInputRows, ceiling)
}

// hiddenDraftRows is how many of the draft's wrapped rows the box is not drawing this frame: what
// the widget scrolls out of sight once the content outgrows the height the frame could pay for
// (draftRowsCeiling) or the box's own taste (maxInputRows). It measures the draft through the same
// mirror of the widget's wrap that sizes the box (inputContentRows), so the count is the box's own
// arithmetic rather than a second guess at it.
func (m Model) hiddenDraftRows() int {
	return max(0, inputContentRows(m.input.Value(), m.inputInnerWidth())-m.input.Height())
}

// refreshViewport re-renders the transcript into the viewport and, unless the human has
// scrolled away (detached), ends the repaint at the TRUE tail so generated output stays in view
// as it streams. Nothing is padded below the content: a new prompt is appended at the bottom of
// the history that was already there and the view follows it, rather than the transcript jumping
// so the prompt opens alone at the top of an emptied screen. A prompt reaches the top row only
// once the reply beneath it has grown a screenful, and from there applyStickyHeader overlays the
// owning prompt at row 0. While detached the offset is left exactly where the human put it;
// submit re-arms following. The body is rendered to transcriptWidth — the viewport's width less
// the right gutter — while the viewport soft-wraps the stored lines against its own full width.
//
// It is also where a live transcript drag-selection lives or dies, by the keep-if-unchanged rule
// (transcriptSel.spanUnchanged, mouse.go): the predicate is evaluated against the OUTGOING lines
// before the fresh ones replace them, so a selection over settled text survives the repaint a
// streamed token causes and one whose own lines were rewritten (the streaming tail, a rewrap) is
// dropped instead of left pointing at text that has moved.
func (m *Model) refreshViewport() {
	rendered := m.transcript.renderView(m.th, m.transcriptWidth(), m.spin.blink())
	if !m.transcriptSel.spanUnchanged(m.lines, rendered.lines) {
		m.transcriptSel = transcriptSel{} // the ground under the span moved: let go (mouse.go)
	}
	m.lines = rendered.lines // stashed for the sticky-header overlay (View)
	m.userBlocks = rendered.userBlocks
	m.lineTargets = rendered.targets // the paint's own click surface, for the mouse (render.go)
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
	m.viewport.SetContentLines(rendered.lines)
	m.viewport.GotoBottom() // the real tail: the content ends where the transcript ends
}

// refreshViewportAnchored re-renders the transcript and then holds ONE content line on the screen
// row it was already drawn on — the line a motionless click landed on (mouse.go). It is the
// anchoring half of the block-toggle rule: a block grows and shrinks BELOW its header, so pinning
// the clicked line's row is the whole of what keeps the line under the cursor still while the body
// appears or goes (layout.md, "Collapsed and expanded blocks").
//
// The repaint itself goes through refreshViewport, so the stash, the keep-if-unchanged rule and the
// short-content padding stay stated once; what this overrides is where that path PARKED the view.
// Its attached path ends at GotoBottom, which would yank the transcript to the tail on every
// toggle — the human clicked a header halfway up the scrollback, not the bottom of it. The offset
// is drawnLineAt's mapping read backwards (line = offset + row), so the clicked line lands back on
// its own row; SetYOffset clamps, so a collapse that took the content out from under the anchor
// settles at the bottom rather than scrolling into blank. That mapping's OTHER branch — the rows
// the sticky header is overlaid on — can never be the one a toggle arrived through: those rows draw
// a user block's lines, and a user block marks no click surface at all (render.go), so a resolved
// target's line is always the offset's own.
//
// detached is then re-derived from where the view actually ended up, exactly as a wheel scroll
// derives it (scrollViewport), which keeps "detached ⇔ off the bottom" total: an anchor holding the
// view off the tail IS the human looking away from it, and following the stream again would move
// the very line the toggle promised to keep still.
func (m *Model) refreshViewportAnchored(line, row int) {
	m.refreshViewport()
	m.viewport.SetYOffset(line - row)
	m.detached = !m.viewport.AtBottom()
}

// ----------------------------------------------------------------------------
// View
// ----------------------------------------------------------------------------

// frameOverlays holds the blocks View stacks between the transcript and the input box: the
// approval or ask prompt, the /sessions browser, the /model | /server picker, the autocomplete
// dropdown, and the staged-interjection strip. Each field is "" when its overlay is closed.
//
// They are gathered as ONE value because two readers need them and must never disagree: View
// composes them into the frame, and [Model.transcriptRows] measures them to say how many screen
// rows the transcript still owns — which is what the mouse maps a click through.
//
// How TALL each of them is drawn is settled before any of them is rendered, by the frame-wide row
// allocation they all share ([Model.frameRowPlan]): they are siblings spending the same viewport,
// not each the only thing in it.
type frameOverlays struct {
	prompt   string // the approval or the ask popup (they belong to different states, so never both)
	browser  string // the /sessions history browser
	picker   string // the /model | /server picker
	dropdown string // the command / @file / skill autocomplete
	queued   string // the staged-interjection strip (ADR 0025)
}

// height is the number of screen rows these overlays take from the transcript. An absent overlay
// is the empty string, which costs nothing — lipgloss.Height("") is 1, so the emptiness is tested
// rather than measured.
func (o frameOverlays) height() int {
	rows := 0
	for _, block := range []string{o.prompt, o.browser, o.picker, o.dropdown, o.queued} {
		if block != "" {
			rows += lipgloss.Height(block)
		}
	}
	return rows
}

// transcriptRows reports the rows the transcript keeps once these overlays have taken theirs out of
// the frame's budget ([Model.transcriptBudget]). The floor is ZERO, not one: on a window too short
// to hold both, the transcript goes away rather than the frame growing past the terminal's last
// row (D2).
func (o frameOverlays) transcriptRows(budget int) int {
	return max(0, budget-o.height())
}

// frameOverlays renders every overlay block of the frame as the Model stands. It is a pure function
// of the Model — nothing here mutates and nothing depends on the frame being composed — so View and
// the mouse mapping may each call it and are guaranteed the same answer.
func (m Model) frameOverlays() frameOverlays {
	var o frameOverlays
	if m.state == stateAwaitingApproval && m.pending != nil {
		o.prompt = m.approvalPrompt(m.pending.Request)
	}
	if m.state == stateAwaitingAsk && m.pendingAsk != nil {
		o.prompt = m.askPrompt(m.pendingAsk.Request)
	}
	o.browser = m.renderSessionBrowser()
	o.picker = m.renderPicker()
	o.dropdown = m.renderAutocomplete()
	o.queued = m.renderPendingInterjections()
	return o
}

// transcriptRows is THE derivation of how many screen rows the transcript occupies in the frame:
// the viewport's laid-out height less the rows this frame's overlays take from it. View composes
// the body at exactly this height, [Model.contentLineAt] refuses every row past it, and
// pointTranscriptRow (mouse.go) bounds a click by it — one number, three readers, so a click on an
// overlay row can no longer name the transcript line the overlay is drawn over.
//
// It stays honest only because View never writes the shrink back: the frame is composed from a
// LOCAL copy of the viewport, so the budget below is always the laid-out one and this subtraction
// can never compound with itself. The budget is transcriptBudget rather than the widget's own
// height, which layout() floors at one row the frame may not have (see there).
func (m Model) transcriptRows() int {
	return m.frameOverlays().transcriptRows(m.transcriptBudget())
}

// frameBlocks is the most blocks one frame stacks — the transcript, the five overlays, the blank
// gap row, the ▔ hairline, the status line, the input box, the footer, and the ▁ hairline — so the
// frame's slice is allocated once at its worst case.
const frameBlocks = 12

// View stacks the transcript, a single blank line, the ▔ top-edge hairline, the status line,
// the bordered input box, the footer line and the ▁ bottom-edge hairline, filling the alternate
// screen (layout.md). Before
// the first WindowSizeMsg there is no geometry to lay out, so it shows a minimal placeholder —
// on the alternate screen as well, so no frame apogee emits ever touches the primary one. The
// approval or ask prompt, when one is pending, sits between the transcript and the blank line, as
// do the /sessions browser and the picker; the autocomplete dropdown and the staged-interjection
// strip sit just above the input box. Every one of them takes its rows from the transcript
// (transcriptRows). It also places the frame's cursor: the real terminal one, at the prompt caret,
// wherever the box is editable (promptCursor).
func (m Model) View() tea.View {
	if !m.ready {
		// The alternate screen from the very FIRST frame, not just the laid-out ones: Bubble Tea v2
		// applies it as a per-frame view field (ADR 0011), so a placeholder that left it unset would
		// paint on the PRIMARY screen, push its lines into scrollback and keep the terminal's own
		// scrollbar alive for the whole run. MouseMode stays laid-out-frame-only — there is nothing
		// here to click.
		v := tea.NewView("apogee — starting…")
		v.AltScreen = true
		return v
	}

	// Rendered once, then measured by the same derivation the mouse maps through, so what is drawn
	// over the transcript and what a click may address are the same rows by construction.
	ov := m.frameOverlays()

	rows := make([]string, 0, frameBlocks)
	// Draw the transcript at the height the overlays leave it, with its sticky header, then hang the
	// scroll-bar gutter off its right edge — the bar is sized from the same local viewport, so the
	// two columns line up row-for-row. With the bar switched off (ui.show-scrollbar) the body goes
	// on alone, into the full window width layout() then gave it.
	// The height goes on that LOCAL copy and is never written back:
	// m.viewport keeps its laid-out height, which is what lets contentLineAt (via transcriptRows)
	// measure this very shrink instead of compounding it. At zero rows the transcript is off the
	// frame entirely — the overlays took the whole budget, a draft grew the box into it, or the
	// window is too short to pay for a transcript row at all (transcriptBudget).
	if h := ov.transcriptRows(m.transcriptBudget()); h > 0 {
		vp := m.viewport
		vp.SetHeight(h)
		body := m.applyStickyHeader(vp.View())
		body = m.highlightTranscript(body) // overlay any transcript drag-selection on the composed rows
		if m.opts.HideScrollbar {
			rows = append(rows, body)
		} else {
			rows = append(rows, m.joinScrollbar(body, m.renderScrollbar(vp)))
		}
	}
	if ov.prompt != "" {
		rows = append(rows, ov.prompt)
	}
	// The /sessions browser and the /model | /server picker share the approval/ask prompt's slot
	// (none of them co-occur — both overlays are idle-only and modal, the prompts belong to busy
	// states).
	if ov.browser != "" {
		rows = append(rows, ov.browser)
	}
	if ov.picker != "" {
		rows = append(rows, ov.picker)
	}
	// The single blank line between chat content and the bottom chrome (layout.md), then the
	// ▔ top-edge hairline capping the chrome, the status line, the autocomplete overlay (when
	// open), the input box, the footer, and the ▁ hairline closing the screen under it.
	rows = append(rows, "", m.topRule(), m.statusLine())
	if ov.dropdown != "" {
		rows = append(rows, ov.dropdown)
	}
	// The staged interjection rows sit closest to the box: they are what ⏎ just put there, and
	// what Backspace on an empty box takes back (ADR 0025).
	if ov.queued != "" {
		rows = append(rows, ov.queued)
	}
	rows = append(rows, m.inputView(), m.footerView(), m.bottomRule())

	v := tea.NewView(m.joinFrame(rows))
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

// contentLineAt maps a viewport row to the index into m.lines of the content ACTUALLY DRAWN there,
// or −1 when the row draws no transcript content at all. Off the header zone the mapping is just
// the scroll offset plus the row; within it, it is the overlaid header line — the sticky header
// covers the content the offset would otherwise put there, so addressing that content by row would
// name text the human cannot see. Selection (the mouse maps a click through here) and the selection
// highlight both go through this one mapping, which is what keeps copy equal to sight on the header
// rows (mouse.go).
//
// The refusal is the OVERLAY half of the same rule: an approval popup, the /sessions browser or the
// picker takes its rows off the bottom of the transcript (transcriptRows), and those rows draw the
// overlay, not the reply lines the un-shrunk viewport would put there. Returning a line index for
// them is what let a click on a popup border arm a selection over hidden text and copy it.
func (m Model) contentLineAt(row int) int {
	if row < 0 || row >= m.transcriptRows() {
		return -1
	}
	return m.drawnLineAt(row)
}

// drawnLineAt is contentLineAt's mapping half, for a row already established to be the
// transcript's own. It exists so a caller drawing INSIDE the transcript block — the selection
// highlight, walking rows it composed at exactly [Model.transcriptRows] — can bound itself once
// instead of re-deriving the frame's row budget on every row of every frame. There is still one
// mapping and one bound; only where the bound is spent differs.
func (m Model) drawnLineAt(row int) int {
	if start, count := m.stickyHeaderSpan(); row < count {
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
// while there is something to scroll. Thumb and track are the same axis in two weights
// (glyphScrollThumb / glyphScrollTrack, theme.go), so the column reads as one line that thickens
// where the view sits. The returned block is exactly vp.Height() rows tall so it lines up with the
// viewport view it is joined to.
//
// It takes the viewport it is drawing FOR rather than reading the Model's, because the frame is
// composed at the height the overlays leave (transcriptRows) on a local copy: sizing the thumb
// against the stored, un-shrunk height would draw a bar for a taller transcript than the one on
// screen.
func (m Model) renderScrollbar(vp viewport.Model) string {
	h := vp.Height()
	if h < 1 {
		return ""
	}
	total := vp.TotalLineCount()
	rows := make([]string, h)
	if total <= h { // nothing to scroll — keep the gutter blank
		for i := range rows {
			rows[i] = " "
		}
		return strings.Join(rows, "\n")
	}
	thumb := max(1, h*h/total)
	pos := int(vp.ScrollPercent() * float64(h-thumb)) // 0 (top) … h-thumb (bottom)
	for i := range rows {
		if i >= pos && i < pos+thumb {
			rows[i] = m.th.scrollThumb.Render(glyphScrollThumb)
		} else {
			rows[i] = m.th.scrollTrack.Render(glyphScrollTrack)
		}
	}
	return strings.Join(rows, "\n")
}

// squareLine makes one composed line occupy exactly w columns WHEN PAINTED: short lines are padded
// with blanks, and a line that would run past w is cut (ANSI-aware, no ellipsis — a cut here means
// something upstream broke the cap, and adding a … would only spend another column).
//
// It measures with the width authority (width.go) rather than lipgloss.Width, and that is the whole
// reason it exists. Every squaring primitive the charm stack offers — lipgloss.JoinVertical and
// JoinHorizontal, Style.Width, the viewport's own Width() padding
// (bubbles/v2@v2.1.0/viewport/viewport.go:743-746) — pads to a width measured in ansi.GraphemeWidth,
// which is not the measure the terminal paints in unless it answered mode 2027. A row carrying a
// VS16 grapheme is padded one cell short of where it paints, and everything the layout hangs off
// that row's right edge moves with it.
func squareLine(measure widthAuthority, s string, w int) string {
	over := measure.Width(s) - w
	if over > 0 {
		return measure.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", -over)
}

// joinScrollbar hangs the scroll-bar gutter off the right edge of the transcript body: every body
// row is squared to the viewport's width first (squareLine), then its bar cell is appended.
//
// It stands in for the lipgloss.JoinHorizontal this used to be. JoinHorizontal pads each block's
// rows to that block's widest row in GraphemeWidth, so on a terminal painting in WcWidth the ⚠️ row
// reached the gutter a column short and dropped its bar cell one column left of every other row's.
// Doing the padding here, in the painter's measure, is what makes the bar a straight column.
func (m Model) joinScrollbar(body, bar string) string {
	if bar == "" {
		return body
	}
	rows := strings.Split(body, "\n")
	barRows := strings.Split(bar, "\n")
	if n := len(barRows); n > len(rows) {
		rows = append(rows, make([]string, n-len(rows))...) // a bar taller than the body keeps its cells
	}
	w := max(0, m.viewport.Width())
	for i, row := range rows {
		row = squareLine(m.th.measure, row, w)
		if i < len(barRows) {
			row += barRows[i]
		}
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

// joinFrame stacks the frame's blocks into the one string the View hands bubbletea, squaring every
// physical line to the window width in the painter's measure.
//
// It stands in for the lipgloss.JoinVertical this used to be, for the same reason joinScrollbar
// stands in for JoinHorizontal: JoinVertical left-aligns by padding every row out to the widest row
// it was given, measured in GraphemeWidth. That is the window width right up until one block
// measures wider than the window in that measure — which is exactly what a frame squared for a
// WcWidth painter does — and then the pad it applies to EVERY OTHER row pushes the whole frame past
// the terminal's last column. Squaring to m.width instead makes layout.md's absolute width cap hold
// at the layer the terminal actually reads, whichever measure that is.
func (m Model) joinFrame(blocks []string) string {
	lines := make([]string, 0, len(blocks))
	for _, block := range blocks {
		for _, ln := range strings.Split(block, "\n") {
			lines = append(lines, squareLine(m.th.measure, ln, m.width))
		}
	}
	return strings.Join(lines, "\n")
}

// inputView renders the textarea inside the rounded, dark-gray, black-bg border, a CLOSED frame:
// the box draws its own ╰─╯ bottom edge, and the footer below it is a frameless line rather than
// the bottom half of the box's chrome. The style's Width sets the box's total width including the
// border and padding, so the box always spans the window and the footer below it aligns. The
// ▔ top-edge hairline that caps the bottom chrome is a separate row above the status line
// (topRule), not part of this box, so the status line reads as sitting directly above the box.
//
// Two overlays compose onto the widget's own block, and the ORDER is the contract: accentTokens
// paints the resolving /skill and @file tokens first (inputaccent.go), then highlightInput paints
// the drag-selection over whatever it covers. shadeCells strips the span it re-renders, so the last
// pass wins on any overlap — and a selection must read as selected even across an accented token.
//
// A third pass writes the elision marker onto the box's own TOP BORDER when the frame could not pay
// for every row the draft wraps to (inputElisionEdge). It is last because it addresses the BORDER
// row, which the two content overlays never touch.
func (m Model) inputView() string {
	box := m.th.inputBorder.Width(m.width).Render(m.highlightInput(m.accentTokens(m.input.View())))
	edge := m.inputElisionEdge(m.hiddenDraftRows())
	if edge == "" {
		return box
	}
	lines := strings.Split(box, "\n")
	lines[0] = edge // the rounded border's top row; the widget's content starts at lines[1]
	return strings.Join(lines, "\n")
}

// inputEdgeChrome is what the marker's row spends besides the marker itself: the two rounded
// corners, the ─ segment leading into it, and the space either side of it (╭─ … +3 ────╮).
const inputEdgeChrome = 5

// inputElisionEdge composes the input box's top border row carrying the count of draft rows the box
// is not drawing, or "" when there are none to count (or no width to count them in) and the box
// keeps its plain border.
//
// HIDING IS NEVER SILENT, and a draft is the case where that matters most: it is what the HUMAN
// typed, not prose a tool authored, so the box may window it but may never let rows of it go missing
// without saying so. The box has no title row to put the fact on, so it goes on the one row the box
// always owns and that carries nothing else — its top border, which is the box's title row in every
// way that counts. The wording is popupElisionMarkerFitting's, unchanged: one phrase for "there is
// content here you cannot see", shedding its noun before its number exactly as a pane's does, so a
// windowed draft and a shrunken pane state the same fact in the same words.
//
// Under the width even the short form needs, the row stays plain. That is the one place this differs
// from a pane's title, and deliberately: a pane sheds its NAME to keep the number, while this row
// has no name to shed — clipping the marker itself would leave "… +1" of "… +12", which is not a
// quieter statement of the count but a WRONG one.
func (m Model) inputElisionEdge(hidden int) string {
	if hidden <= 0 {
		return ""
	}
	budget := m.width - inputEdgeChrome
	marker := popupElisionMarkerFitting(hidden, budget)
	fill := budget - m.th.measure.Width(marker)
	if fill < 0 {
		return ""
	}
	border := lipgloss.RoundedBorder()
	return m.th.chromeRule.Render(border.TopLeft+border.Top+" ") +
		m.th.statusBar.Render(marker) +
		m.th.chromeRule.Render(" "+strings.Repeat(border.Top, fill)+border.TopRight)
}

// topRule renders the full-width ▔ hairline that marks the top edge of the bottom chrome: a
// dim-gray hairline on a black field (hairline, dimmer than the prompt box's own chromeRule
// border so it recedes). It sits directly below the transcript's one blank gap row and above the
// status line (layout.md), capping the whole bottom section while the status line stays directly
// above the input box.
//
// The row also CARRIES the session's name, centered, in place of the rule runes it would otherwise
// draw there (sessionrule.go, which owns the whole arithmetic). That is an IDENTITY and not a gauge:
// a screen full of panes says which conversation each one is, and nothing that changes every frame
// belongs here — everything live already has a home in the chrome below. An unnamed session gets the
// plain unbroken rule this function used to be, which is also what /clear returns the row to.
//
// The two styles are why the pieces are rendered separately rather than composed first: the ▔ runs
// keep the recessive hairline role, while the label — the name and its two spaces — takes statusBar,
// so it reads as a label sitting ON the rule rather than as more rule, and the black field stays
// unbroken across the seam.
func (m Model) topRule() string {
	lead, label, trail := sessionRuleLayout(m.th.measure, m.sessionRuleName(), m.width)
	if label == "" {
		return m.th.hairline.Render(lead)
	}
	return m.th.hairline.Render(lead) + m.th.statusBar.Render(label) + m.th.hairline.Render(trail)
}

// bottomRule renders the full-width ▁ hairline that closes the screen under the footer: the top
// rule INVERTED — ▔ is the upper one-eighth block and ▁ the lower one — in the same recessive
// dim-gray-on-black role, so the bottom chrome is bracketed by one hairline above the status line
// and its mirror below the footer (layout.md).
//
// It is what the footer's ╰──╯ rule left behind. Losing the footer's frame put the workdir line
// flush against the terminal's last row with nothing under it, which read as unfinished rather
// than as light; a hairline closes the screen without re-boxing the footer, because a rule is not
// a border — it belongs to the chrome as a whole, not to the one row above it.
func (m Model) bottomRule() string {
	return m.th.hairline.Render(strings.Repeat("▁", m.width))
}

// footerView renders the footer: ONE frameless line below the prompt box. It used to be three
// rows — a ├──┤ divider standing in for the box's missing bottom edge, the content line between
// two │ bars, and a ╰──╯ rule closing the whole unit — and the box now closes its own frame
// instead, so the divider has nothing to join and the bottom rule nothing to close (layout.md).
// What is left is the content line itself, which is why this is a one-line function rather than
// a composition: the row IS the footer.
func (m Model) footerView() string {
	return m.footerContent(m.width)
}

// footerContent composes the footer's single line: host ✦ model ✦ workdir on the left, the mode
// marker — its symbol and its word (modeMarker) — on the right, over one unbroken black field. It
// takes the STATUS LINE's posture, one row below the box rather than one row above it — a
// bodyIndent lead, the black field filled to the full window width, and the mode marker ending
// bodyIndent short of the window edge, the very column the gauge above it ends in (layout.md,
// "The status line's right slot"). The mode marker takes its own per-mode colour (modeColor), so
// the segments are styled independently and laid out by hand — mirroring statusLine — rather than
// rendered under one style, which would let the mode's colour reset bleed the black field. The host falls back to the endpoint when no alias is
// configured, and every segment nothing has named is dropped with its separator (nonEmpty).
//
// The workdir closes the run rather than opening it because the line reads outward-in — the server
// this session talks to, the model it talks to there, and last the local directory it is pointed
// at, the one fact of the three that no upstream state can change.
//
// A window too narrow to hold both ends keeps today's shape: the left info truncates with an
// ellipsis and the mode marker drops WHOLE, because a clipped mode word would name a blast radius
// the session is not in.
func (m Model) footerContent(w int) string {
	segments := append([]string{hostDisplay(m.opts)}, m.upstreamSegments()...)
	info := strings.Join(nonEmpty(append(segments, m.workdir)...), " "+glyphAssistant+" ")
	offline := ""
	if m.hb.offline {
		offline = " " + glyphAssistant + " " + offlineLabel
	}
	// footerText keeps the black background; only the foreground swaps to the mode's colour. Symbol
	// and word go through that ONE Render, so the glyph can never take a tone of its own. The
	// trailing bodyIndent is the slot's own margin, not the marker's — the same seam statusLine
	// appends its right slot's margin at.
	mode := m.th.footerText.Foreground(modeColor(m.opts.Mode)).Render(modeMarker(m.opts.Mode)) +
		m.th.footerText.Render(bodyIndent)

	gap := w - m.th.measure.Width(bodyIndent) - m.th.measure.Width(info) -
		m.th.measure.Width(offline) - m.th.measure.Width(mode)
	if gap < 1 {
		// Too narrow for both ends: keep the left info, truncate to the window, pad the field black.
		body := m.th.measure.Truncate(bodyIndent+info+offline, max(0, w), "…")
		body += strings.Repeat(" ", max(0, w-m.th.measure.Width(body)))
		return m.th.footerText.Render(body)
	}
	left := m.th.footerText.Render(bodyIndent + info)
	if offline != "" {
		// Styled independently, like the mode marker: the segment carries the error tone on the
		// footer's own black field, so the state reads at a glance without recolouring the line.
		left += m.th.footerText.Foreground(colError).Render(offline)
	}
	fill := m.th.footerText.Render(strings.Repeat(" ", gap))
	return left + fill + mode
}

// The two words the footer says about the Upstream that are not facts about a model.
const (
	// connectingLabel stands where the model goes while a wired heartbeat has not bound one yet —
	// the honest word for the seconds between the first paint and the first landed beat (startup
	// discovery is now that beat).
	connectingLabel = "connecting…"
	// offlineLabel is the footer's own word for the state a send is refused in.
	offlineLabel = "offline"
)

// upstreamSegments is the footer's upstream fact: the display model, or the single word
// "connecting…" while a wired heartbeat has not bound one yet. The context window used to travel
// beside it and no longer does — it is stated live by the status line's gauge, which measures it
// against what the conversation has actually spent — so a stand-in word now replaces the model
// alone and there is no second word to pair it with.
//
// An actuation in flight outranks both (ADR 0029 D6): "loading <profile>…" is the more specific
// truth than "connecting…", and while a model is still bound it is the honest replacement for a
// binding the launcher is in the middle of invalidating.
//
// It stays a slice because the caller joins it into a run of ✦-separated segments and a stand-in
// word may yet be more than one; the callers never index it.
func (m Model) upstreamSegments() []string {
	if label := m.actuationLabel(); label != "" {
		return []string{label}
	}
	if m.opts.Model == "" && m.opts.Heartbeat != nil {
		return []string{connectingLabel}
	}
	return []string{displayModel(m.opts.Model)}
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

// modeSymbol maps an autonomy mode to the glyph that leads its footer marker (the glyphs in
// theme.go): ⬡ plan, ◐ ask before, ✔ allow edits, ▸▸ auto. An unknown mode has no glyph — an
// off-ladder value states its word alone rather than borrowing another rung's shape.
//
// It is display-only and deliberately separate from modeLabel: the label is also read aloud in
// sentences (the confinement overlay's "the current mode is ask before"), where a glyph would be
// noise. Only the footer's marker leads with the symbol.
func modeSymbol(m domain.Mode) string {
	switch m {
	case domain.ModePlan:
		return glyphModePlan
	case domain.ModeAskBefore:
		return glyphModeAskBefore
	case domain.ModeAllowEdits:
		return glyphModeAllowEdits
	case domain.ModeAuto:
		return glyphModeAuto
	default:
		return ""
	}
}

// modeMarker is the footer's mode marker as one string — the mode's symbol, a space, and its
// friendly label ("◐ ask before"). It is composed here rather than at the call site so the two
// halves are rendered under a SINGLE style: the glyph carries the mode's colour because it is
// part of the same styled run as the word, not because it was coloured to match (footerContent).
// A mode with no symbol (off the ladder) renders its label alone, with no leading space.
func modeMarker(m domain.Mode) string {
	if sym := modeSymbol(m); sym != "" {
		return sym + " " + modeLabel(m)
	}
	return modeLabel(m)
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
// body text above it rather than with the ✦/❯ marker column (layout.md). The right slot mirrors
// that lead: whatever occupies it — gauge, key hint, copy flash, primed Ctrl+C — ends bodyIndent
// short of the window edge, the column the footer's mode marker below it ends in, so nothing in
// the slot ever touches the terminal's last column. It reads only display values off Options and
// the model's own state — never off the Engine mid-step.
//
// The LEFT slot is composed to the window's width rather than composed long and clipped to it
// (statusLeft) — the popupTitleLine posture, one row down.
func (m Model) statusLine() string {
	left := m.statusLeft()
	// Fill the whole width with black-bg cells — segments, the justify gap and the right slot's
	// trailing margin alike — so the info line reads as one solid black bar joined to the prompt
	// box below it. A plain justify gap (or a bare-space margin) would show the terminal's default
	// background through the seam. statusRight returns a fully pre-styled string (the gauge carries
	// its own per-cell backgrounds), so it is concatenated raw rather than re-wrapped, which would
	// clobber those backgrounds; the margin is styled separately and appended for the same reason.
	// An empty slot gets no margin: the justify gap already paints black to the last column, so two
	// orphan cells would move nothing. The gap arithmetic below reads the widened right slot, so
	// the too-narrow drop simply happens two columns earlier — the margin's honest price.
	right := m.statusRight()
	if right != "" {
		right += m.th.statusBar.Render(bodyIndent)
	}
	gap := m.width - m.th.measure.Width(left) - m.th.measure.Width(right)
	if gap < 1 {
		return m.th.measure.Truncate(left, max(0, m.width), "")
	}
	return left + m.th.statusBar.Render(strings.Repeat(" ", gap)) + right
}

// statusLeft composes the status line's left slot to the window's WIDTH: the two-column body lead,
// the state's own words, and then the "N queued" readout of what is waiting to go out — in every
// state, because a queue that survives a stop or an error (it does) must keep saying so at idle
// too, where the slot is otherwise empty.
//
// The width is spent in the order the slot is READ for, exactly as a pane's title row spends its
// own (popupTitleLine, layout.md): the count is the last thing the slot gives up, and the state's
// phrase is the thing trimmed around it. That is not a nicety — the band the count belongs to is
// the first surface the frame's row allocation drops, and on the short windows where it is gone the
// status line is the ONLY place the frame says anything about a queue at all. Composing the slot
// long and clipping it to the window put the count on the end of the clip, so at 20 columns a
// running turn's phrase pushed it off the row and the queue vanished from the frame entirely.
//
// Below two columns of room the phrase would be an ellipsis and nothing else, so it is dropped
// whole and its separator with it — the slot never reads " · 2 queued" with no phrase in front of
// the separator.
func (m Model) statusLeft() string {
	lead := m.th.statusBar.Render(bodyIndent)

	var phrase string
	switch m.state {
	case stateRunning:
		phrase = m.spin.view(m.th) + m.th.statusBar.Render(" "+m.runningPhrase(time.Now())) + m.throughputSuffix()
	case stateAwaitingApproval:
		phrase = m.th.statusBar.Render("approval needed")
	case stateAwaitingAsk:
		phrase = m.th.statusBar.Render("answer needed")
	case stateErrored:
		phrase = m.th.statusError.Render("error")
	}
	if phrase == "" {
		return lead + m.queuedSegment(false)
	}

	queued := m.queuedSegment(true)
	room := m.width - m.th.measure.Width(lead) - m.th.measure.Width(queued)
	if m.th.measure.Width(phrase) <= room {
		return lead + phrase + queued
	}
	if room <= 1 {
		return lead + m.queuedSegment(false)
	}
	return lead + m.th.measure.Truncate(phrase, room, "…") + queued
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
// before any usage is measured and the gauge takes the slot the moment it is. Every branch
// returns its occupant flush — statusLine appends the trailing margin at one seam, so the whole
// slot moves together and no branch has to remember an inset of its own.
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

// view renders the gauge as "<used>/<limit> <pct>% <bar>", or "" when usage is unknown. The
// gauge names the window it is measured against — the fill only means something beside the
// limit it fills — so the window is a fact this row states rather than one read off elsewhere.
// The numeric prefix is faint-on-black status text; the bar is a solid two-tone strip
// (renderGaugeBar) carrying its own per-cell backgrounds, so the whole string is pre-styled and
// must be concatenated raw by the caller (never re-wrapped in a background style).
//
// The percentage is clamped to 100 because the bar already is (renderGaugeBar): a conversation
// carried across a switch to a smaller window overfills its new limit, and a full bar labelled
// "137%" is a rendering bug, not a reading. The unclamped Used still shows beside it, so an
// over-window fill is visible as the token count rather than as an impossible percentage — and
// with the limit spelled out it reads as the plain contradiction it is: "137k/98k 100%".
func (c contextUsage) view(th theme) string {
	if c.Used <= 0 || c.Limit <= 0 {
		return ""
	}
	pct := min(c.Used*100/c.Limit, 100)
	prefix := th.statusBar.Render(fmt.Sprintf("%s/%s %d%% ", formatTokens(c.Used), formatTokens(c.Limit), pct))
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

// ----------------------------------------------------------------------------
// The frame's row allocation (layout.md, "What 'height' means")
// ----------------------------------------------------------------------------

// transcriptReserve is the transcript's SOFT claim on the frame's rows: rows kept for the
// conversation wherever the window can spare them. It is the first thing spent — the transcript
// gives way before any other surface does — so an open pane's floor and the staged band are both
// taken out of it first.
const transcriptReserve = 3

// framePane identifies one boxed overlay in the frame's row allocation. The order of the constants
// is the order the panes GIVE WAY in, last first: on a window that cannot seat every open pane the
// dropdown loses its rows before the picker or the browser, and the modal approval or ask prompt —
// the one the run is blocked on — keeps its rows last of all.
type framePane int

const (
	panePrompt   framePane = iota // the approval or the ask prompt
	paneBrowser                   // the /sessions history browser
	panePicker                    // the /model | /server picker
	paneDropdown                  // the command / @file / skill autocomplete
	paneKinds                     // not a pane: the count, so a plan can hold one grant per kind
)

// framePaneSet is the set of boxed overlays open in one frame — the siblings an allocation has to
// divide the window between.
type framePaneSet uint8

// with returns the set including p.
func (s framePaneSet) with(p framePane) framePaneSet { return s | 1<<p }

// has reports whether p is in the set.
func (s framePaneSet) has(p framePane) bool { return s&(1<<p) != 0 }

// openPanes is the set of boxed overlays this Model has open. Each member's predicate is the same
// one its renderer returns "" on, so the allocation is divided between exactly the panes that will
// be drawn.
func (m Model) openPanes() framePaneSet {
	var s framePaneSet
	if (m.state == stateAwaitingApproval && m.pending != nil) ||
		(m.state == stateAwaitingAsk && m.pendingAsk != nil) {
		s = s.with(panePrompt)
	}
	if m.sessionBrowser.open {
		s = s.with(paneBrowser)
	}
	if m.picker.open {
		s = s.with(panePicker)
	}
	if m.autocomplete.active && len(m.autocomplete.items) > 0 {
		s = s.with(paneDropdown)
	}
	return s
}

// frameRowPlan is the ONE row allocation every surface stacked above the input box shares: the
// staged band and each open boxed overlay get their rows out of the same viewport, so the composed
// frame fits the terminal whatever COMBINATION is open. Sizing each surface against the whole
// viewport instead was right one surface at a time and wrong the moment two were open — a dropdown
// beside a staged queue composed a 16-row frame on a 12-row terminal, pushing the input box and the
// footer off the alternate screen.
type frameRowPlan struct {
	band  bandPlan
	panes [paneKinds]int // rows granted to each pane, its own chrome included; 0 = not drawn
}

// frameRowPlan divides the viewport's rows between the frame's surfaces, in the order they give
// way (layout.md): the transcript first, then the staged band, then the panes — and among the
// panes, the dropdown before the browser or the picker, and the modal prompt last. The input box
// and the footer are not in the division at all; layout() has already taken their rows out — and
// what it took for the box is itself capped when a modal prompt is up, which is the one place the
// box gives way and the last step of the same order (draftRowsCeiling).
//
// The order of the passes is the order of the guarantees. Every seated pane's irreducible chrome
// comes off first, so a pane the human is acting on is never squeezed out by a passive band; the
// band then takes what is left, up to what it wants, which is what makes it cost the TRANSCRIPT
// rather than the pane beside it; the transcript keeps its soft reserve out of the remainder; and
// only the surplus past all three grows the seated panes, split evenly when more than one is open.
//
// transcriptBudget is the laid-out budget and always will be: View composes the frame from a LOCAL
// copy of the viewport and never writes the shrink back ([Model.transcriptRows]). It is the frame's
// budget rather than the widget's height, so a window too short to pay for the viewport's one-row
// floor allocates ZERO rows here instead of handing a surface a row the frame has not got.
func (m Model) frameRowPlan(open framePaneSet) frameRowPlan {
	var plan frameRowPlan
	left := m.transcriptBudget()

	seated := 0
	for p := framePane(0); p < paneKinds; p++ {
		if !open.has(p) {
			continue
		}
		if left < popupChrome {
			break // no room for another honest pane: the rest of the order gives way entirely
		}
		plan.panes[p] = popupChrome
		left -= popupChrome
		seated++
	}

	plan.band = bandShape(len(m.pendingInterjections), left)
	left -= plan.band.height()

	if growth := max(0, left-transcriptReserve); growth > 0 && seated > 0 {
		share, extra := growth/seated, growth%seated
		for p := framePane(0); p < paneKinds; p++ {
			if plan.panes[p] == 0 {
				continue
			}
			plan.panes[p] += share
			if extra > 0 {
				plan.panes[p]++
				extra--
			}
		}
	}
	return plan
}

// popupBudget derives the screen-budget caps overlay p hands renderPopup so the bordered pane never
// pushes the input box off-screen (D2). rows is how many selectable rows are on offer and rowCap
// the overlay's own cap on how many of them it shows at once (both 0 for the choiceless approval
// prompt); maxRows caps the scrolled row window and maxBody the wrapped body block, both ≥ 0 and
// both in renderPopup's zero-means-none sense. seated is false when the frame's allocation left
// this pane no rows at all — a window too short to seat it beside its siblings — and the overlay
// renders nothing rather than a pane drawn past the terminal's last row.
//
// For the MODAL prompt that outcome is reachable only below twelve rows, where no pane is drawn at
// all: the prompt is last in the give-way order and the input box's draft rows now give way to it
// (draftRowsCeiling), so on every window a pane can be drawn in the decision the run is blocked on
// is on the frame. It is not "seated or silent" — a decision surface must never be invisible while
// its keys are live.
//
// The budget comes from the frame-wide allocation (frameRowPlan) rather than from the viewport
// directly, because the pane is never the only thing taking rows off the transcript: the staged
// band shares the same rows, and so does any other pane open beside it. p is added to the open set
// here rather than assumed to be in it, so a pane asking for its budget is always counted as one of
// the siblings — which is what lets a renderer be called for a pane the Model's own flags have not
// opened (the direct-call tests) and still be budgeted like the frame would budget it.
//
// Inside its grant the pane spends its chrome first (the 2 borders + the title + the hint), gives
// the ROWS priority — they are what the human acts on — and lets the body have what is left,
// overflowing into the explicit "… (+N more lines)" marker.
//
// BOTH caps floor at ZERO rather than at a comfortable minimum, and that is the point of them: a
// row floor of 6 on a window with 4 rows to give promised a pane the frame could not hold, and the
// surplus came off the bottom — the input box and the footer, off the alt screen. A body floor of
// 1 was the same mistake one row smaller: it made a body-bearing pane's irreducible height 5 where
// a boxed overlay's chrome is 4, so the ask and approval prompts overflowed the shortest window a
// pane fits in at all (12 rows — the fixed chrome below the transcript is 8) by exactly one row
// while the browser fitted. A budget that shrinks to nothing lets the TRANSCRIPT shrink to nothing
// instead, which is the outcome D2 asks for; a pane that can spare neither rows nor body still
// names itself in the title and says how to act in the hint — and when the zero budget dropped
// prose, that title row also carries the "… (+N more lines)" marker (popupTitleLine), so shrinking
// costs the body but never the fact that there is one.
func (m Model) popupBudget(p framePane, rows, rowCap int) (maxBody, maxRows int, seated bool) {
	granted := m.frameRowPlan(m.openPanes().with(p)).panes[p]
	if granted < popupChrome {
		return 0, 0, false
	}
	avail := granted - popupChrome
	maxRows = min(rows, rowCap, max(0, avail-1))
	maxBody = max(0, avail-maxRows)
	return maxBody, maxRows, true
}

// approvalPrompt renders the pending tool call the human must rule on as a bordered popup pane
// above the input box (the shared popup module; D7/D8): the title carries the RAW tool name
// verbatim (not the friendly transcript label — the approval flow is a security surface, so the
// human sees exactly the tool that will run), the body carries a non-empty Reason then the
// pretty-printed Arguments, and the hint carries the decision legend. Every model-authored string
// (tool name, reason, args) is escape-stripped at this call site; stripEscapes removes only the
// ESC byte, so the raw tool name is preserved verbatim. Empty/null arguments add no body. Only the
// top-level (Depth == 0) prompt is rendered this phase.
//
// The guarantee this security surface holds is NOT that the whole reason is always on the screen —
// no pane can promise that on a terminal with four rows to give. It is that the human is never
// asked to decide against text the pane hid WITHOUT SAYING SO: the module word-wraps the reason
// rather than clipping it, and whatever it cannot seat it counts out in the "… (+N more lines)"
// marker — the body block's last row while it has one, and the TITLE row when the window leaves the
// pane its irreducible four (popupBudget grants no body rows between 12 and 15 rows, where a fifth
// would push the input box off the frame). On a pane too narrow to seat the tool name beside the
// full phrase the count is stated in fewer words rather than clipped off the row (popupTitleLine),
// because the terminal that is short is usually the one that is narrow. So the title always carries
// the identity the decision turns on, and says when there is more to read than it is showing.
//
// "Always" is total because the PANE is: the frame seats it last of all the surfaces and the input
// box's draft rows give way to it (draftRowsCeiling), so there is no window a pane can be drawn in
// where a/d/s are live and this pane is not on the frame. The "" below is the sub-twelve-row case,
// where the frame draws no pane at all.
func (m Model) approvalPrompt(req domain.ApprovalRequest) string {
	var parts []string
	if req.Reason != "" {
		parts = append(parts, "reason: "+stripEscapes(req.Reason))
	}
	if args := prettyJSON(req.Arguments); args != "" {
		parts = append(parts, stripEscapes(args))
	}

	maxBodyRows, _, seated := m.popupBudget(panePrompt, 0, 0) // no rows on the prompt, so no row budget either
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}
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
// are what the human acts on), the body takes what is left and overflows into the explicit
// "… (+N more lines)" marker, and on a window that can spare neither the pane shrinks to its
// title and hint — with that title counting BOTH the question lines and the choices it could not
// seat (D2), so a hint still offering ↑↓ is never the only trace of an offering the pane dropped.
func (m Model) askPrompt(req domain.AskRequest) string {
	choicesShown := len(req.Choices) > 0 && m.input.Value() == ""

	selected := -1
	hint := "type your answer below · ⏎ send · ⇧⏎/⌥⏎ newline · esc cancel"
	if choicesShown {
		selected = min(max(m.askSel, 0), len(req.Choices)-1) // clamp: routing keeps it in range, this is defensive
		hint = "↑↓ select · ⏎ send · type for a custom answer · esc cancel"
	}

	// Budget against the live layout so a long question or choice set never pushes the input box
	// off-screen (D2); the rows get priority and the body takes what is left (see popupBudget).
	maxBodyRows, rowsShown, seated := m.popupBudget(panePrompt, len(req.Choices), maxAskChoiceRows)
	if !seated {
		return "" // the frame cannot seat this pane beside its siblings (frameRowPlan)
	}

	spec := popupSpec{
		title:       "the assistant is asking:",
		body:        stripEscapes(req.Question),
		maxBodyRows: maxBodyRows,
		rows:        singleCellRows(stripEscapesAll(req.Choices)),
		selected:    selected,
		hint:        hint,
		maxRows:     rowsShown,
	}
	return renderPopup(m.th, spec, m.width)
}
