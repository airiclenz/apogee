package tui

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

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

// testOpts are the display values the status line renders.
var testOpts = Options{
	Model:    "test-model",
	Endpoint: "http://localhost:1234",
	Mode:     domain.ModeAskBefore,
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
// Layout: the status line and resizing
// ----------------------------------------------------------------------------

func TestModelStatusLine(t *testing.T) {
	m := newTestModel(t)
	got := plain(m.View())
	for _, want := range []string{"test-model", "http://localhost:1234", "ask-before", "turn"} {
		if !strings.Contains(got, want) {
			t.Errorf("status line missing %q:\n%s", want, got)
		}
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
// Depth > 0 tolerance (Phase 3 sub-agents must not crash the Phase-2 renderer)
// ----------------------------------------------------------------------------

func TestModelToleratesNestedDepth(t *testing.T) {
	m := newTestModel(t)
	m = step(t, m, eventMsg{Event: domain.MessageEvent{EventBase: domain.EventBase{Depth: 2}, Text: "nested"}})
	if got := plain(m.View()); !strings.Contains(got, "nested") {
		t.Errorf("nested-depth event not rendered:\n%s", got)
	}
}
