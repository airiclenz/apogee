package tui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Model test harness (phase-2 detail plan §4 P2.2)
// ----------------------------------------------------------------------------

// The model is proven the way the plan asks: feed synthetic Msgs into Update and assert the
// state transitions and View substrings, with no terminal in the loop. The worker Cmd
// submit returns is never executed (these tests drive the state machine directly with the
// five seam Msgs), so the fakeEngine's drive methods are never called.

// testOpts are the display values the status line and footer render.
var testOpts = Options{
	Model:         "test-model",
	Endpoint:      "http://localhost:1234",
	Mode:          domain.ModeAskBefore,
	HostAlias:     "test-host",
	ContextWindow: 32768,
}

// newTestModel builds a ready, idle model sized to a standard window.
func newTestModel(t *testing.T) Model {
	t.Helper()
	m := newModel(context.Background(), &fakeEngine{}, testOpts)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// step runs one Update and returns the next model, discarding the Cmd.
func step(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, _ := m.Update(msg)
	return next.(Model)
}

// stepCmd runs one Update and returns the next model and its Cmd.
func stepCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// keys the model reads by their String() form.
func keyEnter() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeyEnter} }
func keyEsc() tea.KeyPressMsg   { return tea.KeyPressMsg{Code: tea.KeyEscape} }
func keyCtrlC() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl} }

// ansiPattern matches CSI escape sequences so assertions test rendered text, not styling.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;?]*[ -/]*[@-~]")

// plain returns the View's content with styling stripped.
func plain(v tea.View) string { return ansiPattern.ReplaceAllString(v.Content, "") }

// cmdMsg runs a (single, side-effect-free) Cmd and returns its Msg — used only for Cmds
// like tea.Quit whose closure is safe to invoke in a test.
func cmdMsg(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// ----------------------------------------------------------------------------
// The exchange lifecycle: submit → stream → message → done
// ----------------------------------------------------------------------------

func TestModelExchangeLifecycle(t *testing.T) {
	m := newTestModel(t)
	if m.state != stateIdle {
		t.Fatalf("fresh model state = %v, want idle", m.state)
	}

	// Submit launches the worker: state → running, a CancelFunc is stored, a Cmd is
	// returned, and the user message renders.
	m.input.SetValue("hello world")
	m, cmd := stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("after submit state = %v, want running", m.state)
	}
	if m.cancel == nil {
		t.Error("after submit cancel func is nil; the stop key would have nothing to cancel")
	}
	if cmd == nil {
		t.Error("after submit Cmd is nil; the worker was not launched")
	}
	if got := plain(m.View()); !strings.Contains(got, "hello world") {
		t.Errorf("view does not show the submitted message:\n%s", got)
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared after submit: %q", v)
	}

	// Tokens stream live into the in-progress assistant buffer.
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "hi "}})
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "there"}})
	if got := plain(m.View()); !strings.Contains(got, "hi there") {
		t.Errorf("streamed tokens not shown live:\n%s", got)
	}
	if m.state != stateRunning {
		t.Errorf("streaming changed state to %v, want still running", m.state)
	}

	// The MessageEvent finalises the buffer into a committed assistant entry.
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: "hi there"}})
	if m.transcript.streaming {
		t.Error("transcript still streaming after MessageEvent finalised the buffer")
	}
	if n := len(m.transcript.entries); n != 2 { // user + assistant
		t.Errorf("transcript has %d entries, want 2 (user + assistant)", n)
	}

	// The terminal Msg returns the model to idle and clears the CancelFunc.
	m = step(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	if m.state != stateIdle {
		t.Errorf("after exchangeDoneMsg state = %v, want idle", m.state)
	}
	if m.cancel != nil {
		t.Error("CancelFunc not cleared after the exchange completed")
	}
}

// ----------------------------------------------------------------------------
// Token reconciliation: the streamed tokens and the final MessageEvent agree
// ----------------------------------------------------------------------------

