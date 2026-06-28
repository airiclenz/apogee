package tui

import (
	"context"
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
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
	stateRunning                         // a worker drives an Exchange; input is refused
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
	parent context.Context // the program's context; the worker derives from it (C4)
	eng    Engine
	opts   Options
	save   func(domain.Session) error // persists a snapshot on a clean quit; nil ⇒ off

	// Sub-models (Bubbles widgets).
	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	// Lifecycle.
	state      uiState
	cancel     context.CancelFunc // non-nil while a worker runs; the stop key calls it (C4)
	pending    *approvalReqMsg    // the in-flight Approval while awaitingApproval (P2.4 acts on it)
	pendingAsk *askReqMsg         // the in-flight ask_user question while awaitingAsk (P3.11)
	lastErr    error              // the error behind stateErrored, shown in the status line
	lastCtrlC  time.Time          // when the last Ctrl+C landed; a second within the window quits

	// autocomplete is the chat mini-language suggestion overlay shown while typing at idle
	// (commands on "/", workspace files on "@", skills on "/skill"). The zero value is hidden.
	autocomplete autocompleteState

	// files memoises the workspace listing behind the "@" autocomplete so a typing burst reuses
	// one filesystem walk (filecache.go). A pointer — shared across the value-copied Model so
	// the cache survives each Update (ADR 0011); nil-safe (fileSuggestions falls back).
	files *fileCache

	// pendingSkills are the skill IDs attached via the /skill picker, awaiting the next submit
	// (which copies them into UserInput.SkillIDs and clears them). A plain []string — a
	// reference header, safe in the value-copied Model (ADR 0011) — rendered as chips above the
	// input. Backspace on an empty input pops the last one.
	pendingSkills []string

	// Live stats folded from the engine's UsageEvent (server token accounting). ctxUsed is
	// the latest top-level (Depth 0) total-token count, driving the context-usage gauge;
	// genStart marks when the current Turn began streaming content (set on its first token,
	// cleared on a re-stream or once usage lands) and tokPerSec is the last completion's
	// throughput, timed against the Update clock. All value/zero-safe in the copied Model.
	ctxUsed   int
	genStart  time.Time
	tokPerSec float64

	// Content & layout.
	transcript    transcript
	th            theme // the palette and reusable styles, built once at construction
	width, height int
	ready         bool // a WindowSizeMsg has sized the layout at least once
	userScrolled  bool // the human scrolled the transcript; suspend sticky-to-top until submit

	// Last render output, stashed by refreshViewport for View's sticky-header overlay: the
	// physical lines the viewport holds and the line range of every user block.
	lines      []string
	userBlocks []userBlock
}

