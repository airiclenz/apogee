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
	"github.com/airiclenz/apogee/internal/format"
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

	// flushEvents empties the teaSink's token-coalescing buffer (sink.go). The worker calls it the
	// moment each Step returns, so a coalesced token can never be delivered after the Step that
	// emitted it — see stepToBoundary. Only Run can wire it, because the Bridge owns the sink; it
	// stays nil in the model tests, which inject eventMsg past the sink entirely.
	flushEvents func()

	// diag is the --tui-diag log (diagnostics.go), or nil — which is the normal state, the flag
	// being hidden and off by default. It is held by POINTER because it owns a mutex and an open
	// file, which no value-copied Model may carry by value (the no-copy invariant in doc.go), and
	// every method on it is nil-safe so the observation points below are unconditional lines
	// rather than a branch each. Only Run can wire it, like flushEvents above.
	diag *diagLog

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

	// undoGeneration is the journal stamp the last `/undo` preview quoted (undo.go, ADR 0051): the
	// proof a following `/undo confirm` hands back so the engine can refuse a step the human never
	// read. The zero value is "no preview stands" — and it needs no special case, because a journal
	// that holds anything to undo has moved past zero, so a cold confirm meets the stale guard and
	// earns a fresh preview rather than a revert. A plain uint64, so it rides the value-copied
	// Model (ADR 0011).
	undoGeneration uint64

	// picker is the shared single-select overlay's state (picker.go): which offering it lists and
	// which row is highlighted. Its rows are derived at render time from the state they describe —
	// the /model picker reads hb.models live, the /server picker opts.Servers — so the value itself
	// is three plain values and its zero value is "closed", the sessionBrowser posture (ADR 0011).
	// It is driven only at idle.
	picker picker

	// settings is the /settings pane's state (settings.go): whether it is open and which config key
	// is highlighted. Its rows are derived at render time from [Options.SettingsRows], so the value
	// itself is two plain values and its zero value is "closed" — the picker posture (ADR 0011). It
	// is driven only at idle, and it is the frame's one full-height pane (frameRowPlan).
	settings settingsPane

	// usagePane is the /usage report overlay's state (usage.go): whether it is up, and how far the
	// wheel has scrolled its row list. The rows themselves are derived at render time from the folds
	// that already hold the per-agent totals, so the value is a bool and an int and its zero value is
	// "closed at the top", the settings posture (ADR 0011). Unlike the panes above it, it is driven in
	// every state: the verb is whileRunning and the pane reads Model state only.
	usagePane usagePane

	// inspector is the /inspect pane's state (inspector.go): whether the raw-protocol view is up and
	// how far its row list is scrolled. Two plain values whose zero value is "closed", the usagePane
	// posture (ADR 0011), and driven in every state for the same reason: the verb is whileRunning and
	// the pane reads Model state only.
	inspector inspectorPane

	// wire is the Inspector's bounded ring: the most recent maxWireRecords halves of an Upstream
	// round-trip, as the fold recorded them (foldWire). Each half carries the wire stream it came
	// from — the (depth, callID) pair of the agent that made the call — because the one ring holds
	// every run's traffic interleaved, and that pair is what pairs a request to its own reply
	// (hasUnrecordedReply). It sits BESIDE the transcript rather than in
	// it — a wire record is not a conversation entry and must not disturb entry folding — and it is
	// rebuilt rather than appended into, so it rides safely in the value-copied Model (ADR 0011). It
	// is empty on every run that leaves `ui.inspector` off, where the engine emits no WireEvents at
	// all.
	wire []wireRecord

	// settingEdits is the journal of every config key this SESSION changed through the settings surface
	// — the fact behind each row's ` *` marker (ADR 0037 decision 8). It lives here rather than on the
	// pane above because its lifetime is the session's and the pane's is one overlay: the human opens
	// and dismisses /settings as often as they like, and a journal that died with the overlay would tell
	// a session running an edited value that nothing had been edited. Only a relaunch clears it, which
	// is what building a new Model is. One entry per key, and the slice is REPLACED rather than appended
	// into (recordSettingEdit), so it is safe in the value-copied Model (ADR 0011).
	settingEdits []settingEdit

	// cfgWatch is the config watcher's own state on this side of the seam: how many re-reads in a row
	// the binary could not make, and whether the transcript has said so (ADR 0041 decision 7). It sits
	// beside the journal above rather than on the pane, for the pane's own reason — the watch runs for
	// the whole session and most of its reports land with no overlay open at all.
	cfgWatch configWatchState

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
	// askChecked is the ticked set of a MULTI-SELECT ask_user question: one bool per offered
	// choice, ␣-toggled on the highlighted row, and nil for a single-select question — which has
	// no checked set at all, so every single-select path stays exactly what it always was. It is
	// a bare []bool, no self-referential no-copy type, so it rides the value-copied Model
	// (ADR 0011); the toggle writes it in place, which is safe because the slice is allocated
	// fresh for each question (the askReqMsg fold) and no copy of the Model outlives the Update
	// that produced it.
	askChecked []bool
	// approvalSel is the highlighted approvalMenu row while awaitingApproval: the row ↑/↓ move and
	// ⏎ takes. It resets to 0 — Allow, the mockup's default — on every incoming request rather than
	// persisting across them, because a menu that remembers where the last decision was left points
	// at Deny for a call the human has not read yet. A bare int, no no-copy type (ADR 0011).
	approvalSel int
	// askDraft holds what was in the input box when an ask_user question BORROWED it: the question
	// empties the box for the answer (D5 pre-selects the first choice on an empty box), and an
	// unsent message the human was part-way through typing is not the question's to throw away.
	// restoreAskDraft hands it back the moment the box stops being borrowed. A plain string, so it
	// rides the value-copied Model (ADR 0011).
	askDraft  string
	lastErr   error     // the error behind stateErrored, shown in the status line
	lastCtrlC time.Time // when the last Ctrl+C landed; a second within the window quits
	quitting  bool      // a quit whose exit is deferred: to the worker's terminal Msg while busy (C4), else to the closing flush reaching disk (quit)

	// activityBusy is the last value published through [Options.ReportActivity] — the high-water
	// mark that turns a per-Update reading into a per-TRANSITION report (schedule.go). Its zero
	// value is the truth about a fresh Model: a session at its prompt is not busy, so the first
	// report a listener ever gets is the one that says it started working.
	activityBusy bool

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

	// lastEvent is when the ENGINE was last heard from: any Event, at any depth, of any variant —
	// including a ReasoningEvent, since a reasoning stream is life and the stall the guard was built
	// for (2026-08-14) had NO events at all — plus a move to an activity kind the guard watches, the
	// launch of a worker whose request is away but unanswered (moveActivity, isQuietWatched). It is the quiet clock the stall guard reads: once the gap to now
	// crosses [Options.StallAfter] the status line qualifies the running phrase with a bare `quiet`
	// in front of the activity's own clock — "thinking · quiet · 2m 59s" (runningPhrase,
	// activity.quiet) — and the silence's own length is shown nowhere. Nothing on a timer touches
	// it: a heartbeat or a spinner frame proves the TUI is alive, not the engine. A plain time.Time,
	// safe in the value-copied Model (ADR 0011).
	lastEvent time.Time

	// reasoning is the current Turn's reasoning as the view RETAINS it — a bounded, escape-stripped
	// tail of the chunks domain.ReasoningEvent reveals, for one agent at a time (reasoning.go).
	// NOTHING RENDERS IT. It is the seam a future reasoning display reads, landed ahead of that
	// display so the retention rules settle where they can be tested rather than inside a renderer;
	// what the status line says about reasoning today it says from the ARRIVAL of the event (act,
	// above), never from these bytes.
	reasoning reasoningTail

	// Live stats folded from the engine's UsageEvent (server token accounting). ctxUsed is
	// the latest top-level (Depth 0) total-token count, driving the context-usage gauge;
	// genStart marks when the current Turn began streaming content (set on its first token,
	// cleared on a re-stream or once usage lands) and tokPerSec is the last completion's
	// throughput, timed against the Update clock. All value/zero-safe in the copied Model.
	ctxUsed   int
	genStart  time.Time
	tokPerSec float64

	// usage is the MAIN agent's cumulative token accounting for the session — the latest reading
	// its Depth-0 UsageEvents carried (usageTotals, fold.go), including the maintenance events the
	// gauge above skips. It is persisted with the session record and restored on reopen, so a
	// resumed session reports what it has already spent. A sub-agent's totals live on its own run
	// head instead (transcript.applyUsage), which is where the per-agent grouping already is.
	usage usageTotals

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
	th            theme // the palette and reusable styles; rebuilt whenever the colour scheme changes (ADR 0040)
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
	// cursor is the transcript's modal KEYBOARD pointer over those same targets (blockcursor.go):
	// whether the walk is on, and which content line its highlight is standing on. Two plain fields
	// by ADR 0011, and a content line rather than a screen row so the highlight travels with the text
	// it names through a scroll or a stream — refreshViewport keeps it standing on a real click
	// surface (blockCursor.clamp) exactly as it keeps the map above it honest.
	cursor blockCursor
}