func TestModelMessageEventIsCanonical(t *testing.T) {
	m := newTestModel(t)
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "draft"}})
	// The MessageEvent text is canonical and supersedes the streamed preview.
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: "final answer"}})
	got := plain(m.View())
	if !strings.Contains(got, "final answer") {
		t.Errorf("canonical message text not shown:\n%s", got)
	}
	if strings.Contains(got, "draft") {
		t.Errorf("superseded streamed preview still shown:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// The four states are each reachable from their seam Msg
// ----------------------------------------------------------------------------

func TestModelSeamMessageTransitions(t *testing.T) {
	t.Run("approvalReqMsg → awaitingApproval", func(t *testing.T) {
		m := newTestModel(t)
		req := approvalReqMsg{
			Request: domain.ApprovalRequest{Tool: "write_file", Reason: "write"},
			Reply:   make(chan domain.ApprovalDecision, 1),
		}
		m = step(t, m, req)
		if m.state != stateAwaitingApproval {
			t.Fatalf("state = %v, want awaitingApproval", m.state)
		}
		if m.pending == nil {
			t.Fatal("pending approval not stored")
		}
		if got := plain(m.View()); !strings.Contains(got, "allow") || !strings.Contains(got, "deny") {
			t.Errorf("approval hint not shown:\n%s", got)
		}
	})

	t.Run("cancelledMsg → idle with a note", func(t *testing.T) {
		m := newTestModel(t)
		m.cancel = func() {} // stand in for a live worker
		m.state = stateRunning
		m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
		if m.state != stateIdle {
			t.Fatalf("state = %v, want idle", m.state)
		}
		if m.cancel != nil || m.pending != nil {
			t.Error("cancel/pending not cleared after cancellation")
		}
		if got := plain(m.View()); !strings.Contains(got, "cancelled") {
			t.Errorf("cancellation note not shown:\n%s", got)
		}
	})

	t.Run("errMsg → errored", func(t *testing.T) {
		m := newTestModel(t)
		m.state = stateRunning
		m = step(t, m, errMsg{Err: errors.New("upstream unreachable")})
		if m.state != stateErrored {
			t.Fatalf("state = %v, want errored", m.state)
		}
		if m.lastErr == nil {
			t.Error("lastErr not recorded")
		}
		if got := plain(m.View()); !strings.Contains(got, "error") {
			t.Errorf("error not surfaced in the view:\n%s", got)
		}
	})

	t.Run("errored → enter dismisses to idle", func(t *testing.T) {
		m := newTestModel(t)
		m.state = stateErrored
		m.lastErr = errors.New("boom")
		m = step(t, m, keyEnter())
		if m.state != stateIdle {
			t.Fatalf("state = %v, want idle after dismiss", m.state)
		}
		if m.lastErr != nil {
			t.Error("lastErr not cleared on dismiss")
		}
	})
}

// ----------------------------------------------------------------------------
// The single-worker invariant: submit while running is a no-op
// ----------------------------------------------------------------------------

func TestModelSubmitWhileRunningIsNoOp(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("first")
	m, _ = stepCmd(t, m, keyEnter())
	if m.state != stateRunning {
		t.Fatalf("state = %v, want running", m.state)
	}

	m.input.SetValue("second")
	next, cmd := stepCmd(t, m, keyEnter())
	if next.state != stateRunning {
		t.Errorf("state = %v, want still running (submit refused)", next.state)
	}
	if cmd != nil {
		t.Error("a second worker Cmd was launched while one was running")
	}
	if v := next.input.Value(); v != "second" {
		t.Errorf("input was consumed by a refused submit: %q", v)
	}
}

// blank submit is also refused (no worker, stays idle).
func TestModelBlankSubmitIsIgnored(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("   ")
	next, cmd := stepCmd(t, m, keyEnter())
	if next.state != stateIdle {
		t.Errorf("state = %v, want idle (blank submit ignored)", next.state)
	}
	if cmd != nil {
		t.Error("a worker was launched for a blank submit")
	}
}

// The newline keys insert a line break into the input instead of submitting: shift+enter on
// Kitty-capable terminals, alt+enter and ctrl+j everywhere. Plain enter still submits.
func TestModelNewlineKeysInsertLineBreak(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyPressMsg
	}{
		{"shift+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift}},
		{"alt+enter", tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt}},
		{"ctrl+j", tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModel(t)
			m.input.SetValue("line one")
			m.input.MoveToEnd()
			next := step(t, m, tc.key)
			// State stays idle and the input keeps growing: the key was a newline, not a submit
			// (a submit would switch to running and clear the input).
			if next.state != stateIdle {
				t.Errorf("state = %v, want idle (%s must not submit)", next.state, tc.name)
			}
			if got := next.input.Value(); !strings.Contains(got, "\n") {
				t.Errorf("%s did not insert a newline: input = %q", tc.name, got)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Cancellation and quit
// ----------------------------------------------------------------------------

func TestModelStopKeys(t *testing.T) {
	t.Run("esc while running cancels but does not quit", func(t *testing.T) {
		m := newTestModel(t)
		cancelled := false
		m.cancel = func() { cancelled = true }
		m.state = stateRunning
		next, cmd := stepCmd(t, m, keyEsc())
		if !cancelled {
			t.Error("esc did not cancel the in-flight worker")
		}
		if next.state != stateRunning {
			t.Errorf("state = %v, want still running until the worker reports back", next.state)
		}
		if msg := cmdMsg(cmd); msg != nil {
			if _, isQuit := msg.(tea.QuitMsg); isQuit {
				t.Error("esc quit the program instead of cancelling the worker")
			}
		}
	})

	t.Run("esc while idle quits", func(t *testing.T) {
		m := newTestModel(t)
		_, cmd := stepCmd(t, m, keyEsc())
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
			t.Error("esc at idle did not quit")
		}
	})

	t.Run("ctrl+c quits and cancels any worker", func(t *testing.T) {
		m := newTestModel(t)
		cancelled := false
		m.cancel = func() { cancelled = true }
		m.state = stateRunning
		_, cmd := stepCmd(t, m, keyCtrlC())
		if !cancelled {
			t.Error("ctrl+c did not cancel the in-flight worker before quitting")
		}
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
			t.Error("ctrl+c did not quit")
		}
	})
}

// ----------------------------------------------------------------------------
// The Approval UI (phase-2 detail plan §4 P2.4; ADR 0004 — the C3 face)
// ----------------------------------------------------------------------------

// newApprovalModel drives a fresh model to awaitingApproval with a buffered reply channel,
// returning both so a test can assert on the decision the keys send back.
func newApprovalModel(t *testing.T, req domain.ApprovalRequest) (Model, chan domain.ApprovalDecision) {
	t.Helper()
	reply := make(chan domain.ApprovalDecision, 1)
	m := step(t, newTestModel(t), approvalReqMsg{Request: req, Reply: reply})
	if m.state != stateAwaitingApproval {
		t.Fatalf("state = %v, want awaitingApproval", m.state)
	}
	return m, reply
}

// Each decision key yields the matching ApprovalDecision on the reply channel, clears the
// prompt, and returns to running so the worker's blocked Step resumes.
func TestModelApprovalDecisionKeys(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyPressMsg
		want domain.ApprovalDecision
	}{
		{"a → allow", tea.KeyPressMsg{Code: 'a'}, domain.ApprovalAllow},
		{"d → deny", tea.KeyPressMsg{Code: 'd'}, domain.ApprovalDeny},
		{"s → allow-for-session", tea.KeyPressMsg{Code: 's'}, domain.ApprovalAllowForSession},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, reply := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})

			m, cmd := stepCmd(t, m, tc.key)

			select {
			case got := <-reply:
				if got != tc.want {
					t.Errorf("decision = %q, want %q", got, tc.want)
				}
			default:
				t.Fatal("no decision sent on the reply channel")
			}
			if m.state != stateRunning {
				t.Errorf("state = %v, want running after the decision", m.state)
			}
			if m.pending != nil {
				t.Error("pending approval not cleared after the decision")
			}
			if cmd == nil {
				t.Error("spinner tick not re-armed on the return to running")
			}
		})
	}
}