// newModel builds the initial idle Model. parent is the program context the worker derives
// its cancellable child from (so a program-wide shutdown also cancels an in-flight
// Exchange — C4). The input box is focused here, not in Init, because Init returns only a
// Cmd: the focus *state* must be set on the stored widget, while Init returns the cursor's
// blink Cmd.
func newModel(parent context.Context, eng Engine, opts Options) Model {
	th := newTheme()

	ta := textarea.New()
	ta.Placeholder = "Send a message…  ⏎ send · ⇧⏎/⌥⏎ newline · ⌃c quit"
	ta.Prompt = "" // the rounded border is the frame; no inline prompt gutter (layout.md)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit; the model, not the widget, bounds a turn
	// Plain Enter submits (intercepted in handleKey), so the textarea's newline binding is
	// repurposed: shift+enter works on terminals that support the Kitty keyboard protocol,
	// and alt+enter / ctrl+j are byte-distinct fallbacks that insert a newline everywhere.
	ta.KeyMap.InsertNewline.SetKeys("shift+enter", "alt+enter", "ctrl+j")
	blackenInput(&ta)
	ta.Focus()

	vp := viewport.New()
	vp.SoftWrap = true // wrap long transcript lines to the viewport width

	sp := newBrailleSpinner()
	sp.Style = lipgloss.NewStyle().Background(colBlack) // match the status bar's black field

	return Model{
		parent:   parent,
		eng:      eng,
		opts:     opts,
		save:     opts.Save,
		input:    ta,
		viewport: vp,
		spinner:  sp,
		th:       th,
		state:    stateIdle,
		files:    &fileCache{},
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

// Init starts the cursor blink. The window is sized by the first WindowSizeMsg the program
// sends; nothing else needs an initial Cmd (the spinner ticks only while running).
func (m Model) Init() tea.Cmd {
	return m.input.Focus()
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
		m.layout()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case ctrlCResetMsg:
		// The Ctrl+C quit window elapsed without a second press: disarm the gesture so the
		// "press ctrl+c again to quit" hint clears (handleKey's ctrl+c case).
		m.lastCtrlC = time.Time{}
		return m, nil

	case eventMsg:
		m = m.foldStats(msg.Event)
		m.transcript.apply(msg.Event)
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
		m.input.Reset()
		m.layout()
		return m, m.input.Focus()

	case exchangeDoneMsg:
		m.finishWorker(stateIdle)
		return m, nil

	case cancelledMsg:
		// The worker cancelled at a quiescent boundary and has returned, so the engine is the
		// Update loop's to touch again (C1). Discard the interrupted Exchange so the engine
		// leaves its open-Exchange state: without this the Agent stays inExchange after a cancel
		// and the next /clear or message is rejected with ErrInputPending — the post-Esc wedge.
		// The visible transcript is untouched (the "cancelled" note and any streamed partial
		// stay in scrollback); only the model's memory drops the scrapped Exchange.
		m.eng.AbortExchange()
		m.transcript.addNote("cancelled")
		m.finishWorker(stateIdle)
		m.refreshViewport()
		return m, nil

	case errMsg:
		m.lastErr = msg.Err
		m.transcript.addError("loop", msg.Err.Error(), 0)
		m.finishWorker(stateErrored)
		m.refreshViewport()
		return m, nil

	case spinner.TickMsg:
		// Keep the chain alive only while running; dropping the tick when idle lets it
		// die naturally (the spinner's tag mechanism prevents a doubled chain on restart).
		if m.state == stateRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.MouseWheelMsg:
		// The wheel scrolls the transcript in every state — unlike the keyboard path, which is
		// state-gated (idle/ask feed the input). Mouse reporting is enabled in View
		// (MouseModeCellMotion); the viewport's own Update turns the wheel into a scroll.
		return m.scrollViewport(msg)

	default:
		// Other Bubble Tea Msgs (e.g. the cursor blink) belong to the widgets; route them
		// to the focused input so the cursor keeps blinking.
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
// is otherwise a no-op); Ctrl+C twice within ctrlCQuitWindow quits. Enter submits at idle and
// is a no-op while a worker runs (the single-worker invariant the seam relies on). Other keys
// feed the input while idle, or scroll the transcript while busy.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// While the autocomplete overlay is open (idle only), it claims the navigation, accept,
	// and dismiss keys — including enter and tab — before the normal routing below. Any other
	// key returns handled=false and falls through to edit the input (which re-derives it).
	if m.state == stateIdle && m.autocomplete.active {
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
		case stateAwaitingAsk:
			// Submit the typed answer back to the blocked ask_user tool (P3.11).
			return m.submitAnswer()
		case stateErrored:
			// Dismiss the error and return to idle so the next message can be sent.
			m.lastErr = nil
			m.state = stateIdle
			return m, nil
		default:
			return m, nil // no-op while running / awaiting approval
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

	// While awaiting an ask_user answer the input box is live so the human types the reply —
	// the same editing path as idle (enter, handled above, submits it).
	if m.state == stateIdle || m.state == stateAwaitingAsk {
		// Backspace on an empty input pops the last attached skill chip (idle only) — so a chip
		// is removed the same way a typed character is, once the message field is empty.
		if m.state == stateIdle && len(m.pendingSkills) > 0 && m.input.Value() == "" && msg.String() == "backspace" {
			m.pendingSkills = m.pendingSkills[:len(m.pendingSkills)-1]
			m.layout()
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.state == stateIdle {
			m.autocomplete = m.computeAutocomplete() // re-derive the overlay from the edited input
		}
		m.layout() // re-flow: the input box auto-grows as the message wraps to more rows
		return m, cmd
	}
	// While running, let the keys scroll the transcript rather than edit the (refused) input;
	// a scroll that actually moves the viewport suspends sticky-to-top until the next submit.
	return m.scrollViewport(msg)
}

// scrollViewport routes a key to the viewport and records a human scroll: if the offset
// moved, sticky-to-top is suspended (refreshViewport stops re-pinning the last user prompt)
// so reading history is not yanked back as new content streams in.
func (m Model) scrollViewport(msg tea.Msg) (tea.Model, tea.Cmd) {
	before := m.viewport.YOffset()
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	if m.viewport.YOffset() != before {
		m.userScrolled = true
	}
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
		return m, m.spinner.Tick
	}
	return m.scrollViewport(msg)
}

// submit parses the input through the chat mini-language and routes it: a recognised
// /command goes to runCommand; anything else is a message sent to the agent (with its @file
// references extracted for the loop to resolve). It records the user message, switches to
// running, stores the worker's CancelFunc (C4), and batches the worker Cmd with the spinner
// tick. A blank message is ignored. Only reachable from stateIdle, so the single-worker
// invariant holds.
func (m Model) submit() (tea.Model, tea.Cmd) {
	parsed := parseInput(m.input.Value())
	if parsed.kind == kindCommand {
		return m.runCommand(parsed.command)
	}
	// Nothing to send only when there is neither text NOR an attached skill: an empty message
	// with skills attached is a valid send (the skill bodies are the payload).
	if parsed.text == "" && len(m.pendingSkills) == 0 {
		return m, nil
	}
	attached := m.pendingSkills
	m.input.Reset()
	m.autocomplete = autocompleteState{}
	m.pendingSkills = nil
	m.userScrolled = false // a fresh prompt re-arms sticky-to-top
	m.transcript.addUser(parsed.text, m.skillDisplayNames(attached))
	m.layout() // the emptied input box shrinks back; the new prompt pins to the top

	cmd, cancel := startExchange(m.parent, m.eng,
		domain.UserInput{Text: parsed.text, FileRefs: parsed.fileRefs, SkillIDs: attached})
	m.cancel = cancel
	m.state = stateRunning
	return m, tea.Batch(cmd, m.spinner.Tick)
}

// skillDisplayNames resolves the attached skill IDs to their display names (falling back to the
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
		names = append(names, name)
	}
	return names
}

// runCommand handles a recognised local /command from the idle state. /continue is the one
// command that opens an agent turn (a canned "Please continue"); /clear and /compact act on
// the engine's context and stay idle, recording a transcript note. The input box and the
// autocomplete overlay are cleared either way. Reached only from submit (stateIdle), so the
// engine is quiescent — no worker owns it — and ClearContext/Compact are safe to call here.
func (m Model) runCommand(command string) (tea.Model, tea.Cmd) {
	m.input.Reset()
	m.autocomplete = autocompleteState{}

	switch command {
	case "continue":
		// /continue carries any attached skills into the canned turn (the user lined them up
		// before asking the model to keep going).
		attached := m.pendingSkills
		m.pendingSkills = nil
		m.userScrolled = false
		m.transcript.addUser("/continue", m.skillDisplayNames(attached))
		m.layout()
		cmd, cancel := startExchange(m.parent, m.eng,
			domain.UserInput{Text: "Please continue", SkillIDs: attached})
		m.cancel = cancel
		m.state = stateRunning
		return m, tea.Batch(cmd, m.spinner.Tick)

	case "clear":
		// Clearing the model's memory also drops the staged chips — they belonged to the turn
		// being abandoned.
		m.pendingSkills = nil
		if err := m.eng.ClearContext(); err != nil {
			m.transcript.addNote("could not clear context: " + err.Error())
		} else {
			m.transcript.addNote("context cleared — the model's memory of this session is reset")
		}
		m.layout()
		return m, nil

	case "compact":
		// A stub until the generative reducer lands; surface whatever Compact reports. When it
		// becomes a real upstream call it must move onto a worker goroutine (like startExchange)
		// so it does not block the Update loop. Staged chips are dropped (the turn is reset).
		m.pendingSkills = nil
		if err := m.eng.Compact(m.parent); err != nil {
			m.transcript.addNote("compact: " + err.Error())
		} else {
			m.transcript.addNote("context compacted")
		}
		m.layout()
		return m, nil
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
	answer := strings.TrimSpace(m.input.Value())
	m.pendingAsk.Reply <- domain.AskAnswer{Text: answer}
	m.pendingAsk = nil
	m.input.Reset()
	m.state = stateRunning
	m.layout()
	return m, m.spinner.Tick
}

// stopWorker cancels the in-flight worker. The worker honours the cancel at the next
// quiescent boundary and returns a cancelledMsg, which clears the state; until then the
// model stays running. A cancelled approval gate unblocks the same way (C3/C4).
func (m *Model) stopWorker() {
	if m.cancel != nil {
		m.cancel()
	}
}

// finishWorker returns the model to a terminal state once the worker's terminal Msg
// arrives: it clears the CancelFunc and any pending Approval or ask_user question. The new
// state is idle for a completed or cancelled Exchange, errored for a loop fault.
func (m *Model) finishWorker(next uiState) {
	m.cancel = nil
	m.pending = nil
	m.pendingAsk = nil
	m.state = next
}

// quit ends the program. On a clean quit — idle or errored, where the worker has already
// returned — it first snapshots the conversation to the host saver; the Agent is
// single-goroutine, so reading it from the Update goroutine is only safe once no worker
// owns it (the busy() states). While busy it cancels the in-flight worker instead (so its
// goroutine unwinds rather than racing a snapshot) and quits without saving — snapshotting
// the last boundary mid-run is deferred (plan §6.1; handoff 16).
func (m Model) quit() (tea.Model, tea.Cmd) {
	if m.busy() {
		m.stopWorker()
		return m, tea.Quit
	}
	m.saveSession()
	return m, tea.Quit
}

// saveSession best-effort persists a snapshot of the conversation through the host saver.
// It is a no-op without a saver or with an empty transcript (nothing worth resuming).
// Both Snapshot and the save are best-effort: a quit must never fail, so an error is
// swallowed rather than blocking the exit. The caller guarantees no worker is running, so
// calling Snapshot here respects the Agent's single-goroutine contract (C1).
func (m Model) saveSession() {
	if m.save == nil || len(m.transcript.entries) == 0 {
		return
	}
	sess, err := m.eng.Snapshot()
	if err != nil {
		return
	}
	_ = m.save(sess)
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

// The fixed chrome heights below the transcript. The status line is one row; one blank gap
// row separates the transcript from the chrome (layout.md); the footer is three rows (its
// top divider, its content line, its bottom rule). The input box and the viewport take what
// remains — the box grows with its content, the viewport gets the rest.
const (
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
// with its content (clamped), and the viewport gets the height left after the status row, the
// gap row, the input box (its content rows plus a top border — the divider below it belongs to
// the footer), and the footer. A floor of one row keeps the viewport valid on a tiny window.
func (m *Model) layout() {
	m.viewport.SetWidth(max(1, m.width-scrollbarWidth)) // reserve the scroll-bar gutter column
	m.input.SetWidth(m.inputInnerWidth())
	m.input.SetHeight(m.inputRows())

	inputBoxHeight := m.input.Height() + 1 // content rows + top border (no bottom — it is the footer's divider)
	vpHeight := m.height - statusHeight - gapHeight - inputBoxHeight - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.SetHeight(vpHeight)
	m.refreshViewport()
}

// inputInnerWidth is the textarea's text width: the window less the border and padding
// columns, floored at one so a very narrow window does not produce a zero-width box.
func (m *Model) inputInnerWidth() int {
	return max(1, m.width-borderFrame-inputPadding)
}

// inputRows is the textarea's height: the number of rows its current content wraps to,
// clamped to [minInputRows, maxInputRows]. The box grows as the human types a multi-line
// message and stops growing at the cap, where the textarea scrolls internally.
func (m *Model) inputRows() int {
	rows := inputContentRows(m.input.Value(), m.inputInnerWidth())
	return clampInt(rows, minInputRows, maxInputRows)
}

// refreshViewport re-renders the transcript into the viewport and, unless the human has
// scrolled, pins the last user prompt to the top of the visible area (sticky-to-top, as in
// apogee-code) so the prompt stays put while the reply streams beneath it. With no user
// prompt yet, it falls back to the bottom. A human scroll (userScrolled) suspends the pin so
// reading history is not yanked back; submit re-arms it.
func (m *Model) refreshViewport() {
	rendered := m.transcript.renderView(m.th, m.viewport.Width())
	m.lines = rendered.lines // stashed for the sticky-header overlay (View)
	m.userBlocks = rendered.userBlocks
	if m.userScrolled {
		m.viewport.SetContentLines(rendered.lines)
		return
	}
	if rendered.lastUserStart < 0 {
		m.viewport.SetContentLines(rendered.lines)
		m.viewport.GotoBottom()
		return
	}
	off := wrappedOffset(rendered.lines[:rendered.lastUserStart], m.viewport.Width())
	lines := rendered.lines
	// Pad with trailing blank rows so the viewport can scroll the prompt all the way to the top
	// even when the reply beneath it is shorter than a screen; otherwise SetYOffset is clamped
	// to maxYOffset (totalRows-height) and the prompt sits mid-screen.
	if need := off + m.viewport.Height(); len(lines) < need {
		lines = append(lines, make([]string, need-len(lines))...)
	}
	m.viewport.SetContentLines(lines)
	m.viewport.SetYOffset(off)
}

// ----------------------------------------------------------------------------
// View
// ----------------------------------------------------------------------------

// View stacks the transcript, a single blank line, the status line, the bordered input box,
// and the footer bar, filling the alternate screen (layout.md). Before the first
// WindowSizeMsg there is no geometry to lay out, so it shows a minimal placeholder. The
// approval prompt, when one is pending, sits between the transcript and the blank line; the
// viewport is shrunk on this local copy to make room (View has a value receiver, so the
// stored layout is untouched).
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
	// The autocomplete overlay and the attached-skill chips (idle only) sit just above the input
	// box. They, and the approval/ask prompt, each steal rows from the transcript viewport, so
	// shrink it by their combined height before rendering. The chips can co-occur with the
	// dropdown (attaching one skill while picking another); the prompt cannot (different states).
	dropdown := m.renderAutocomplete()
	chips := m.renderSkillChips()
	shrink := 0
	if prompt != "" {
		shrink += lipgloss.Height(prompt)
	}
	if dropdown != "" {
		shrink += lipgloss.Height(dropdown)
	}
	if chips != "" {
		shrink += lipgloss.Height(chips)
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
	body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.renderScrollbar(m.viewport.Height()))
	rows := []string{body}
	if prompt != "" {
		rows = append(rows, prompt)
	}
	// The single blank line between chat content and the bottom chrome (layout.md), then the
	// status line, the autocomplete overlay (when open), the input box, and the footer.
	rows = append(rows, "", m.statusLine())
	if dropdown != "" {
		rows = append(rows, dropdown)
	}
	if chips != "" {
		rows = append(rows, chips)
	}
	rows = append(rows, m.inputView(), m.footerView())

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, rows...))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion // enable wheel scrolling (Update routes MouseWheelMsg)
	return v
}

// applyStickyHeader overlays the user prompt that owns the content at the top of the viewport,
// frozen at row 0, so the prompt the on-screen replies belong to is always visible. As the next
// section's prompt scrolls up into the header zone it pushes the current header off the top — a
// position: sticky hand-off — until the next prompt is itself the natural top line. With the base
// pin in place (not scrolled) this redraws the latest prompt where it already is, a visual no-op.
func (m Model) applyStickyHeader(view string) string {
	if len(m.userBlocks) == 0 {
		return view
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
		return view // the top content sits above the first prompt — nothing to stick
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
		return view // this header is fully pushed out; the next one is already the natural top
	}
	header := m.lines[b.start+push : b.start+b.count] // the still-visible (bottom) header rows
	viewLines := strings.Split(view, "\n")
	for i, hl := range header {
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
// below it aligns.
func (m Model) inputView() string {
	return m.th.inputBorder.Width(m.width).Render(m.input.View())
}

// footerView renders the footer bar: a decorative top divider (the shared border with the
// input box above), the content line, and a decorative bottom rule. The content shows the
// host alias, model, and static context window on the left and the autonomy mode on the
// right — string(Mode), so a later rung (ModeAllowEdits, P3.4) appears for free. A window too
// narrow for a bordered bar renders nothing rather than overflowing.
func (m Model) footerView() string {
	w := m.width
	if w < 3 {
		return ""
	}
	rule := func(left, right string) string {
		return m.th.footerRule.Render(left + ruleMix(w-2) + right)
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
	host := m.opts.HostAlias
	if host == "" {
		host = m.opts.Endpoint
	}
	info := strings.Join(nonEmpty(host, displayModel(m.opts.Model), formatTokens(m.opts.ContextWindow)), " "+glyphAssistant+" ")
	mode := modeLabel(m.opts.Mode)
	bar := m.th.footerRule.Render("│")
	field := w - 2 // content columns between the two │ borders (footerView guards w >= 3)

	// One-column margins inside the borders; a black-bg gap justifies the mode marker right.
	gap := field - 2 - lipgloss.Width(info) - lipgloss.Width(mode)
	if gap < 1 {
		// Too narrow for both segments: keep the left info, truncate to the field, pad black.
		body := ansi.Truncate(" "+info, field, "…")
		body += strings.Repeat(" ", max(0, field-lipgloss.Width(body)))
		return bar + m.th.footerText.Render(body) + bar
	}
	left := m.th.footerText.Render(" " + info)
	fill := m.th.footerText.Render(strings.Repeat(" ", gap))
	// footerText keeps the black background; only the foreground swaps to the mode's colour.
	right := m.th.footerText.Foreground(modeColor(m.opts.Mode)).Render(mode) + m.th.footerText.Render(" ")
	return bar + left + fill + right + bar
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
func displayModel(s string) string {
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

// foldStats updates the live token stats from one engine Event (the eventMsg fold). Only the
// top-level agent's (Depth 0) accounting drives the status line: a sub-agent's usage nests in
// the stream, but the gauge tracks the conversation the human is steering. It marks when a
// Turn's content begins streaming (its first token) so a later UsageEvent can time the
// completion for a tokens/sec readout, resets that clock when the Turn re-streams, and on usage
// adopts the new context fill (the gauge's Used) and throughput. It mutates the local copy and
// returns it, like every Update fold.
func (m Model) foldStats(e domain.Event) Model {
	switch e := e.(type) {
	case domain.TokenEvent:
		if e.Depth == 0 && m.genStart.IsZero() {
			m.genStart = time.Now()
		}
	case domain.StreamResetEvent:
		if e.Depth == 0 {
			m.genStart = time.Time{} // the Turn re-streams (events.go) — time the fresh generation
		}
	case domain.UsageEvent:
		if e.Depth != 0 {
			break
		}
		// Prefer the server's total; fall back to prompt+completion when it omits the sum.
		total := e.TotalTokens
		if total == 0 {
			total = e.PromptTokens + e.CompletionTokens
		}
		if total > 0 {
			m.ctxUsed = total
		}
		if !m.genStart.IsZero() && e.CompletionTokens > 0 {
			if secs := time.Since(m.genStart).Seconds(); secs > 0 {
				m.tokPerSec = float64(e.CompletionTokens) / secs
			}
		}
		m.genStart = time.Time{}
	}
	return m
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

// statusLine renders the activity indicator and turn on the left and the live context gauge
// (or a key hint) on the right, justified across the window. It reads only display values off
// Options and the model's own state — never off the Engine mid-step.
func (m Model) statusLine() string {
	turn := m.th.statusBar.Render(fmt.Sprintf("turn %d", m.transcript.turn))
	left := turn
	switch m.state {
	case stateRunning:
		left = m.spinner.View() + m.th.statusBar.Render(" ") + turn + m.throughputSuffix()
	case stateAwaitingApproval:
		left = m.th.statusBar.Render("approval needed · ") + turn
	case stateAwaitingAsk:
		left = m.th.statusBar.Render("answer needed · ") + turn
	case stateErrored:
		left = m.th.statusError.Render("error") + m.th.statusBar.Render(" · ") + turn
	}
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

// statusRight is the status line's right slot: the live context gauge when token usage is
// known, else a state-appropriate key hint. The gauge is empty until Phase 4 routes usage, so
// for now the hint shows; once usage is wired, the gauge takes the slot with no rework.
func (m Model) statusRight() string {
	// A primed Ctrl+C takes the slot: tell the human a second press inside the window quits.
	if !m.lastCtrlC.IsZero() {
		return m.th.statusBar.Render("press ctrl+c again to quit")
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
func (c contextUsage) view(th theme) string {
	if c.Used <= 0 || c.Limit <= 0 {
		return ""
	}
	prefix := th.statusBar.Render(fmt.Sprintf("%s %d%% ", formatTokens(c.Used), c.Used*100/c.Limit))
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

// approvalStyle weights the approval prompt's lead line so it stands out from the transcript
// above it.
var approvalStyle = lipgloss.NewStyle().Bold(true)

// approvalPrompt renders the pending tool call the human must rule on: the RAW tool name (not
// the friendly transcript label — the approval flow is a security surface, so the human sees
// exactly the tool that will run) and its Reason on the lead line, the decision legend on the
// next, then the pretty-printed Arguments. Empty/null arguments add no body. Only the
// top-level (Depth == 0) prompt is rendered this phase.
func (m Model) approvalPrompt(req domain.ApprovalRequest) string {
	head := approvalStyle.Render("approve " + req.Tool + "?")
	if req.Reason != "" {
		head += "  " + m.th.statusFaint.Render("("+req.Reason+")")
	}
	body := head + "\n" + m.th.statusFaint.Render("a allow · d deny · s allow-session · esc cancel")
	if args := prettyJSON(req.Arguments); args != "" {
		body += "\n" + m.th.toolDetail.Render(args)
	}
	return body
}

// askPrompt renders the pending ask_user question above the input box: the question on a
// bold lead line and a one-line hint that the input below is the answer field (P3.11). The
// human types into the (borrowed) input box and presses enter to submit, or esc to cancel —
// the same chrome as a normal message, with the question framing it.
func (m Model) askPrompt(req domain.AskRequest) string {
	head := approvalStyle.Render("the assistant is asking:")
	body := head + "\n" + m.th.toolDetail.Render(req.Question)
	body += "\n" + m.th.statusFaint.Render("type your answer below · ⏎ send · ⇧⏎/⌥⏎ newline · esc cancel")
	return body
}
