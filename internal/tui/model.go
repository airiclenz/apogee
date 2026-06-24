package tui

import (
	"context"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

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

	// Content & layout.
	transcript    transcript
	th            theme // the palette and reusable styles, built once at construction
	width, height int
	ready         bool // a WindowSizeMsg has sized the layout at least once
	userScrolled  bool // the human scrolled the transcript; suspend sticky-to-top until submit
}

// newModel builds the initial idle Model. parent is the program context the worker derives
// its cancellable child from (so a program-wide shutdown also cancels an in-flight
// Exchange — C4). The input box is focused here, not in Init, because Init returns only a
// Cmd: the focus *state* must be set on the stored widget, while Init returns the cursor's
// blink Cmd.
func newModel(parent context.Context, eng Engine, opts Options) Model {
	th := newTheme()

	ta := textarea.New()
	ta.Placeholder = "Send a message…  ⏎ send · ⇧⏎ newline · esc quit"
	ta.Prompt = "" // the rounded border is the frame; no inline prompt gutter (layout.md)
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit; the model, not the widget, bounds a turn
	blackenInput(&ta)
	ta.Focus()

	vp := viewport.New()
	vp.SoftWrap = true // wrap long transcript lines to the viewport width

	sp := newBrailleSpinner()

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

	case eventMsg:
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

	default:
		// Other Bubble Tea Msgs (e.g. the cursor blink) belong to the widgets; route them
		// to the focused input so the cursor keeps blinking.
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

// handleKey routes a keypress against the current state. The stop keys (esc / ctrl+c)
// cancel an in-flight worker or quit at idle; enter submits at idle and is a no-op while a
// worker runs (the single-worker invariant the seam relies on). Other keys feed the input
// while idle, or scroll the transcript while busy.
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m.quit()
	case "esc":
		if m.busy() {
			m.stopWorker()
			return m, nil
		}
		return m.quit()
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
	}

	// A live approval prompt claims the decision keys. This branch must precede the scroll
	// fall-through below, which would otherwise swallow a/d/s as viewport scroll keys.
	if m.state == stateAwaitingApproval {
		return m.handleApprovalKey(msg)
	}

	// While awaiting an ask_user answer the input box is live so the human types the reply —
	// the same editing path as idle (enter, handled above, submits it).
	if m.state == stateIdle || m.state == stateAwaitingAsk {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
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

// submit launches a worker for the text in the input box. It records the user message,
// switches to running, stores the worker's CancelFunc (C4), and batches the worker Cmd
// with the spinner tick. A blank message is ignored. Only reachable from stateIdle, so the
// single-worker invariant holds.
func (m Model) submit() (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return m, nil
	}
	m.input.Reset()
	m.userScrolled = false // a fresh prompt re-arms sticky-to-top
	m.transcript.addUser(text)
	m.layout() // the emptied input box shrinks back; the new prompt pins to the top

	cmd, cancel := startExchange(m.parent, m.eng, domain.UserInput{Text: text})
	m.cancel = cancel
	m.state = stateRunning
	return m, tea.Batch(cmd, m.spinner.Tick)
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
	m.viewport.SetWidth(m.width)
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
	m.viewport.SetContentLines(rendered.lines)
	if m.userScrolled {
		return
	}
	if rendered.lastUserStart < 0 {
		m.viewport.GotoBottom()
		return
	}
	m.viewport.SetYOffset(wrappedOffset(rendered.lines[:rendered.lastUserStart], m.viewport.Width()))
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
	if prompt != "" {
		h := m.viewport.Height() - lipgloss.Height(prompt)
		if h < 1 {
			h = 1
		}
		m.viewport.SetHeight(h)
	}

	rows := []string{m.viewport.View()}
	if prompt != "" {
		rows = append(rows, prompt)
	}
	// The single blank line between chat content and the bottom chrome (layout.md).
	rows = append(rows, "", m.statusLine(), m.inputView(), m.footerView())

	v := tea.NewView(lipgloss.JoinVertical(lipgloss.Left, rows...))
	v.AltScreen = true
	return v
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

// footerContent composes the footer's content line: host ✦ model ✦ ctx on the left, mode on
// the right, between two dark-gray │ borders on a black field. The host falls back to the
// endpoint when no alias is configured, and the context window is omitted when unknown (0).
func (m Model) footerContent(w int) string {
	host := m.opts.HostAlias
	if host == "" {
		host = m.opts.Endpoint
	}
	left := strings.Join(nonEmpty(host, m.opts.Model, formatTokens(m.opts.ContextWindow)), " "+glyphAssistant+" ")
	body := fitLeftRight(left, string(m.opts.Mode), w-2)
	bar := m.th.footerRule.Render("│")
	return bar + m.th.footerText.Render(body) + bar
}

// statusLine renders the activity indicator and turn on the left and the live context gauge
// (or a key hint) on the right, justified across the window. It reads only display values off
// Options and the model's own state — never off the Engine mid-step.
func (m Model) statusLine() string {
	turn := m.th.statusFaint.Render(fmt.Sprintf("turn %d", m.transcript.turn))
	left := turn
	switch m.state {
	case stateRunning:
		left = m.spinner.View() + " " + turn
	case stateAwaitingApproval:
		left = m.th.statusFaint.Render("approval needed · ") + turn
	case stateAwaitingAsk:
		left = m.th.statusFaint.Render("answer needed · ") + turn
	case stateErrored:
		left = m.th.errorText.Render("error") + m.th.statusFaint.Render(" · ") + turn
	}
	return justify(left, m.th.statusFaint.Render(m.statusRight()), m.width)
}

// statusRight is the status line's right slot: the live context gauge when token usage is
// known, else a state-appropriate key hint. The gauge is empty until Phase 4 routes usage, so
// for now the hint shows; once usage is wired, the gauge takes the slot with no rework.
func (m Model) statusRight() string {
	if g := m.contextGauge(); g != "" {
		return g
	}
	switch m.state {
	case stateRunning:
		return "esc stop"
	case stateErrored:
		return "enter dismiss · esc quit"
	default:
		return ""
	}
}

// contextGauge renders the live token-usage gauge for the status line. Token counting is a
// Phase-4 deliverable (phase-3 detail plan §6), so Used is 0 today and the gauge renders
// nothing; the static window is shown in the footer instead. When Phase 4 routes usage, the
// gauge lights up here automatically — no UI rework.
func (m Model) contextGauge() string {
	return contextUsage{Used: 0, Limit: m.opts.ContextWindow}.view()
}

// contextUsage is the live context-window gauge's data: tokens Used out of the window Limit.
// It is self-hiding — view renders nothing until usage is known.
type contextUsage struct {
	Used  int
	Limit int
}

// view renders the gauge as "<used> <pct>% <bar>", or "" when usage is unknown.
func (c contextUsage) view() string {
	if c.Used <= 0 || c.Limit <= 0 {
		return ""
	}
	const barWidth = 6
	filled := clampInt(c.Used*barWidth/c.Limit, 0, barWidth)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return fmt.Sprintf("%s %d%% %s", formatTokens(c.Used), c.Used*100/c.Limit, bar)
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
	body += "\n" + m.th.statusFaint.Render("type your answer below · ⏎ send · esc cancel")
	return body
}
