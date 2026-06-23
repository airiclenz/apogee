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

	// Sub-models (Bubbles widgets).
	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	// Lifecycle.
	state   uiState
	cancel  context.CancelFunc // non-nil while a worker runs; the stop key calls it (C4)
	pending *approvalReqMsg    // the in-flight Approval while awaitingApproval (P2.4 acts on it)
	lastErr error              // the error behind stateErrored, shown in the status line

	// Content & layout.
	transcript    transcript
	width, height int
	ready         bool // a WindowSizeMsg has sized the layout at least once
}

// newModel builds the initial idle Model. parent is the program context the worker derives
// its cancellable child from (so a program-wide shutdown also cancels an in-flight
// Exchange — C4). The input box is focused here, not in Init, because Init returns only a
// Cmd: the focus *state* must be set on the stored widget, while Init returns the cursor's
// blink Cmd.
func newModel(parent context.Context, eng Engine, opts Options) Model {
	ta := textarea.New()
	ta.Placeholder = "Send a message…"
	ta.Prompt = "› "
	ta.ShowLineNumbers = false
	ta.CharLimit = 0 // no limit; the model, not the widget, bounds a turn
	ta.Focus()

	vp := viewport.New()
	vp.SoftWrap = true // wrap long transcript lines to the viewport width

	sp := spinner.New(spinner.WithSpinner(spinner.Dot))

	return Model{
		parent:   parent,
		eng:      eng,
		opts:     opts,
		input:    ta,
		viewport: vp,
		spinner:  sp,
		state:    stateIdle,
	}
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
		// The worker's Approver hands the gate to the Update loop. P2.2 records it and
		// switches state; P2.4 renders the prompt and replies on msg.Reply.
		m.state = stateAwaitingApproval
		m.pending = &msg
		return m, nil

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
		case stateErrored:
			// Dismiss the error and return to idle so the next message can be sent.
			m.lastErr = nil
			m.state = stateIdle
			return m, nil
		default:
			return m, nil // no-op while running / awaiting approval
		}
	}

	if m.state == stateIdle {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	// While busy, let the keys scroll the transcript rather than edit the (refused) input.
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
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
	m.transcript.addUser(text)
	m.refreshViewport()

	cmd, cancel := startExchange(m.parent, m.eng, domain.UserInput{Text: text})
	m.cancel = cancel
	m.state = stateRunning
	return m, tea.Batch(cmd, m.spinner.Tick)
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
// arrives: it clears the CancelFunc and any pending Approval. The new state is idle for a
// completed or cancelled Exchange, errored for a loop fault.
func (m *Model) finishWorker(next uiState) {
	m.cancel = nil
	m.pending = nil
	m.state = next
}

// quit cancels any in-flight worker (so its goroutine unwinds rather than outliving the
// program) and tells Bubble Tea to exit.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.stopWorker()
	return m, tea.Quit
}

// busy reports whether a worker is in flight (running or blocked on an Approval) — the
// states in which input is refused and the stop key cancels instead of quitting.
func (m Model) busy() bool {
	return m.state == stateRunning || m.state == stateAwaitingApproval
}

// ----------------------------------------------------------------------------
// Layout
// ----------------------------------------------------------------------------

// inputHeight is the textarea's fixed height in rows; statusHeight and hintHeight are one
// row each. The viewport takes whatever remains.
const (
	inputHeight  = 3
	statusHeight = 1
	hintHeight   = 1
)

// layout sizes the viewport and input to the current window. The viewport gets the height
// left after the status, input, and hint rows; a floor of one row keeps it valid on a tiny
// window. It refreshes the viewport content so a resize reflows without losing the bottom.
func (m *Model) layout() {
	vpHeight := m.height - inputHeight - statusHeight - hintHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.SetWidth(m.width)
	m.viewport.SetHeight(vpHeight)
	m.input.SetWidth(m.width)
	m.input.SetHeight(inputHeight)
	m.refreshViewport()
}

// refreshViewport re-renders the transcript into the viewport and pins it to the bottom so
// the newest content stays visible as it streams in.
func (m *Model) refreshViewport() {
	m.viewport.SetContent(m.transcript.render())
	m.viewport.GotoBottom()
}

// ----------------------------------------------------------------------------
// View
// ----------------------------------------------------------------------------

// View renders the transcript, the status line, the input box, and a key hint, stacked top
// to bottom and filling the alternate screen. Before the first WindowSizeMsg there is no
// geometry to lay out, so it shows a minimal placeholder.
func (m Model) View() tea.View {
	if !m.ready {
		return tea.NewView("apogee — starting…")
	}
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		m.viewport.View(),
		m.statusLine(),
		m.input.View(),
		m.hintLine(),
	)
	v := tea.NewView(body)
	v.AltScreen = true
	return v
}

// statusStyle and hintStyle dim the two chrome rows so the transcript reads as the
// foreground. Colour theming is a later polish (plan §6).
var (
	statusStyle = lipgloss.NewStyle().Faint(true)
	hintStyle   = lipgloss.NewStyle().Faint(true)
)

// statusLine renders the model · endpoint · mode [· bypass] · turn, prefixed by a
// state indicator (a spinner while running, a word otherwise). It reads only display
// values off Options and the model's own state — never off the Engine mid-step.
func (m Model) statusLine() string {
	parts := []string{m.opts.Model, m.opts.Endpoint, string(m.opts.Mode)}
	if m.opts.Bypass {
		parts = append(parts, "bypass")
	}
	parts = append(parts, fmt.Sprintf("turn %d", m.transcript.turn))
	line := strings.Join(parts, " · ")

	switch m.state {
	case stateRunning:
		line = m.spinner.View() + " " + line
	case stateAwaitingApproval:
		line = "approval needed · " + line
	case stateErrored:
		line = "error · " + line
	default:
		line = "ready · " + line
	}
	return statusStyle.Width(m.width).Render(line)
}

// hintLine renders the live key legend for the current state.
func (m Model) hintLine() string {
	var hint string
	switch m.state {
	case stateRunning:
		hint = "esc stop"
	case stateAwaitingApproval:
		hint = "a allow · d deny · s allow-session · esc cancel"
	case stateErrored:
		hint = "enter dismiss · esc quit"
	default:
		hint = "enter send · esc quit"
	}
	return hintStyle.Width(m.width).Render(hint)
}