// The pending request renders into the View: the tool, its Reason, and the arguments.
func TestModelApprovalPromptRender(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{
		Tool:      "write_file",
		Reason:    "write",
		Arguments: json.RawMessage(`{"path":"notes.txt"}`),
	})
	got := plain(m.View())
	for _, want := range []string{"write_file", "write", "notes.txt"} {
		if !strings.Contains(got, want) {
			t.Errorf("approval prompt missing %q:\n%s", want, got)
		}
	}
}

// A non-decision key while a prompt is up neither resolves the gate nor leaves the state.
func TestModelApprovalIgnoresOtherKeys(t *testing.T) {
	m, reply := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})

	m = step(t, m, tea.KeyPressMsg{Code: 'x'})

	select {
	case got := <-reply:
		t.Errorf("a non-decision key sent %q on the reply channel", got)
	default:
	}
	if m.state != stateAwaitingApproval {
		t.Errorf("state = %v, want still awaitingApproval", m.state)
	}
	if m.pending == nil {
		t.Error("pending approval cleared by a non-decision key")
	}
}

// A stop key while pending cancels the worker; the prompt clears when the worker reports back
// (the cancel path is structural — esc → stopWorker → cancelledMsg → finishWorker).
func TestModelApprovalCancelClearsPrompt(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})
	cancelled := false
	m.cancel = func() { cancelled = true }

	m = step(t, m, keyEsc())
	if !cancelled {
		t.Error("esc did not cancel the in-flight worker")
	}
	if m.state != stateAwaitingApproval {
		t.Errorf("state = %v, want still awaitingApproval until the worker reports back", m.state)
	}

	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
	if m.state != stateIdle {
		t.Fatalf("state = %v, want idle after cancellation", m.state)
	}
	if m.pending != nil {
		t.Error("pending prompt not cleared after cancellation")
	}
}