// newModel builds the initial idle Model. parent is the program context the worker derives
// its cancellable child from (so a program-wide shutdown also cancels an in-flight
// Exchange — C4). The input box is focused here, not in Init: Init takes a COPY of the Model
// and returns only a Cmd, so the focus *state* has to be set on the stored widget. The caret it
// is focused for is the real terminal cursor in Options.CursorShape (newPromptEditor).
func newModel(parent context.Context, eng Engine, opts Options, notify func(tea.Msg)) Model {
	th := newTheme(opts.colorScheme())

	vp := viewport.New()
	vp.SoftWrap = true // wrap long transcript lines to the viewport width

	m := Model{
		parent:       parent,
		eng:          eng,
		opts:         opts,
		sessions:     opts.Sessions,
		notify:       notify,
		promptEditor: newPromptEditor(opts.CursorShape, th.surface),
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

	// Give the transcript its block-paint cache (paintcache.go). It is built ONCE, here, and lives
	// behind a pointer for the same reason ws is set before the first fold: every by-value copy of
	// the Model has to reach the same one (ADR 0011), and a cache rebuilt per copy would never hit.
	m.transcript.paints = newPaintCache()

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
	// What loading the colour scheme cost, if anything (ADR 0040 design call 11). It goes after the
	// notices above because it is about the SCREEN rather than about the session, so it reads as the
	// last word on the frame the human is looking at — and because a scheme that loaded cleanly, the
	// ordinary case, adds nothing here at all.
	m.noteColorSchemeWarnings(opts.ColorSchemeWarnings)
	// A session the binary could not resolve a server for opens ASKING (ADR 0036 decisions 3 and 7):
	// the `/server` picker, or `/settings` when nothing is configured to pick. It goes last of all —
	// after every notice above — because it is the one thing the human has to act on before the
	// session can do anything at all (prebound.go).
	m.openPrebound()
	// And, on a session that HAS its server, the one other question a start-up may ask unasked: a
	// plaintext `api-key:` this machine's secret store could hold instead (ADR 0047, keymigration.go).
	// It goes last because it gives way to the ask above — a session with no server has a more urgent
	// question, and this one costs nothing by coming back at the next start-up.
	m.openKeyMigration()
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
	// …and reopen the accounting where the record left it: the totals are the reading this session
	// last took, so a resumed session reports its spend instead of starting from nothing. A record
	// written before the feature carries zeros, which is exactly the nothing-reported state.
	m.usage = usageTotals(r.Usage)
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
		m.transcript.addEphemeralNote("standing system content ~" + format.TokensFine(report.StandingTokens) +
			" tokens exceeds its Budget share (~" + format.TokensFine(report.SystemShare) +
			") — trim context files or the system prompt")
	}
}

// noteColorSchemeWarnings surfaces what loading the configured colour scheme cost — an unknown
// name, an unreadable file, a key whose value is not a colour. The load is forgiving by design (ADR
// 0040 design call 8): every one of those keeps the default palette rather than failing the run, so
// a warning is the only thing that tells the human their scheme is not the one on screen.
//
// The lines are rendered by the binary (Options.ColorSchemeWarnings), which is what read the file;
// this only places them. They are EPHEMERAL notes for the reason the context notices are: each is
// re-derived from the config and the schemes folder at every startup, so persisting one would stack
// a duplicate into the record on every resume while telling the next launch nothing it does not
// re-discover.
//
// The text names a file and a key from the user's own schemes folder, so it is untrusted disk input
// like a session title — addEphemeralNote escape-strips it at the seam.
func (m *Model) noteColorSchemeWarnings(warnings []string) {
	for _, w := range warnings {
		m.transcript.addEphemeralNote(w)
	}
}

// fillInput gives the textarea the solid interior the layout calls for: the base, text, cursor
// line, and placeholder all sit on the scheme's `surface` tone so the box reads as one solid field
// inside its chrome border, on any terminal theme. The colour is passed in rather than read off a
// package-level palette because it is the active scheme's ([theme.surface], ADR 0040) — the widget
// belongs to Bubble Tea, so this is the only way the theme reaches it.
func fillInput(ta *textarea.Model, surface color.Color) {
	s := ta.Styles()
	for _, st := range []*textarea.StyleState{&s.Focused, &s.Blurred} {
		st.Base = st.Base.Background(surface)
		st.Text = st.Text.Background(surface)
		st.CursorLine = st.CursorLine.Background(surface)
		st.Placeholder = st.Placeholder.Background(surface)
		st.EndOfBuffer = st.EndOfBuffer.Background(surface)
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

// Init fires the first heartbeat, reads this workspace's prompt recall, opens the wait on the
// config file and asks whether a recorded Launch profile has to be restored, and that is the whole of
// its work. The window is sized by the first WindowSizeMsg the program sends, the input's focus
// STATE is set at construction (newModel — Init holds a copy, so it could not set it here), and the
// caret needs no start-up Cmd either: the virtual cursor is retired, so there is no blink to
// schedule and nothing to animate (steadyCursor, View). The spinner likewise ticks only while
// running. The beat goes out immediately rather than after one Interval, because startup discovery
// IS the first beat (the binary no longer probes before painting): the sooner it lands, the sooner
// the footer stops saying "connecting…".
//
// The four are batched rather than sequenced because none waits on another — the recall read is a
// file the box needs before the first ↑, the beat a network probe the footer needs, the config
// wait parks until somebody saves a file that may never be touched all session, and the restore check
// reads the launcher's own config and answers with a decision the fold acts on (actuation.go). Every
// Cmd is nil when its seam is unwired, and tea.Batch drops nils and collapses to the single survivor
// (or to nil), so an unwired TUI still starts nothing at all — exactly as before.
//
// This is also the ONLY place the boot restore is issued from, which is what makes it interactive-TUI
// only: a headless run builds no Model, so it never reaches an Init and never actuates a server.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.beatCmd(), m.loadRecallCmd(), m.awaitConfigChange(), m.restoreCmd())
}

// ----------------------------------------------------------------------------
// Update — fold the five seam Msgs + keypresses (C1–C4)
// ----------------------------------------------------------------------------

// Update folds exactly the messages ADR 0011 defines — the five worker→Update Msgs
// (eventMsg / approvalReqMsg / exchangeDoneMsg / cancelledMsg / errMsg), keypresses, the
// window size, and the spinner tick — and nothing else. It mutates the local copy and
// returns it. It contains no agent logic: a submit launches the worker (C1), the seam Msgs
// move the state machine and render, and the stop key cancels (C4).
//
// The deferred report is the one thing every fold below shares. Each case returns from its own
// arm — there is no common tail — so publishing this session's activity from a defer is what makes
// [Options.ReportActivity] TOTAL rather than a list of transition sites a later change has to
// remember to extend (schedule.go). It is pure bookkeeping: it reads the folded model, calls the
// seam only when the value moved, and returns the model carrying the new high-water mark.
func (m Model) Update(msg tea.Msg) (next tea.Model, cmd tea.Cmd) {
	defer func() { next = reportActivity(next) }()

	// The --tui-diag observation point (diagnostics.go). It consumes nothing — every message it
	// recognises still reaches the switch below — so a session with the flag on behaves exactly
	// like one without it, and with the flag off (the normal case) m.diag is nil and this is a
	// nil check. It sits above the switch rather than inside three cases because one of the three
	// (the colour profile) has no case of its own and adding one would change what happens to it.
	m.diag.observe(msg)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.sel = promptSel{} // the box moved/reflowed: its stored visual coords are stale
		m.dropRecall()      // and the box the human recalled into is no longer the box in front of them
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
		// What the report MADE of the measure, beside the report itself — the diag log records
		// the mode number and value above, and this is the consequence a rendering bug is
		// actually argued from (diagnostics.go). Change-suppressed, so only the one report that
		// moves it writes a line.
		m.diag.record(diagWidthMethod, widthMethodName(m.th.measure.Method()))
		if m.th.measure != before && m.ready {
			m.layout() // the measure moved: re-wrap and repaint everything against the new one
		}
		return m, nil

	case tea.KeyboardEnhancementsMsg:
		// The terminal answered bubbletea's kitty-keyboard query — sent on the first frame and
		// again whenever the enhancements or the screen switch
		// (bubbletea/v2@v2.0.8/cursed_renderer.go:378-393), so a capable terminal is heard from
		// within the first frames and one that is not simply never sends this. Non-zero flags mean
		// key disambiguation is on, which is the ONLY condition under which ⇧⏎ arrives as anything
		// other than a plain ⏎; the prompt legend is the one thing that must not claim the chord
		// before then (prompteditor.go). Nothing else in the program reads this message — like the
		// mode report above it is consumed here rather than falling through to the input widget,
		// which ignores it.
		m.setKeyDisambiguation(msg.SupportsKeyDisambiguation())
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
		//
		// The /settings pane is asked FIRST and for the same reason handleKey asks it first: while it
		// is open it is the surface the human is typing at, and the box below is one they cannot see
		// (settings.go).
		if next, cmd, claimed := m.settingsPaste(msg); claimed {
			return next, cmd
		}
		if !m.inputEditable() {
			return m, nil
		}
		m.sel = promptSel{} // the value is about to change; drop the selection before its coords go stale
		m.dropRecall()      // a paste is an edit: the recalled entry is now the human's own draft
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.state == stateIdle || m.state == stateRunning {
			var reload tea.Cmd
			m, reload = m.recomputeAutocomplete() // re-derive the overlay from the pasted-into input
			cmd = tea.Batch(cmd, reload)          // a "/" menu the paste opened owes a catalog re-scan, off the loop
		}
		m.layout() // re-flow: the box auto-grows as the pasted text wraps to more rows
		return m, cmd

	case ctrlCResetMsg:
		// The Ctrl+C quit window elapsed without a second press: disarm the gesture so the
		// "press ctrl+c again to quit" hint clears (handleKey's ctrl+c case).
		m.lastCtrlC = time.Time{}
		return m, nil

	case eventMsg:
		// Any Event, any depth, any variant: the engine is not silent. Stamped before the fold and
		// unconditionally, so an Event the folds deliberately ignore — usage accounting, an audit
		// record, a fired mechanism — still counts as life on the stall guard's clock (activity.go).
		m.noteEngineHeard()
		m = m.foldEvent(msg.Event)
		m.refreshViewport()
		return m, nil

	case approvalReqMsg:
		// The worker's Approver hands the gate to the Update loop; this case records the
		// request and switches state. View renders the prompt and handleApprovalKey replies
		// on msg.Reply (the C3 rendezvous; P2.4).
		m.state = stateAwaitingApproval
		m.pending = &msg
		m.approvalSel = 0       // the menu opens on Allow for every request (docs/layout/user-questions-layout.md)
		m.dismissAutocomplete() // a stale menu never shares the frame with a decision surface
		m.layout()              // the pane the decision turns on outranks the draft's extra rows
		return m, nil

	case askReqMsg:
		// The worker's Asker hands a free-text question to the Update loop; record it, switch
		// state, and re-focus the (emptied) input so the human types the answer. View renders
		// the question; submitAnswer replies on msg.Reply when the human submits (P3.11).
		return m.foldAskRequest(msg)

	case scheduleEventMsg:
		// The scheduler narrating one of its Schedules (ADR 0033). Like presentedMsg it is a
		// record rather than a gate: it appends one note and touches neither the state machine
		// nor the running Turn, because the Firing it describes is a separate headless run that
		// this session's conversation knows nothing about.
		m = m.foldScheduleEvent(msg.Event)
		m.refreshViewport()
		return m, nil

	case routingNoticeMsg:
		// The composition root's second heartbeat reporting that delegations changed destination
		// (ADR 0045 §4). Like the schedule Event above it is a record and not a gate — one note, no
		// state transition, no engine call — because routing is a fact about the OTHER server and
		// this session's conversation carries on regardless.
		//
		// The note is EPHEMERAL, like the "context: …" line: the routing state is re-derived from
		// live beats every time a session starts or resumes, so a stored "routing to grunt" is a
		// claim about a server nobody has beaten since — and five resumes would keep five of them
		// (addEphemeralNote).
		m.transcript.addEphemeralNote(msg.note)
		m.refreshViewport()
		return m, nil

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
		// "Held" covers exactly what the worker never DELIVERED. A row it did deliver was dropped
		// from the conversation by the AbortExchange above and stays dropped — sent is sent (owner
		// ruling 2026-08-03): it is not the queue's to hold back, and the ⧖ block beside the
		// "cancelled" note is the visible record of the model having read it before the Exchange was
		// scrapped, not a claim that it is still waiting to go out.
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
		m.transcript.addError("loop", msg.Err.Error(), runRef{})
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

	case settingsEditedMsg:
		// The external editor the /settings pane suspended into has exited: re-read the config the
		// human just edited and apply every key that came back different (settings.go). Nothing else
		// moves — the program was paused, not stopped, and the pane is still the screen.
		return m.foldSettingsEdit(msg)

	case settingsDetachedMsg:
		// The other launch: an editor started WITHOUT this terminal (ADR 0041 decision 6) either
		// started or did not. Nothing is re-read — the pane never left, and what gets saved out there
		// arrives through the config watcher — so this only puts the outcome on the launching row.
		return m.foldDetachedEdit(msg)

	case configChangedMsg:
		// The config file changed on disk, whoever changed it — the detached editor above, another
		// window's, or a second terminal's (ADR 0041 decision 3). Re-read it, apply every key that came
		// back different through the journal and the dispatcher a pane edit uses, and open the next
		// wait (settings.go). Nothing else moves: nobody pressed anything, so the screen is whatever it
		// already was.
		return m.foldConfigChanged(msg)

	case sessionListMsg:
		// Sessions.List() returned off the Update loop: open (or refresh) the /sessions browser
		// over the metas, or note the empty/error case with no overlay (sessions.go).
		return m, m.foldSessionList(msg)

	case sessionLoadedMsg:
		// Sessions.Load(id) returned: restore the record into the live engine and repaint its
		// scrollback, or note the failure with the view left untouched (sessions.go).
		return m, m.resumeLoaded(msg)

	case skillsReloadedMsg:
		// The catalog re-scan the merged "/" menu dispatched when it opened has finished and swapped
		// in a fresh snapshot on the shared provider: repaint the dropdown over it, so a skill
		// written since launch shows in the menu that asked for the walk (autocomplete.go). The walk
		// itself ran on the Cmd goroutine, which is the point — a keystroke no longer waits on it.
		m.foldSkillsReloaded()
		return m, nil

	case skillsRescannedMsg:
		// The catalog re-scan /skills dispatched has finished: record the listing over the snapshot
		// the provider now holds (skills.go). Same walk as the menu's and on the same Cmd goroutine
		// — the verb no longer freezes the render loop for the length of a disk walk (ADR 0011) —
		// and the note it writes here is the report the human ran the verb for.
		m.noteSkillCatalog()
		return m, nil

	case recallLoadedMsg:
		// Recall.LoadPrompts() returned (Init's start-up read): install what this workspace has
		// sent before as the box's recall state (recall.go). It repaints nothing — the entries are
		// invisible until an arrow asks for one — so no layout follows.
		m.foldRecallLoaded(msg)
		return m, nil

	case spinnerTickMsg:
		// Keep the chain alive only while running, and only for the current generation: dropping
		// the tick when idle lets the chain die naturally, and dropping a tick from a previous arm
		// means a re-arm (an approval answered, an ask replied to) cannot leave two chains running
		// and double the frame rate (spinner.go).
		if m.state != stateRunning || msg.gen != m.spin.gen {
			return m, nil
		}
		wasBlink := m.spin.blink()
		m.spin.frame++
		if m.spin.blink() != wasBlink && m.transcript.hasOpenToolCall() {
			// The frame the status line spins on is also the LIVE STAR's clock: a block still holding
			// an open call paints its header glyph from this frame's blink phase (layout.md, "The live
			// star"; blockState.star), so the flip needs a repaint the tick did not use to do. It is
			// asked for only on the tick that actually FLIPS the phase, and only while something is
			// open — every other tick paints byte-identically, and re-rendering the whole scrollback
			// ten to twenty times a second for an identical result would be work for its own sake, and
			// would put the keep-if-unchanged rule (refreshViewport) between the human and every
			// drag-selection they hold through a turn. A selection spanning a header that DOES flip is
			// dropped, which is that same rule doing its ordinary job on a line that changed.
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

	case restoreMsg:
		// The start-up restore check answered (actuation.go): load the Launch profile this server was
		// left on, state why it is not being loaded, or do nothing at all. It lands off a Cmd goroutine
		// like a beat, and what it may do it does through the latch above rather than beside it.
		return m.foldRestore(msg)

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
		// A notch over the open /settings pane walks its key list instead (mouse.go): the pane is what
		// is under the pointer there, and the transcript keeps every notch outside it.
		if next, handled := m.settingsWheel(msg); handled {
			return next, nil
		}
		// A notch over the open /usage report scrolls its row list the same way, for the same reason
		// (mouse.go) — the two panes never share a frame, so the order between them is arbitrary.
		if next, handled := m.usageWheel(msg); handled {
			return next, nil
		}
		// A notch over the open /inspect pane scrolls its record list on the same terms (mouse.go).
		// It is asked after the report because it is drawn under it, and the two rectangles never
		// overlap, so only one of them can hold the pointer.
		if next, handled := m.inspectorWheel(msg); handled {
			return next, nil
		}
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
		//
		// "The focused input" is the /settings field while the pane has one open, and this arm is the
		// only route such a field can be reached by: the clipboard reply its own ctrl+v asks for is a
		// Msg of the widget package's own unexported type, which no switch above can name
		// (settingsEditorMsg, lineEditor.editMsg).
		if next, cmd, claimed := m.settingsEditorMsg(msg); claimed {
			return next, cmd
		}
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
// is otherwise a no-op); Ctrl+C twice within ctrlCQuitWindow quits; Ctrl+L forces a full repaint
// and changes nothing else. Enter submits at idle — the
// box plus any interjections a stop or an error left held, even on an empty box — answers a
// pending ask, dismisses an error, and while a worker runs STAGES what was typed as an
// interjection, launching nothing, so the single-worker invariant the seam relies on still
// holds. Other keys feed the input wherever the box is editable (idle, ask, running —
// inputEditable), and scroll the transcript at the inert states; PgUp/PgDn scroll in every state,
// which is what a running Exchange leaves the keyboard for reading with (ADR 0025).
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Any keypress drops a mouse drag-selection: typing past it, navigating, or submitting all
	// move on from what was selected, and clearing here (before the value can change) keeps the
	// selection's cached visual coordinates from ever going stale (mouse.go). Backspace and Del are
	// carved OUT of that meaning — they DELETE what is selected rather than move on from it — so
	// the span is stashed on the way past. The clear itself stays unconditional: whichever branch
	// below claims the key, the stale coordinates are already gone.
	sel := m.sel
	m.sel = promptSel{}

	// Recall mode ends on any keypress for the same reason and by the same shape: it means "the box
	// holds a recalled entry the human has not acted in", and a keypress IS acting in it. The two
	// arrows that are recall itself are carved out the same way the destructive keys are — the walk's
	// position is stashed on the way past and read back by recallKey (recall.go) — so the clear can
	// stay unconditional and no branch below has to remember to end the mode. The entries are not
	// mode: they survive every key.
	rec := m.recall
	m.dropRecall()

	// The /sessions browser is a modal overlay (idle only): while open it claims every keypress —
	// selection, resume, delete-confirm, rename edit, and esc to close (sessions.go) — before the
	// normal input routing below, exactly as the autocomplete overlay claims its keys first.
	if m.state == stateIdle && m.sessionBrowser.open {
		return m.sessionBrowserKey(msg)
	}

	// The /settings pane is modal in the same way and for a stronger reason: it is the frame's one
	// FULL-HEIGHT pane (frameRowPlan), so the input box behind it is a box the human cannot read —
	// a keystroke falling through to it would edit an invisible draft. Idle-only, like its verb
	// (commandSpecs), and it swallows every key it does not act on (settings.go).
	if m.settingsOwnsInput() {
		return m.settingsKey(msg)
	}

	// The picker is the browser's simpler sibling and claims keys the same way: while it is open the
	// selection, the accept and esc are all its own (picker.go). It claims them in BOTH live states,
	// because /schedule opens its cycle and mode popups mid-Exchange as well — the verb is
	// whileRunning (commandSpecs), and an overlay that renders without taking keys would be a modal
	// the human cannot answer. The older kinds are unaffected: their verbs are idle-only, and a
	// picker cannot be open when a worker STARTS, since it owns ⏎ for as long as it is up.
	if (m.state == stateIdle || m.state == stateRunning) && m.picker.open {
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

	// The /usage report claims esc and the four keys that scroll it, and nothing else (usage.go). It
	// is not modal — it says something rather than asking, so the box behind it stays live and every
	// other key goes where it always went — and the claim sits below the overlays above precisely
	// because they ARE modal: a pane that owns the keyboard answers its own esc first. Below the
	// dropdown for the same reason one rung down: a menu a keystroke opened is dismissed by the esc
	// the human means for it.
	//
	// It must stay ABOVE the transcript's PgUp/PgDn interception further down, which claims those two
	// keys in every state: the pane is what the human is reading, and a page key that scrolled the
	// conversation hidden BEHIND the report would move the one list they cannot see.
	if handled, next, cmd := m.usageKey(msg); handled {
		return next, cmd
	}

	// The /inspect pane claims the same five keys on the same terms (inspector.go): not modal, so
	// every key it does not act on goes where it always went, and below the modal overlays above
	// because a pane that owns the keyboard answers its own esc first. It sits beside the report it
	// is shaped after — the two are never open together in practice and their claims are disjoint
	// while one of them is closed, so the order between THEM decides nothing.
	if handled, next, cmd := m.inspectorKey(msg); handled {
		return next, cmd
	}

	// The transcript's modal block cursor claims its keys here, ahead of the switch below, because
	// two of them — ⏎ and esc — are the frame's own verbs and the mode exists to borrow them
	// (blockcursor.go, design call 7). It is entered by ⌥↑/⌥↓ and left by esc or by typing, and the
	// typing case is why the next Model is taken whether or not the key was claimed: the printable
	// key that ends the walk goes on to the prompt box below with the mode already off.
	walked, cursorCmd, cursorTook := m.blockCursorKey(msg)
	m = walked
	if cursorTook {
		return m, cursorCmd
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
	case "ctrl+l":
		// The readline/terminal meaning of the key — redraw — and here it is a repair tool. The
		// renderer paints each frame as a DIFF against its own model of the screen, so a terminal
		// that disagrees with that model about where a write landed leaves cells the diff believes
		// are already correct and never repaints (the Windows ghosting: see
		// docs/plans/2026-08-06 - 04 - windows-tui-ghosting-plan.md). tea.ClearScreen drives the
		// renderer's MoveTo(0,0) + Erase(), which marks the whole screen for a full repaint on the
		// next frame — the same resync a terminal resize performs today, without resizing.
		//
		// It is a display command and nothing else: this branch touches no state, so the frame that
		// lands is the frame that would have landed anyway — with the one exception every branch of
		// this switch inherits, the unconditional pre-switch clears above (m.sel, dropRecall). A
		// live drag-selection therefore loses its highlight to ⌃l exactly as it loses it to any
		// other keypress, which is the every-keypress contract those clears exist to keep, not a
		// side effect of redrawing. Live in every state, and
		// it steals nothing — the textarea binds no ctrl+l (its bindings are set in
		// newPromptEditor / lineEditor.singleLine) — while the modal overlays above swallow it with
		// every other key they do not claim, which is the contract they already keep.
		return m, tea.ClearScreen
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
		case stateAwaitingApproval:
			// Take the approval menu's highlighted row. The menu IS the decision surface now — the
			// legend that used to spell the letters out is gone, so ⏎ on the pointed-at row is the
			// way in for a human who has not learnt a/s/d yet (resolveApproval).
			return m.resolveApproval()
		case stateErrored:
			// Dismiss the error and return to idle so the next message can be sent. With a queue
			// held by the fault (ADR 0025) that is deliberately TWO presses: this one clears the
			// error, the next sends what was held. An error the human has not finished reading
			// must not be answered by firing off their queued remarks.
			m.lastErr = nil
			m.state = stateIdle
			return m, nil
		default:
			return m, nil
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

	// The ask prompt's choice keys — ↑/↓ over the offering and ␣ on a multi-select row — are the
	// pane's own while the answer box is still empty (ask.go). It claims nothing once the human starts
	// typing, so the arrows go back to being cursor keys and the free-text answer is never stolen.
	if claimed, next, cmd := m.askChoiceKey(msg); claimed {
		return next, cmd
	}

	// Wherever the box is editable the keys type into it: idle (a message to send), awaiting an
	// ask_user answer (the borrowed answer box), and — the deliberate behavioural change this
	// feature ships — while a worker runs, where ⏎ stages the message as an interjection
	// (inputEditable; ADR 0025). Enter is handled above; everything else is an edit.
	if m.inputEditable() {
		// Backspace and Del take the SELECTION when the box holds one — the carve-out the chokepoint
		// above stashed the span for. The predicate is exactly what paints the highlight
		// (highlightInput, mouse.go): an active, non-empty span. Anything less would let a span the
		// human cannot see swallow the keypress — a release that copied nothing leaves the offsets
		// standing with active false, and a collapsed span is a caret, not a selection.
		//
		// The order against the empty-box case below is deliberate rather than incidental: a
		// selection implies a non-empty box, so the two can never both apply, and saying so here
		// keeps it true if either side ever moves.
		if sel.active && sel.anchorOff != sel.headOff {
			switch msg.String() {
			case "backspace", "delete":
				m.deleteSelection(sel)
				var reload tea.Cmd
				if m.state == stateIdle || m.state == stateRunning {
					m, reload = m.recomputeAutocomplete() // the value changed: re-derive the overlay, as a typed edit does
				}
				m.layout() // re-flow: the box shrinks as the cut unwraps rows
				return m, reload
			}
		}
		if m.input.Value() == "" && msg.String() == "backspace" {
			// Backspace on an empty input un-does the last thing staged: it lifts the newest queued
			// interjection back into the box. Nothing else is staged beside the text any more — a
			// skill is a /token IN it, deleted like any other word.
			if popped, reload, ok := m.popInterjection(); ok {
				return popped, reload
			}
		}
		// ↑/↓ walk this workspace's recorded prompts while the box is empty or holds an untouched
		// recalled entry (recall.go). It sits immediately before the fall-through because that is the
		// arrows' other duty — moving the caret — and recall may only take them where it has something
		// to show: recallKey claims nothing on a typed draft, at the ask, or with no entries at all, so
		// a box the human is working in never loses its cursor keys.
		if next, handled := m.recallKey(msg, rec); handled {
			return next, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.state == stateIdle || m.state == stateRunning {
			var reload tea.Cmd
			m, reload = m.recomputeAutocomplete() // re-derive the overlay from the edited input
			cmd = tea.Batch(cmd, reload)          // a "/" menu this keystroke opened owes a catalog re-scan, off the loop
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
	// The line VERBATIM, read before anything empties the box: what ↑ hands back has to be what the
	// human typed, @refs and /tokens included, not the parse of it (recall.go). Only the paths that
	// actually send or run record it — a refusal leaves the line in the box, where ↑ would be an
	// insult — and an empty box records nothing, which is also the "⏎ sends the held queue" case:
	// those rows were recorded when they were staged.
	sent := m.input.Value()
	var record tea.Cmd

	parsed := m.promptEditor.submitParse(m.knownSkillID)
	if parsed.kind == kindCommand {
		// A whole-input invocation IS the command line — nothing else was typed — so emptying the
		// box is exactly stripping the verb. runCommand does not touch the editor: its other caller,
		// the dropdown's accept path, cuts the token out and deliberately keeps the rest of the draft.
		m.promptEditor.reset()
		if parsed.recallable() {
			// A /command line is an input the human sent (decision 3) — except the session-reset
			// pair, whose noRecall flag keeps it out of the walk entirely (commandSpec).
			m, record = m.recordSend(sent)
		}
		next, cmd := m.runCommand(parsed)
		return next, tea.Batch(cmd, record)
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
	if m.prebound() {
		// There is no engine to send to yet, and the human has not been asked which server to build
		// one on since they closed the pane that asked (ADR 0036 decision 3). Ask again rather than
		// merely refuse: the ONE act standing between this message and the model is a keystroke on
		// the picker that comes back up, and the message stays in the box while they make it
		// (preboundRefusal).
		return m.preboundRefusal()
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
	// Where the /tokens sit in the text that is about to become the block — the parse's own offsets
	// for a plain send, re-based onto the composition when the held rows join it below.
	spans := parsed.skillSpans
	if held {
		// The held rows and the box become one unmarked user message (joinedInterjections), which
		// is what keeps the derived Exchange opening trivially correct: exactly one of them opens
		// the Exchange, whether the human sent one message or five.
		in, spans = m.joinedInterjections(parsed)
		m.pendingInterjections = nil
	}
	m.promptEditor.reset()         // empties the textarea, closes the overlay, clears the skill region
	m, record = m.recordSend(sent) // the send is committed: this line is recallable from here on
	m.detached = false             // a fresh prompt re-arms follow-the-tail: sending means "done reading history"
	m.transcript.addUser(in.Text, spans)
	// The first prompt of a fresh Session record also NAMES it: one cosmetic out-of-band completion
	// fired here, in parallel with the Exchange this prompt starts, so a single-slot server answers
	// it between Turns 1 and 2 (autotitle.go). It drives no engine and enters no transcript — it
	// simply rides along in the batch below, and is nil whenever nothing should fire.
	nameCmd := m.maybeAutoTitle(in.Text)
	m.layout() // the emptied input box shrinks back; the repaint follows the tail onto the new prompt
	next, cmd := m.launchExchange(in)
	return next, tea.Batch(cmd, nameCmd, record)
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
	m.setActivity(actStopping, "", 0, "")
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
	m.askChecked = nil // and with it the ticked set: no path leaves a dead checked set standing
	// A question that dies with its Exchange — a stop, a fault — lets go of the box exactly as an
	// answered one does, so the message it borrowed the box from comes back here too.
	m.restoreAskDraft()
	// The Exchange is over, so its mailbox has no reader left: drop it, and with it any row the
	// worker had not drained by the time it unwound. Nothing is lost — the display queue
	// (pendingInterjections) is the queue of record and still holds every undelivered row. What
	// happens to those rows next is the CALLER's ruling, not this one's: a natural completion
	// flushes them into a new Exchange (flushAfterCompletion), a stop or a fault holds them for
	// the next ⏎ (noteHeldQueue) — ADR 0025.
	m.box = nil
	m.setPlaceholder(m.idleLegend()) // nothing is running: ⏎ sends again
	m.genStart = time.Time{}
	// The worker has unwound, so the activity is over — including a sticky "stopping", which
	// only this path clears (activity.go). Idle renders an empty left slot.
	m.act = activity{}
	// Whatever the model was reasoning about died with the worker: a stop and a fault emit no
	// closing MessageEvent, so this is the boundary that keeps a cancelled Turn's reasoning from
	// outliving it (reasoning.go).
	m.reasoning.reset()
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

// busy reports whether a worker is in flight (running, blocked on an Approval, or blocked on
// an ask_user question) — the states in which a free submit is refused and the stop key
// cancels instead of quitting.
func (m Model) busy() bool {
	return m.state == stateRunning ||
		m.state == stateAwaitingApproval ||
		m.state == stateAwaitingAsk
}

// ----------------------------------------------------------------------------
// Layout
// ----------------------------------------------------------------------------

// The fixed chrome heights below the transcript. One blank gap row separates the transcript from
// whatever comes next — the bottom chrome, or a pane in the transcript-side overlay slot, which
// then seats flush on that chrome (layout.md, View); the ▔ top-edge hairline is one row; the
// status line is one row;
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
	// than eating it invisibly. A `/settings` edit of the key moves it mid-session (ADR 0037) and
	// lays out again from here, so the transcript re-wraps exactly once per deliberate change —
	// see bodyRightGutter (theme.go).
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
// dropped instead of left pointing at text that has moved. A COLLAPSED span — a press whose button
// is still down — is exempt and always survives: it shades nothing, and the release still needs the
// line the press named to toggle the block under it (spanUnchanged, mouse.go).
func (m *Model) refreshViewport() {
	rendered := m.transcript.renderView(m.th, m.transcriptWidth(), m.spin.blink())
	if !m.transcriptSel.spanUnchanged(m.lines, rendered.lines) {
		m.transcriptSel = transcriptSel{} // the ground under the span moved: let go (mouse.go)
	}
	m.lines = rendered.lines // stashed for the sticky-header overlay (View)
	m.userBlocks = rendered.userBlocks
	m.lineTargets = rendered.targets // the paint's own click surface, for the mouse (render.go)
	// The keyboard cursor stands on that same map, so it is re-seated against the paint that just
	// landed: this is the ONE place the map is restashed, and a highlight left on a line whose
	// meaning moved would be a ⏎ opening some other block (blockCursor.clamp, blockcursor.go).
	m.cursor = m.cursor.clamp(m.lineTargets)
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
// approval or ask prompt, the /sessions browser, the /model | /server picker, the /settings
// configuration pane, the /usage report, the autocomplete dropdown, and the staged-interjection
// strip. Each field is "" when its overlay is closed.
//
// They sit in two slots, and every one of them is FLUSH on the chrome its slot abuts: the first
// five go directly above the ▔ hairline (the frame's blank gap row falls above them, not between
// them and the chrome), the last two directly above the input box.
//
// They are gathered as ONE value because two readers need them and must never disagree: View
// composes them into the frame, and [Model.transcriptRows] measures them to say how many screen
// rows the transcript still owns — which is what the mouse maps a click through.
//
// How TALL each of them is drawn is settled before any of them is rendered, by the frame-wide row
// allocation they all share ([Model.frameRowPlan]): they are siblings spending the same viewport,
// not each the only thing in it.
type frameOverlays struct {
	prompt    string // the approval or the ask popup (they belong to different states, so never both)
	browser   string // the /sessions history browser
	picker    string // the /model | /server picker
	settings  string // the /settings configuration pane (full-height — frameRowPlan)
	usage     string // the /usage token-accounting report (usage.go)
	inspector string // the /inspect raw-protocol pane (inspector.go)
	dropdown  string // the command / @file / skill autocomplete
	queued    string // the staged-interjection strip (ADR 0025)
}

// height is the number of screen rows these overlays take from the transcript. An absent overlay
// is the empty string, which costs nothing — lipgloss.Height("") is 1, so the emptiness is tested
// rather than measured.
func (o frameOverlays) height() int {
	rows := 0
	for _, block := range []string{o.prompt, o.browser, o.picker, o.settings, o.usage, o.inspector, o.dropdown, o.queued} {
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
// the mouse mapping may each call it and are guaranteed the same answer. That guarantee is per Model
// VALUE and says nothing across two of them, which is exactly why the click chain snapshots one: a
// dismissal mid-chain yields a different model, and every rect asked of it after that would be a
// different frame's (handleMouseClick, mouse.go).
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
	o.settings = m.renderSettings()
	o.usage = m.renderUsage()
	o.inspector = m.renderInspector()
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

// frameBlocks is the most blocks one frame stacks — the transcript, the seven overlays, the blank
// gap row, the ▔ hairline, the status line, the input box, the footer, and the ▁ hairline — so the
// frame's slice is allocated once at its worst case.
const frameBlocks = 14

// View stacks the transcript, a single blank line, the ▔ top-edge hairline, the status line,
// the bordered input box, the footer line and the ▁ bottom-edge hairline, filling the alternate
// screen (layout.md). Before
// the first WindowSizeMsg there is no geometry to lay out, so it shows a minimal placeholder —
// on the alternate screen as well, so no frame apogee emits ever touches the primary one. The
// approval or ask prompt, when one is pending, sits between the blank line and the ▔ hairline — so
// it seats flush on the bottom chrome — as do the /sessions browser and the picker; the
// autocomplete dropdown and the staged-interjection strip sit just above the input box, flush on it
// in the same way. Every one of them takes its rows from the transcript
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
		body = m.highlightTranscript(body)  // overlay any transcript drag-selection on the composed rows
		body = m.highlightBlockCursor(body) // and the keyboard cursor's bar (blockcursor.go — they never coexist)
		if m.opts.HideScrollbar {
			rows = append(rows, body)
		} else {
			rows = append(rows, m.joinScrollbar(body, m.renderScrollbar(vp)))
		}
	}
	// The single blank line between chat content and what follows it (layout.md). It sits ABOVE the
	// transcript-side overlay slot, not below it, so a pane opened there seats FLUSH on the ▔
	// hairline rather than painting an empty row against the bottom chrome — the asymmetry the
	// dropdown and the staged band never had, since they already hug the input box. With no pane
	// open the frame is unchanged: the blank row still lands directly above the ▔. Either way the
	// frame spends exactly one gap row, so the row arithmetic (frameFixedRows, transcriptBudget)
	// does not know or care which side of the slot it falls on.
	rows = append(rows, "")
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
	// The /settings pane shares that slot too, and on every window it is seated in it is the only
	// thing in it: its verb is idle-only and it swallows every key, so no prompt, browser, picker or
	// dropdown can be up beside it. What differs is its height — it was granted the transcript's
	// whole budget (frameRowPlan), so the block above it is usually nothing at all.
	if ov.settings != "" {
		rows = append(rows, ov.settings)
	}
	// The /usage report shares the slot as well, and it is the one pane there that can be up beside
	// another: its verb is whileRunning, so it can be opened over an approval or ask prompt the run
	// is blocked on. It goes last in the slot — nearest the chrome — so the surface the human is
	// answering keeps the position it has when the report is not up.
	if ov.usage != "" {
		rows = append(rows, ov.usage)
	}
	// The /inspect pane closes the slot, under the report it is shaped after: its verb is
	// whileRunning too, so it is the other pane that can be up beside a prompt the run is blocked on,
	// and the surface the human is answering keeps its position when the raw-protocol view is opened
	// over it.
	if ov.inspector != "" {
		rows = append(rows, ov.inspector)
	}
	// Then the ▔ top-edge hairline capping the chrome, the status line, the autocomplete overlay
	// (when open), the input box, the footer, and the ▁ hairline closing the screen under it.
	rows = append(rows, m.topRule(), m.statusLine())
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

// squareOnField is squareLine with the pad painted on field rather than left bare — the same split
// lipgloss makes between the run it styles as text and the run it styles as whitespace
// (lipgloss/v2@v2.0.4/align.go:12-56). It matters wherever the surface is a solid field: a content
// line carries an SGR reset of its own at its end, so a bare pad after it would show the terminal's
// background through the gap between the text and the box's right edge.
func squareOnField(measure widthAuthority, field lipgloss.Style, s string, w int) string {
	over := measure.Width(s) - w
	if over > 0 {
		return measure.Truncate(s, w, "")
	}
	return s + field.Render(strings.Repeat(" ", -over))
}

// drawBox frames content in style's border and horizontal padding and returns the box's rows: one
// string per PAINTED row, each exactly width columns in the authority's measure, ONE row per line of
// content it was handed.
//
// It stands in for style.Width(width).Render(…), and for squareLine's reason one level up. A
// lipgloss Width does not merely pad: past its width it WRAPS, and it measures in GraphemeWidth
// whatever the painter is doing (ADR 0030 §5). So a composed row the authority calls exactly the
// box's inner width can be wider than that to lipgloss — any VARIATION SELECTOR-16 cluster makes it
// so — and the style folds that ONE composed row into TWO painted rows: the row's tail moves to a
// line of its own, the box grows past the row budget it was drawn for, and the fold leaves rows that
// stop short of the box's right border. Drawing the rows here keeps one composed row one painted row
// at every width, under either measure.
//
// Only the shape the two boxed surfaces use is honoured — a four-sided border, horizontal padding,
// and the field colour that padding sits on — because that is the whole of what theme.go's
// startupBorder and popupBorder carry. Where the width cannot pay for the padding the PADDING gives
// way before the border does, so a box that is drawn at all is exactly width columns wide; narrower
// than its own two border glyphs there is no box to draw and the answer is no rows.
//
// A surface that would rather carry its name IN the top border than on a row of its own asks
// drawTitledBox for it; this is that same function with nothing to splice.
func drawBox(measure widthAuthority, style lipgloss.Style, content []string, width int) []string {
	return drawTitledBox(measure, style, content, width, "", lipgloss.NewStyle())
}

// drawTitledBox is drawBox with a name spliced INTO the top border — ╭────── Approve terminal? ──────╮
// — for a surface that would rather spend the row on what the human is deciding about than on a
// heading of its own (popupSpec.titleInBorder). The title is centred between two runs of the
// border's own rune with one space each side, so the box still closes itself at exactly width
// columns: the corners, the two dash runs, and the label are measured in the authority the rest of
// the box is drawn in, never in lipgloss's (ADR 0030). An EMPTY title is the plain border drawBox
// has always drawn, which is what makes drawBox exactly this function with nothing to splice — one
// border assembly, so a titled box and a plain one cannot drift apart. A title too wide for the
// span is dropped rather than overflowed; every caller fits it first (renderPopup composes it TO
// the pane's inner width), so the fallback is a backstop and not a silent elision.
//
// titleStyle is how the name is painted; it is re-backgrounded onto the top border's own field so
// the spliced label carries a background like every other cell of the box (the pane is filled
// solid black — a bare cell in the middle of the border would read as a hole in it).
func drawTitledBox(measure widthAuthority, style lipgloss.Style, content []string, width int, title string, titleStyle lipgloss.Style) []string {
	edges := style.GetHorizontalBorderSize()
	if width < edges {
		return nil
	}
	border := style.GetBorderStyle()
	span := width - edges // the columns between the two vertical border glyphs
	padLeft := min(style.GetPaddingLeft(), span)
	padRight := min(style.GetPaddingRight(), span-padLeft)
	inner := span - padLeft - padRight

	// Each edge takes its own colours off the style, and the padding takes the box's field, so a
	// transparent card (startupBorder) and a solid-black pane (popupBorder) both come out of the one
	// routine. An unset colour reads back as lipgloss.NoColor, which renders as no sequence at all.
	top := lipgloss.NewStyle().Foreground(style.GetBorderTopForeground()).Background(style.GetBorderTopBackground())
	bottom := lipgloss.NewStyle().Foreground(style.GetBorderBottomForeground()).Background(style.GetBorderBottomBackground())
	left := lipgloss.NewStyle().Foreground(style.GetBorderLeftForeground()).Background(style.GetBorderLeftBackground())
	right := lipgloss.NewStyle().Foreground(style.GetBorderRightForeground()).Background(style.GetBorderRightBackground())
	field := lipgloss.NewStyle().Background(style.GetBackground())

	lead := left.Render(border.Left) + field.Render(strings.Repeat(" ", padLeft))
	tail := field.Render(strings.Repeat(" ", padRight)) + right.Render(border.Right)

	topRow := top.Render(border.TopLeft + strings.Repeat(border.Top, span) + border.TopRight)
	if title != "" {
		label := " " + title + " " // one space each side: the name is set off the border, not welded to it
		if w := measure.Width(label); w <= span {
			dashes := (span - w) / 2 // centred, an odd cell going to the right — the mockup's own drawing
			topRow = top.Render(border.TopLeft+strings.Repeat(border.Top, dashes)) +
				titleStyle.Background(style.GetBorderTopBackground()).Render(label) +
				top.Render(strings.Repeat(border.Top, span-w-dashes)+border.TopRight)
		}
	}

	rows := make([]string, 0, len(content)+2) //nolint:mnd // +2: the top and bottom border rows
	rows = append(rows, topRow)
	for _, ln := range content {
		rows = append(rows, lead+squareOnField(measure, field, ln, inner)+tail)
	}
	return append(rows, bottom.Render(border.BottomLeft+strings.Repeat(border.Bottom, span)+border.BottomRight))
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
// paints the resolving "/id" and @file tokens first (inputaccent.go), then highlightInput paints
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
	marker := popupElisionMarkerFitting(m.th, hidden, budget)
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
// "The status line's right slot"). The mode marker takes its own per-mode colour (theme.modeColor), so
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
	mode := m.th.footerText.Foreground(m.th.modeColor(m.opts.Mode)).Render(modeMarker(m.opts.Mode)) +
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
		left += m.th.footerText.Foreground(m.th.errorFg).Render(offline)
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
		Context: format.Tokens(opts.ContextWindow),
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
// theme.go): ⊞ plan, ◐ ask before, ✔ allow edits, ⏵⏵ auto. An unknown mode has no glyph — an
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

	// qualified is the running slot composed WITH the stall guard's `quiet` qualifier — the same row
	// one rung richer — and "" in every frame and every state that owes nothing (activity.quiet).
	// The qualifier sits BETWEEN the activity phrase and its clock, so it cannot be a fragment the
	// width test appends after the fact: the slot is composed both ways and the width below picks
	// one, which is also what drops the qualifier whole rather than truncating it — see below.
	var phrase, qualified string
	switch m.state {
	case stateIdle:
		// Idle says nothing for itself — the input box below already invites a message — with TWO
		// exceptions. An open /settings pane the frame could not seat leaves its fact here, the licence
		// layout.md gives every surface that gives way entirely (settingsGiveWayPhrase); and a session
		// with no server bound carries its standing fact here for as long as that holds, which is what
		// survives the esc that closes the pane asking about it (preboundPhrase, ADR 0036). The pane's
		// fact wins where both are owed: a modal swallowing every key on a window showing none of it is
		// the more urgent of the two, and the session's fact is still there when it closes. Every other
		// idle frame leaves the slot to the queue, as before.
		phrase = m.settingsGiveWayPhrase()
		if phrase == "" {
			phrase = m.preboundPhrase()
		}
	case stateRunning:
		now := time.Now()
		spinner, throughput := m.spin.view(m.th)+m.th.statusBar.Render(" "), m.throughputSuffix()
		phrase = spinner + m.runningPhrase(now, false) + throughput
		if m.act.quiet(m.lastEvent, now, m.opts.StallAfter) {
			qualified = spinner + m.runningPhrase(now, true) + throughput
		}
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
	// The quiet qualifier is the FIRST thing the slot gives up, one rung below the phrase it
	// qualifies: a row too tight for the qualified form falls back to the plain one, so the
	// qualifier goes whole and never truncates into "· quie…", which would report neither the
	// silence nor its own word and would cost the phrase its last columns to say so.
	if qualified != "" && m.th.measure.Width(qualified) <= room {
		phrase = qualified
	}
	if m.th.measure.Width(phrase) <= room {
		return lead + phrase + queued
	}
	if room <= 1 {
		return lead + m.queuedSegment(false)
	}
	return lead + m.th.measure.Truncate(phrase, room, "…") + queued
}

// runningPhrase composes the running left slot's styled text: the activity phrase, the stall
// guard's bare `quiet` qualifier when quiet says the row owes it, and the clock counting THIS
// activity — "thinking · quiet · 2m 59s" while the engine is silent, "thinking · 12s" in every
// frame that owes nothing. There is ONE clock either way, and it is the ACTIVITY's: it counts
// continuously and never jumps backwards when the qualifier appears or clears. The guard's own
// span is not shown at all, because in the case it was built for — an engine that never spoke
// after the request went out — the two spans are the same one, and the row stated it twice.
//
// The qualifier's fragment, its separator included, wears the amber statusWarning; the phrase and
// the clock keep the bar's own field, since the clock belongs to the activity rather than to the
// guard. Amber and deliberately not the red statusError the errored state keeps for itself: this
// qualifies a phrase that is still running, it does not announce a fault (theme.go). The qualifier
// disappears by itself the moment an Event lands, since the clock activity.quiet reads is restamped
// there (Model.lastEvent).
//
// An activity with no phrase — the defensive case, since every path into stateRunning sets one —
// degrades to the bare clock rather than to a dangling separator, and can never be the qualified
// case: activity.quiet speaks only for thinking and responding, and both always word themselves.
// now is passed in so the composition is testable off the wall clock.
//
// This is the seam where the acting delegation's NAME is resolved: activity carries the spawning
// call id, the transcript owns the run heads, and the phrase itself stays pure (activity.go). The
// lookup happens per frame rather than at fold time on purpose — the name is display identity that
// the head may not have folded yet, and an unresolved one costs a fallback word, not a wrong word.
func (m Model) runningPhrase(now time.Time, quiet bool) string {
	clock := formatElapsed(m.act.elapsed(now))
	phrase := m.act.text(m.transcript.runName(m.act.spawn))
	switch {
	case phrase == "":
		return m.th.statusBar.Render(clock)
	case quiet:
		return m.th.statusBar.Render(phrase) + m.th.statusWarning.Render(quietQualifier) + m.th.statusBar.Render(" · "+clock)
	default:
		return m.th.statusBar.Render(phrase + " · " + clock)
	}
}

// quietQualifier is the stall guard's whole surface: the word, and the separator that hangs it off
// the running phrase (runningPhrase). It says nothing about how long, and nothing about what the
// silence means — a 43k-token prompt is legitimately silent for a minute or two, so the honest
// fact is that nothing has arrived, never a verdict about it.
const quietQualifier = " · quiet"

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
	prefix := th.statusBar.Render(fmt.Sprintf("%s/%s %d%% ", format.Tokens(c.Used), format.Tokens(c.Limit), pct))
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
		b.WriteString(th.gaugeFill.Background(th.chrome).Render(string(gaugeEighths[rem-1])))
	}
	if empty > 0 {
		b.WriteString(th.gaugeTrack.Render(strings.Repeat(" ", empty)))
	}
	return b.String()
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
//
// The /usage report sits at the transient end beside the dropdown, and above it: it is a pane
// nothing is being decided on — a question already answered, dismissed by the esc that is its only
// key — so it yields to every surface the human is acting IN, while the dropdown, which the next
// keystroke re-derives anyway, yields before it.
//
// The /inspect pane sits between the two, and yields before the report: both are passive reads, and
// what separates them is what a lost row costs. The report is a dozen rows of totals that stop
// being a report the moment they are cut, while the raw-protocol view is a window onto a ring that
// keeps its records whether or not the pane is drawn — reopened on a taller window, it says exactly
// what it would have said.
type framePane int

const (
	panePrompt    framePane = iota // the approval or the ask prompt
	paneBrowser                    // the /sessions history browser
	panePicker                     // the /model | /server picker
	paneSettings                   // the /settings configuration pane — the frame's one full-height pane
	paneUsage                      // the /usage token-accounting report
	paneInspector                  // the /inspect raw-protocol pane
	paneDropdown                   // the command / @file / skill autocomplete
	paneKinds                      // not a pane: the count, so a plan can hold one grant per kind
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
	if m.settings.open {
		s = s.with(paneSettings)
	}
	if m.usagePane.open {
		s = s.with(paneUsage)
	}
	if m.inspector.open {
		s = s.with(paneInspector)
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
// panes, the dropdown before the settings pane, the browser or the picker, and the modal prompt
// last. The one pane that claims more than an even share of the surplus is /settings, and it claims
// it from the TRANSCRIPT rather than from a sibling: while it is open the conversation keeps no
// reserve, which is the whole of the full-height rule (below). The input box
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

	// The transcript's soft reserve is the one thing the FULL-HEIGHT pane changes: while /settings is
	// open the conversation keeps nothing at all, so every row past the pane floors and the band's
	// claim goes to the panes (layout.md, "One pane may claim the whole budget"). It is a reserve and
	// never a floor — the transcript is first in the give-way order and already goes to nothing on a
	// short window — so dropping it here shifts rows between two surfaces that were both giving way,
	// and leaves every pane floor, the band's shape and the frame's own chrome exactly where they are.
	reserve := transcriptReserve
	if open.has(paneSettings) {
		reserve = 0
	}

	if growth := max(0, left-reserve); growth > 0 && seated > 0 {
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
// pushes the input box off-screen (D2). rows is what the pane's whole row list demands and rowCap
// the overlay's own cap on how much of it it shows at once — both in the LINES that window paints
// (popupSpec.maxRows' own denomination), which is the row COUNT exactly while every row is one line
// and nothing is set off from anything: a spec that wraps its rows, gaps them or pads the block
// states its demand through popupRowBlockLines instead (approvalPrompt, askPrompt). Both are 0 for a
// pane with no rows on offer. maxRows caps the scrolled row window and maxBody the wrapped body
// block, both ≥ 0 and both in renderPopup's zero-means-none sense. seated is false when the frame's
// allocation left this pane no rows at all — a window too short to seat it beside its siblings —
// and the overlay renders nothing rather than a pane drawn past the terminal's last row.
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
// Inside its grant the pane spends its chrome first — whatever frame the spec it is about to compose
// actually draws, which is the chrome parameter and not always the four rows of a titled, hinted
// pane — gives the ROWS priority (they are what the human acts on) and lets the body have what is
// left, overflowing into the explicit "… (+N more lines)" marker.
//
// floor is the ONE thing that comes off ahead of the rows, and it is the caller's for the same reason
// chrome is (popupFloor). Rows-first is right where the rows are the pane and the prose above them is
// a caption — every pane but one passes the zero floor and is budgeted exactly as it always was — and
// wrong where the prose is the thing being decided about: the ask prompt's four answers, the blanks
// between them and the pad around the block ask for nine lines of the ten an eighty-by-twenty-four
// window grants it, so rows-first left the question itself as a count on a terminal with room to
// spare. A body claim (popupBodyLineCount, capped by the pane's own taste) is taken off the top
// instead, bounded by what leaves the rows their anchor row, so the shrink ladder below is
// unchanged at the heights where it bites and the surplus above them goes to the question rather
// than to the breathing room around the answers. It is a claim on the grant and never a promise
// past it — it is spent out of avail like everything else, so both caps still floor at ZERO on a
// window that cannot pay for either, which is the paragraph further down and the whole reason a
// floor here is safe.
//
// chrome is what that frame costs the pane, and it is the CALLER's because only the caller knows the
// spec it is about to compose. All three constants are in use: popupChrome where the title takes a
// content row of its own and a hint row spells the keys below the list (the /sessions browser, the
// picker, the /settings pane, the autocomplete dropdown); popupTitleBorderChrome where no title row is drawn and the
// hint alone keeps one — its only caller is the ask prompt, which draws no title at ALL, its name
// having gone into the question itself (layout.md), so what titleInBorder buys there is a top
// border left plain by an empty title, carrying the question only at the heights that seat no line
// of it (popupSpec.titleFromBody); and popupBorderChrome where neither row is drawn — the approval
// prompt's tool name does ride its top border and its shortcut letters are written beside the
// options they take, so its two borders are its whole frame (approvalPrompt). Each spends the row
// it saved on the pane's own content rather than leaving it unclaimed. chrome is also what "seated"
// is measured against, so a pane is refused only on a grant its own chrome cannot fit in.
//
// BOTH caps floor at ZERO rather than at a comfortable minimum, and that is the point of them: a
// row floor of 6 on a window with 4 rows to give promised a pane the frame could not hold, and the
// surplus came off the bottom — the input box and the footer, off the alt screen. A body floor of
// 1 was the same mistake one row smaller: it made a body-bearing pane's irreducible height 5 where
// a boxed overlay's chrome is 4, so the ask and approval prompts overflowed the shortest window a
// pane fits in at all (12 rows — the fixed chrome below the transcript is 8) by exactly one row
// while the browser fitted. A budget that shrinks to nothing lets the TRANSCRIPT shrink to nothing
// instead, which is the outcome D2 asks for.
//
// WHICH nothing a pane reaches at the four-row floor follows from the chrome it spends, because the
// arithmetic below leaves the rows at most avail−1 lines and the body whatever that did not take. A
// popupChrome pane is down to BOTH zeros there: it still names itself in its title row and says how
// to act in its hint row, and everything it dropped — prose and rows alike — is counted onto that
// title (popupTitleLine). A popupTitleBorderChrome pane keeps one body line, so the ask prompt's
// question holds a row while an offering with no window left rides the border instead — and where the
// question is too long for that one row, the row goes to the marker and the question falls back onto
// the border beside it (popupSpec.titleFromBody), because a pane's identity may shrink to its lead
// but never to a count. A popupBorderChrome pane keeps two, one body line and one
// row, so the approval menu is in neither case: its count stays in its body and a decision stays on
// the screen. Shrinking costs the prose, and the rows outside the window, but never the fact that
// there are some.
func (m Model) popupBudget(p framePane, rows, rowCap, chrome int, floor popupFloor) (maxBody, maxRows int, seated bool) {
	granted := m.frameRowPlan(m.openPanes().with(p)).panes[p]
	if granted < chrome {
		return 0, 0, false
	}
	avail := granted - chrome
	// The body's claim comes off the top, between the two bounds that keep the arbitration honest at
	// both ends of the ladder: never less than the one line the body has always kept (which is what
	// makes the zero floor today's rule exactly), and never more than what leaves the rows the lines
	// their anchor row costs — so prose can take a roomy window's surplus without emptying a decision
	// surface on a short one. Past that the rows are first, as they always were.
	reserve := clampInt(floor.body, 1, max(1, avail-max(1, floor.rows)))
	maxRows = min(rows, rowCap, max(0, avail-reserve))
	maxBody = max(0, avail-maxRows)
	return maxBody, maxRows, true
}

// popupScrollbarOn is the answer every popup spec carries in popupSpec.scrollbar: the human's
// `ui.show-scrollbar` (Options.HideScrollbar, the inverted form the composition root passes),
// read HERE rather than inside the popup module, which is not given a Model to read it from.
//
// One switch covers both bars because it is one preference. The key is about whether apogee draws
// an indicator down a right-hand column at all — a reader who took the transcript's away did not
// ask to keep the same stroke beside a list — so a pane asks the same question of the same field
// and gets the same answer, and no popup has a switch of its own to fall out of step with.
//
// It says nothing about OVERFLOW: a spec that sets the flag and fits paints no bar and gives up no
// column (popupRowLines). What this decides is only whether the bar is available to the pane, and
// stamping it at every construction site is what makes "every overflowing popup" true by
// construction rather than by a list of panes someone has to remember to extend.
func (m Model) popupScrollbarOn() bool { return !m.opts.HideScrollbar }