// ----------------------------------------------------------------------------
// The ask-user UI (P3.11 — the free-text C3-style face)
// ----------------------------------------------------------------------------

// newAskModel drives a fresh model to awaitingAsk with a buffered reply channel.
func newAskModel(t *testing.T, req domain.AskRequest) (Model, chan domain.AskAnswer) {
	t.Helper()
	reply := make(chan domain.AskAnswer, 1)
	m := step(t, newTestModel(t), askReqMsg{Request: req, Reply: reply})
	if m.state != stateAwaitingAsk {
		t.Fatalf("state = %v, want awaitingAsk", m.state)
	}
	return m, reply
}

// typeInput feeds each rune of s into the model as a keypress (the input box is live while
// awaitingAsk, so this is how the human types the answer).
func typeInput(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = step(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// An ask question switches to awaitingAsk; typing then enter sends the answer on the reply
// channel, clears the pending question, and returns to running so the worker resumes.
func TestModelAskRoundTrip(t *testing.T) {
	m, reply := newAskModel(t, domain.AskRequest{Question: "what colour?"})

	m = typeInput(t, m, "teal")
	m, cmd := stepCmd(t, m, keyEnter())

	select {
	case got := <-reply:
		if got.Text != "teal" {
			t.Errorf("answer = %q, want %q", got.Text, "teal")
		}
	default:
		t.Fatal("no answer sent on the reply channel")
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want running after the answer", m.state)
	}
	if m.pendingAsk != nil {
		t.Error("pending question not cleared after the answer")
	}
	if cmd == nil {
		t.Error("spinner tick not re-armed on the return to running")
	}
}

// The pending question renders into the View.
func TestModelAskPromptRender(t *testing.T) {
	m, _ := newAskModel(t, domain.AskRequest{Question: "pick a port number"})
	got := plain(m.View())
	if !strings.Contains(got, "pick a port number") {
		t.Errorf("ask prompt missing the question:\n%s", got)
	}
}

// A stop key while a question is pending cancels the worker; the question clears when the
// worker reports back (the same structural cancel path as the Approval gate).
func TestModelAskCancelClearsPrompt(t *testing.T) {
	m, _ := newAskModel(t, domain.AskRequest{Question: "q?"})
	cancelled := false
	m.cancel = func() { cancelled = true }

	m = step(t, m, keyEsc())
	if !cancelled {
		t.Error("esc did not cancel the in-flight worker")
	}

	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
	if m.state != stateIdle {
		t.Fatalf("state = %v, want idle after cancellation", m.state)
	}
	if m.pendingAsk != nil {
		t.Error("pending question not cleared after cancellation")
	}
}

// ----------------------------------------------------------------------------
// Snapshot-on-quit (phase-2 detail plan §4 P2.5; §6.1)
// ----------------------------------------------------------------------------

// recordingSaver captures the snapshot the model hands the saver seam.
type recordingSaver struct {
	called bool
	sess   domain.Session
	err    error // injected to simulate a save failure
}

func (r *recordingSaver) save(s domain.Session) error {
	r.called = true
	r.sess = s
	return r.err
}

// newSavingModel builds a ready, idle model wired to the given saver.
func newSavingModel(t *testing.T, eng Engine, save func(domain.Session) error) Model {
	t.Helper()
	m := newModel(context.Background(), eng, Options{Save: save})
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// A clean quit (idle, with a non-empty conversation) snapshots the Engine and hands the
// result to the saver, then quits.
func TestModelSavesOnCleanQuit(t *testing.T) {
	marker := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"saved":true}`)}
	eng := &fakeEngine{snapshotFn: func() (domain.Session, error) { return marker, nil }}
	rec := &recordingSaver{}
	m := newSavingModel(t, eng, rec.save)
	m.transcript.addUser("hello") // give it content worth saving

	_, cmd := stepCmd(t, m, keyEsc())
	if !rec.called {
		t.Fatal("a clean quit did not save the session")
	}
	if string(rec.sess.State) != string(marker.State) {
		t.Errorf("saved snapshot = %q; want the Engine's snapshot %q", rec.sess.State, marker.State)
	}
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("a clean quit did not quit the program")
	}
}

// An empty conversation is not worth a snapshot file — quit without saving.
func TestModelDoesNotSaveEmptyConversation(t *testing.T) {
	rec := &recordingSaver{}
	m := newSavingModel(t, &fakeEngine{}, rec.save)

	_, cmd := stepCmd(t, m, keyEsc())
	if rec.called {
		t.Error("an empty conversation was saved on quit")
	}
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("quit did not exit")
	}
}

// Quitting while a worker is in flight must NOT snapshot — the worker owns the Agent, and
// the Agent is single-goroutine, so a snapshot here would race its Step. ctrl+c cancels
// and quits instead (the last boundary is unsaved this phase).
func TestModelDoesNotSaveWhileBusy(t *testing.T) {
	snapshotted := false
	eng := &fakeEngine{snapshotFn: func() (domain.Session, error) {
		snapshotted = true
		return domain.Session{}, nil
	}}
	rec := &recordingSaver{}
	m := newSavingModel(t, eng, rec.save)
	m.transcript.addUser("hi")
	m.state = stateRunning
	m.cancel = func() {}

	_, cmd := stepCmd(t, m, keyCtrlC())
	if snapshotted || rec.called {
		t.Error("snapshotted while a worker was running (would race the single-goroutine Agent)")
	}
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("ctrl+c while busy did not quit")
	}
}

// A nil saver (session saving disabled) must not break the quit path.
func TestModelQuitWithoutSaver(t *testing.T) {
	m := newTestModel(t) // testOpts carries no Save
	m.transcript.addUser("hi")
	_, cmd := stepCmd(t, m, keyEsc())
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("quit with no saver did not exit")
	}
}

// ----------------------------------------------------------------------------
// Layout: the status line and resizing
// ----------------------------------------------------------------------------

// The status line carries the turn; the footer carries the host alias, model, static context
// window, and mode. The full endpoint URL is no longer shown — the footer uses the host alias.
func TestModelStatusLine(t *testing.T) {
	m := newTestModel(t)
	got := plain(m.View())
	for _, want := range []string{"test-host", "test-model", "32k", "ask-before", "turn"} {
		if !strings.Contains(got, want) {
			t.Errorf("status/footer missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "http://localhost:1234") {
		t.Errorf("footer shows the full endpoint URL; want the host alias instead:\n%s", got)
	}
}

func TestModelResizeDoesNotPanic(t *testing.T) {
	m := newTestModel(t)
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {200, 60}, {20, 6}, {5, 2}, {1, 1}} {
		m = step(t, m, tea.WindowSizeMsg{Width: size.w, Height: size.h})
		if got := m.View().Content; got == "" {
			t.Errorf("empty view at %dx%d", size.w, size.h)
		}
		if w := m.viewport.Width(); w != size.w {
			t.Errorf("viewport width = %d at window width %d", w, size.w)
		}
	}
}

// Before the first WindowSizeMsg the view is a placeholder, not a panic.
func TestModelViewBeforeReady(t *testing.T) {
	m := newModel(context.Background(), &fakeEngine{}, testOpts)
	if m.ready {
		t.Fatal("model ready before any WindowSizeMsg")
	}
	if got := plain(m.View()); !strings.Contains(got, "apogee") {
		t.Errorf("pre-ready placeholder unexpected:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// Depth > 0 rendering (Phase 3 sub-agents render as a framed block, no crash) — P3.14
// ----------------------------------------------------------------------------

func TestModelRendersNestedDepth(t *testing.T) {
	m := newTestModel(t)
	m = step(t, m, eventMsg{Event: domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "nested"}})
	got := plain(m.View())
	if !strings.Contains(got, "nested") {
		t.Errorf("nested-depth event not rendered:\n%s", got)
	}
	if !strings.Contains(got, "⤷ sub-agent") {
		t.Errorf("nested-depth block not opened by a sub-agent label:\n%s", got)
	}
	if !strings.Contains(got, "│ │ ✦ nested") {
		t.Errorf("depth-2 block not framed by two rail gutters:\n%s", got)
	}
}

// ----------------------------------------------------------------------------
// Structural guard: the value-copied Model must hold no strings.Builder by value
// ----------------------------------------------------------------------------

// TestModelNoBuilderByValue asserts that nothing the Model copies on every Update is a
// strings.Builder held by value. A Builder records a pointer to itself on its first write
// and panics ("illegal use of non-zero Builder copied by value") when a later write finds
// it at a different address — which is exactly what happens to a value field of a model
// Bubble Tea copies on each Update. A behavioural two-token test cannot reliably catch this:
// the panic is address-dependent, and a tight test loop reuses the same stack slot for the
// Update receiver, hiding it. This walks the Model's value-reachable type graph instead, so
// the invariant holds no matter how the renderer is rewired. A Builder behind a pointer,
// slice, or map is fine — only the header is copied — so the walk descends through value
// composites (structs, arrays) only.
func TestModelNoBuilderByValue(t *testing.T) {
	builderType := reflect.TypeOf(strings.Builder{})
	seen := map[reflect.Type]bool{}

	var walk func(typ reflect.Type, path string)
	walk = func(typ reflect.Type, path string) {
		if seen[typ] {
			return
		}
		seen[typ] = true

		if typ == builderType {
			t.Errorf("strings.Builder held by value at %s — the Model is copied on every "+
				"Update and a value Builder panics copyCheck; hold it by pointer or use a string", path)
			return
		}
		switch typ.Kind() {
		case reflect.Struct:
			for i := 0; i < typ.NumField(); i++ {
				f := typ.Field(i)
				walk(f.Type, path+"."+f.Name)
			}
		case reflect.Array:
			walk(typ.Elem(), path+"[]")
		}
		// Pointer/Slice/Map/Chan/Interface/Func are reference headers: copying the Model
		// copies the reference, not the pointee, so a Builder behind one is never copied.
	}

	walk(reflect.TypeOf(Model{}), "Model")
}

// ----------------------------------------------------------------------------
// Sticky-to-top and auto-grow input (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------

// firstVisibleLine returns the viewport's top visible line, styling stripped.
func firstVisibleLine(vp viewport.Model) string {
	return strings.SplitN(ansiPattern.ReplaceAllString(vp.View(), ""), "\n", 2)[0]
}

// The last user prompt is pinned to the top of the viewport while the reply streams beneath
// it, and a human scroll suspends the pin so reading history is not yanked back.
func TestStickyPinsLastUserPrompt(t *testing.T) {
	m := newTestModel(t) // 80x24

	// A transcript taller than the viewport, with a clear last prompt followed by enough
	// content below it to fill the screen (so the pin is not clamped to the bottom).
	m.transcript.addUser("first question")
	m.transcript.commitAssistant(strings.Repeat("filler above. ", 80), 0)
	m.transcript.addUser("STICKY-PROMPT")
	for i := 0; i < 30; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()

	if m.viewport.YOffset() == 0 {
		t.Fatal("viewport did not scroll; the sticky prompt cannot be pinned to the top")
	}
	if top := firstVisibleLine(m.viewport); !strings.Contains(top, "STICKY-PROMPT") {
		t.Errorf("top visible line = %q; want the last user prompt pinned to the top", top)
	}

	// A human scroll suspends the pin: a later refresh must not re-pin and move the offset.
	m.userScrolled = true
	off := m.viewport.YOffset()
	m.transcript.commitAssistant("more streamed content", 0)
	m.refreshViewport()
	if m.viewport.YOffset() != off {
		t.Errorf("a scrolled viewport was re-pinned: offset %d → %d", off, m.viewport.YOffset())
	}
}

// The input box grows with its content and the viewport shrinks by the same number of rows,
// keeping the layout balanced as a multi-line message is typed.
func TestInputAutoGrowReflowsViewport(t *testing.T) {
	m := newTestModel(t)
	if r := m.input.Height(); r != 1 {
		t.Fatalf("empty input height = %d, want 1 row", r)
	}
	vpBefore := m.viewport.Height()

	m.input.SetValue("line1\nline2\nline3\nline4")
	m.layout()

	if r := m.input.Height(); r != 4 {
		t.Errorf("input height after a four-line message = %d, want 4", r)
	}
	if got, want := m.viewport.Height(), vpBefore-3; got != want {
		t.Errorf("viewport height = %d; want %d (shrunk by the three rows the input grew)", got, want)
	}
}
