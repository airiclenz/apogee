package tui

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

// ctrlCQuit drives the two-press Ctrl+C quit gesture: the first press arms it (its disarm
// tick is discarded by step), the second — landing microseconds later, well inside
// ctrlCQuitWindow — confirms the quit. Returns the model and the confirming press's Cmd.
func ctrlCQuit(t *testing.T, m Model) (Model, tea.Cmd) {
	t.Helper()
	m = step(t, m, keyCtrlC())
	return stepCmd(t, m, keyCtrlC())
}

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
// Live token stats: the UsageEvent lights the context gauge and times throughput
// ----------------------------------------------------------------------------

func TestUsageEventDrivesGaugeAndThroughput(t *testing.T) {
	m := newTestModel(t) // ContextWindow 32768

	// The gauge is dark until the first turn reports usage.
	if g := m.contextGauge(); g != "" {
		t.Fatalf("context gauge lit before any usage: %q", g)
	}

	// A token starts the throughput clock; a short gap guarantees a non-zero elapsed before the
	// terminal usage lands.
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "hi"}})
	time.Sleep(2 * time.Millisecond)
	m = step(t, m, eventMsg{Event: domain.UsageEvent{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200}})

	if m.ctxUsed != 1200 {
		t.Errorf("ctxUsed = %d, want 1200 (the reported total)", m.ctxUsed)
	}
	if g := m.contextGauge(); !strings.Contains(g, "1k") {
		t.Errorf("context gauge not lit by usage: %q", g)
	}
	if m.tokPerSec <= 0 {
		t.Errorf("tokPerSec = %v, want > 0 (completion timed against the token clock)", m.tokPerSec)
	}
	if s := m.throughputSuffix(); !strings.Contains(s, "tok/s") {
		t.Errorf("throughput readout empty after usage: %q", s)
	}

	// A sub-agent's usage (Depth > 0) nests in the stream but must not move the top-level gauge.
	prev := m.ctxUsed
	m = step(t, m, eventMsg{Event: domain.UsageEvent{
		EventBase:    domain.EventBase{Depth: 1},
		PromptTokens: 9, CompletionTokens: 9, TotalTokens: 9,
	}})
	if m.ctxUsed != prev {
		t.Errorf("a Depth>0 UsageEvent changed the top-level gauge: %d -> %d", prev, m.ctxUsed)
	}
}

// A re-streamed Turn (StreamResetEvent) restarts the throughput clock, so the next usage times
// only the accepted generation.
func TestUsageThroughputClockResetsOnReStream(t *testing.T) {
	m := newTestModel(t)
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "draft"}})
	m = step(t, m, eventMsg{Event: domain.StreamResetEvent{}})
	if !m.genStart.IsZero() {
		t.Errorf("throughput clock not reset by StreamReset: %v", m.genStart)
	}
}

// The gauge bar is a solid two-tone strip (llama-launcher look): full blocks for the filled
// cells, an eighth-block partial cell for sub-cell granularity, and a solid track for the
// rest — with a min-sliver floor and a clamp at the window limit.
func TestContextGaugeBarRendering(t *testing.T) {
	th := newTheme()

	// 50% of a 10-cell bar lands on a whole-cell boundary: 5 full blocks, no partial.
	half := contextUsage{Used: 16384, Limit: 32768}.view(th)
	if !strings.Contains(half, "50%") {
		t.Errorf("gauge missing percentage: %q", ansi.Strip(half))
	}
	if got := strings.Count(ansi.Strip(half), "█"); got != 5 {
		t.Errorf("full blocks = %d, want 5 for 50%% of a %d-cell bar: %q", got, gaugeWidth, ansi.Strip(half))
	}

	// Zero usage hides the gauge entirely (the static window shows in the footer instead).
	if g := (contextUsage{Used: 0, Limit: 32768}).view(th); g != "" {
		t.Errorf("gauge lit at zero usage: %q", g)
	}

	// A tiny nonzero fraction still shows the smallest eighth sliver — never a blank bar.
	sliver := ansi.Strip(contextUsage{Used: 1, Limit: 32768}.view(th))
	if !strings.ContainsRune(sliver, gaugeEighths[0]) {
		t.Errorf("min-sliver not shown for tiny usage: %q", sliver)
	}

	// An over-limit Used clamps to a full bar — gaugeWidth full blocks, no overflow.
	full := ansi.Strip(contextUsage{Used: 40000, Limit: 32768}.view(th))
	if got := strings.Count(full, "█"); got != gaugeWidth {
		t.Errorf("full blocks = %d, want %d at/over the limit: %q", got, gaugeWidth, full)
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

	t.Run("cancelledMsg discards the Exchange so the next input is accepted", func(t *testing.T) {
		// The post-Esc wedge regression: a cancel must tell the engine to abort the open
		// Exchange, otherwise the engine stays inExchange and the next /clear or message is
		// rejected with ErrInputPending.
		eng := &fakeEngine{}
		m := newModel(context.Background(), eng, testOpts)
		m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		m.cancel = func() {} // stand in for a live worker
		m.state = stateRunning
		m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
		if m.state != stateIdle {
			t.Fatalf("state = %v, want idle", m.state)
		}
		if got := eng.aborts(); got != 1 {
			t.Fatalf("AbortExchange called %d times, want 1 (the cancel must discard the open Exchange)", got)
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

	t.Run("errMsg discards the open Exchange so the next input is accepted", func(t *testing.T) {
		// The error flavour of the post-Esc wedge: a loop fault must abort the open Exchange the
		// same way a cancel does, otherwise a mid-Exchange Step error would leave the engine
		// inExchange and the next /clear or message would be rejected with ErrInputPending. Latent
		// today (Step surfaces faults as an ErrorEvent at a boundary), so this pins the guard.
		eng := &fakeEngine{}
		m := newModel(context.Background(), eng, testOpts)
		m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		m.cancel = func() {} // stand in for a live worker
		m.state = stateRunning
		m = step(t, m, errMsg{Err: errors.New("loop fault mid-exchange")})
		if m.state != stateErrored {
			t.Fatalf("state = %v, want errored", m.state)
		}
		if got := eng.aborts(); got != 1 {
			t.Fatalf("AbortExchange called %d times, want 1 (a loop fault must discard the open Exchange)", got)
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

	t.Run("esc while idle does not quit", func(t *testing.T) {
		m := newTestModel(t)
		_, cmd := stepCmd(t, m, keyEsc())
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
			t.Error("esc at idle quit the program; it must never end the app")
		}
	})

	t.Run("esc while errored does not quit", func(t *testing.T) {
		m := newTestModel(t)
		m.state = stateErrored
		_, cmd := stepCmd(t, m, keyEsc())
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
			t.Error("esc while errored quit the program; it must never end the app")
		}
	})

	t.Run("a single ctrl+c arms the gesture but does not quit", func(t *testing.T) {
		m := newTestModel(t)
		next, _ := stepCmd(t, m, keyCtrlC())
		if next.lastCtrlC.IsZero() {
			t.Error("a single ctrl+c did not arm the quit gesture")
		}
		if got := plain(next.View()); !strings.Contains(got, "press ctrl+c again to quit") {
			t.Errorf("the arm hint is not shown after one ctrl+c:\n%s", got)
		}
	})

	t.Run("ctrl+c twice at idle quits immediately", func(t *testing.T) {
		m := newTestModel(t)
		_, cmd := ctrlCQuit(t, m)
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
			t.Error("ctrl+c×2 at idle did not quit")
		}
	})

	t.Run("ctrl+c twice while busy defers the quit until the worker returns", func(t *testing.T) {
		m := newTestModel(t)
		cancelled := false
		m.cancel = func() { cancelled = true }
		m.state = stateRunning
		next, cmd := ctrlCQuit(t, m)
		if !cancelled {
			t.Error("ctrl+c×2 did not cancel the in-flight worker")
		}
		// The exit is DEFERRED: returning tea.Quit here would race runRoot's Close() teardown
		// against a worker still inside Step. The quit is armed instead.
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
			t.Error("ctrl+c×2 while busy quit immediately instead of deferring to the worker")
		}
		if !next.quitting {
			t.Error("ctrl+c×2 while busy did not arm the deferred quit")
		}
		// The worker's single terminal Msg fires the real quit once its goroutine has unwound.
		_, doneCmd := stepCmd(t, next, cancelledMsg{})
		if _, isQuit := cmdMsg(doneCmd).(tea.QuitMsg); !isQuit {
			t.Error("the worker's terminal Msg did not fire the deferred quit")
		}
	})

	t.Run("a second ctrl+c after the window only re-arms", func(t *testing.T) {
		m := newTestModel(t)
		m = step(t, m, keyCtrlC())
		m.lastCtrlC = m.lastCtrlC.Add(-2 * ctrlCQuitWindow) // pretend the window lapsed
		// Re-arming refreshes lastCtrlC to ~now; the quit path leaves it untouched. A refreshed
		// stamp therefore proves the press took the arm branch, not the quit branch — asserted
		// on state so the disarm-tick Cmd need not run (it would block for the whole window).
		next, _ := stepCmd(t, m, keyCtrlC())
		if !next.lastCtrlC.After(m.lastCtrlC) {
			t.Error("ctrl+c after the window quit instead of only re-arming")
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
	m.transcript.addUser("hello", nil) // give it content worth saving

	_, cmd := ctrlCQuit(t, m)
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

	_, cmd := ctrlCQuit(t, m)
	if rec.called {
		t.Error("an empty conversation was saved on quit")
	}
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("quit did not exit")
	}
}

// Quitting while a worker is in flight must NOT snapshot — the worker owns the Agent, and
// the Agent is single-goroutine, so a snapshot here would race its Step. ctrl+c cancels and
// DEFERS the exit until the worker returns (item 8), and the last boundary stays unsaved.
func TestModelDoesNotSaveWhileBusy(t *testing.T) {
	snapshotted := false
	eng := &fakeEngine{snapshotFn: func() (domain.Session, error) {
		snapshotted = true
		return domain.Session{}, nil
	}}
	rec := &recordingSaver{}
	m := newSavingModel(t, eng, rec.save)
	m.transcript.addUser("hi", nil)
	m.state = stateRunning
	m.cancel = func() {}

	next, cmd := ctrlCQuit(t, m)
	if snapshotted || rec.called {
		t.Error("snapshotted while a worker was running (would race the single-goroutine Agent)")
	}
	// The exit is DEFERRED while busy: an immediate tea.Quit would race runRoot's Close()
	// teardown against the still-running worker.
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
		t.Error("ctrl+c×2 while busy quit immediately instead of waiting for the worker")
	}
	// The worker's terminal Msg fires the deferred quit — and still saves nothing (the busy
	// path never armed a save; the last boundary stays unsaved this phase).
	_, doneCmd := stepCmd(t, next, cancelledMsg{})
	if snapshotted || rec.called {
		t.Error("the deferred quit snapshotted the cancelled boundary; the busy path must not save")
	}
	if _, isQuit := cmdMsg(doneCmd).(tea.QuitMsg); !isQuit {
		t.Error("the worker's terminal Msg did not fire the deferred quit")
	}
}

// A nil saver (session saving disabled) must not break the quit path.
func TestModelQuitWithoutSaver(t *testing.T) {
	m := newTestModel(t) // testOpts carries no Save
	m.transcript.addUser("hi", nil)
	_, cmd := ctrlCQuit(t, m)
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("quit with no saver did not exit")
	}
}

// ----------------------------------------------------------------------------
// Layout: the status line and resizing
// ----------------------------------------------------------------------------

// The footer carries the host alias, model, static context window, and mode. The full endpoint
// URL is no longer shown — the footer uses the host alias. (The status line's own left slot is
// the live activity, covered by TestModelStatusLineActivity; at idle it is empty.)
func TestModelStatusLine(t *testing.T) {
	m := newTestModel(t)
	got := plain(m.View())
	// The footer renders the mode as a friendly, spaced label (ask-before → "ask before").
	for _, want := range []string{"test-host", "test-model", "32k", "ask before"} {
		if !strings.Contains(got, want) {
			t.Errorf("status/footer missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "http://localhost:1234") {
		t.Errorf("footer shows the full endpoint URL; want the host alias instead:\n%s", got)
	}
}

// elapsedPattern matches the clock the status line hangs off the activity phrase
// ("· 0s", "· 1m 04s").
var elapsedPattern = regexp.MustCompile(`· (\d+m )?\d+s`)

// statusText renders the model's status line with its styling stripped, so a test asserts on
// the words rather than on the black-field escapes.
func statusText(t *testing.T, m Model) string {
	t.Helper()
	return strings.TrimSpace(ansiPattern.ReplaceAllString(m.statusLine(), ""))
}

// TestModelStatusLineActivity proves the status line answers "what is it doing?" at the state
// level: idle leaves the left slot empty (the input box below already invites a message), and a
// running worker shows the live phrase with an elapsed clock, re-derived from each Event.
func TestModelStatusLineActivity(t *testing.T) {
	m := newTestModel(t)
	if got := statusText(t, m); got != "" {
		t.Errorf("idle status line is not empty: %q", got)
	}

	// Submit: the request is away, nothing has come back — "thinking · 0s".
	m.input.SetValue("hello")
	m = step(t, m, keyEnter())
	got := statusText(t, m)
	if !strings.Contains(got, "thinking") {
		t.Errorf("running status line = %q, want it to contain %q", got, "thinking")
	}
	if !elapsedPattern.MatchString(got) {
		t.Errorf("running status line = %q, want an elapsed clock suffix", got)
	}

	// Each Event re-derives the phrase: streamed text, then a named tool with its target.
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "hi"}})
	if got := statusText(t, m); !strings.Contains(got, "responding") {
		t.Errorf("status line while streaming = %q, want it to contain %q", got, "responding")
	}
	m = step(t, m, eventMsg{Event: domain.ToolCallEvent{
		Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
	}})
	if got := statusText(t, m); !strings.Contains(got, "reading · main.go") {
		t.Errorf("status line during a tool call = %q, want it to name the tool and target", got)
	}

	// Esc registers the stop, and the phrase stays until the worker's terminal Msg unwinds it.
	m = step(t, m, keyEsc())
	if got := statusText(t, m); !strings.Contains(got, "stopping") {
		t.Errorf("status line after esc = %q, want it to contain %q", got, "stopping")
	}
	m = step(t, m, cancelledMsg{})
	if got := statusText(t, m); got != "" {
		t.Errorf("status line after the worker unwound = %q, want the idle empty slot", got)
	}
}

func TestModelResizeDoesNotPanic(t *testing.T) {
	m := newTestModel(t)
	for _, size := range []struct{ w, h int }{{80, 24}, {120, 40}, {200, 60}, {20, 6}, {5, 2}, {1, 1}} {
		m = step(t, m, tea.WindowSizeMsg{Width: size.w, Height: size.h})
		if got := m.View().Content; got == "" {
			t.Errorf("empty view at %dx%d", size.w, size.h)
		}
		// The viewport gives up one column to the scroll-bar gutter (floored at 1 on a tiny window).
		if want := max(1, size.w-scrollbarWidth); m.viewport.Width() != want {
			t.Errorf("viewport width = %d at window width %d, want %d", m.viewport.Width(), size.w, want)
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
	m.transcript.addUser("first question", nil)
	m.transcript.commitAssistant(strings.Repeat("filler above. ", 80), 0)
	m.transcript.addUser("STICKY-PROMPT", nil)
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

// The mouse wheel scrolls the transcript in every state, including idle. The keyboard scroll
// path is state-gated (idle feeds the input box), so without the MouseWheelMsg route in Update a
// finished reply could not be scrolled back — the "scrolling only works intermittently" bug.
func TestMouseWheelScrollsWhileIdle(t *testing.T) {
	m := newTestModel(t) // 80x24, stateIdle
	m.transcript.addUser("question", nil)
	for i := 0; i < 40; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()
	m.viewport.GotoBottom() // scroll to the end so there is room to wheel back up
	m.userScrolled = false  // GotoBottom is not a human scroll; start from a clean flag

	if m.state != stateIdle {
		t.Fatalf("precondition: state = %v, want stateIdle", m.state)
	}
	before := m.viewport.YOffset()
	if before == 0 {
		t.Fatal("precondition: viewport not scrolled; cannot observe a wheel-up")
	}

	m = step(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if m.viewport.YOffset() >= before {
		t.Errorf("wheel-up while idle did not scroll: offset %d → %d", before, m.viewport.YOffset())
	}
	if !m.userScrolled {
		t.Error("a wheel scroll did not set userScrolled; sticky-to-top would yank history back")
	}
}

// firstViewLine returns the top line of the full View (styling stripped). The sticky-header
// overlay writes to View, not the viewport, so firstVisibleLine (viewport-only) cannot see it.
func firstViewLine(m Model) string {
	return strings.SplitN(plain(m.View()), "\n", 2)[0]
}

// A short reply still pins the latest prompt to the top: the trailing-blank padding keeps
// SetYOffset from clamping below row 0 when the content is shorter than the viewport.
func TestStickyPinsShortReply(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("first question", nil)
	m.transcript.commitAssistant("a prior short answer", 0)
	m.transcript.addUser("LATEST-PROMPT", nil)
	m.transcript.commitAssistant("a short reply", 0)
	m.refreshViewport()

	if top := firstViewLine(m); !strings.Contains(top, "LATEST-PROMPT") {
		t.Errorf("top line = %q; want the latest prompt pinned to the top for a short reply", top)
	}
}

// While scrolled, the prompt that owns the on-screen replies is frozen at the top as a sticky
// header, and the next prompt takes over only once it is the natural top line (position: sticky).
func TestStickyHeaderHandoffOnScroll(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("PROMPT-ONE", nil)
	for i := 0; i < 20; i++ {
		m.transcript.commitAssistant("one reply "+strings.Repeat("x", 10), 0)
	}
	m.transcript.addUser("PROMPT-TWO", nil)
	for i := 0; i < 20; i++ {
		m.transcript.commitAssistant("two reply "+strings.Repeat("y", 10), 0)
	}
	m.refreshViewport()
	two := m.userBlocks[len(m.userBlocks)-1] // section two's user-block range in the stashed lines

	// Scrolled into the middle of section one's reply: its prompt is above the top, so it is
	// drawn as the sticky header.
	m.userScrolled = true
	m.viewport.SetYOffset(3)
	if top := firstViewLine(m); !strings.Contains(top, "PROMPT-ONE") {
		t.Errorf("scrolled into section one: top line = %q; want PROMPT-ONE stuck to the top", top)
	}

	// Section two's prompt is the natural top line: it owns the top now.
	m.viewport.SetYOffset(two.start)
	if top := firstViewLine(m); !strings.Contains(top, "PROMPT-TWO") {
		t.Errorf("section two at the top: top line = %q; want PROMPT-TWO", top)
	}

	// One row earlier, the incoming PROMPT-TWO has not yet reached the top: PROMPT-ONE still owns
	// it (the hand-off boundary).
	m.viewport.SetYOffset(two.start - 1)
	if top := firstViewLine(m); strings.Contains(top, "PROMPT-TWO") {
		t.Errorf("hand-off boundary: top line = %q; PROMPT-TWO should not yet own the top row", top)
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

// typeText drives each rune of s through the real key path (handleKey → input.Update → layout),
// the same as a human typing, so the box auto-grows and the scroll re-seat runs per keystroke.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m = step(t, m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

// TestPromptScrollClampedWhileGrowing is the ISSUES #2 regression: typing past the wrap width
// grows the box, and after every keystroke below the max the textarea's internal scroll must sit
// at 0 so the first content row stays visible — not the stale downward offset SetHeight used to
// leave (first line hidden, phantom blank row below). It types a long unbroken run so the box
// grows through several rows, including the exact-width fill points where the widget's own wrap
// adds a trailing row.
func TestPromptScrollClampedWhileGrowing(t *testing.T) {
	m := newTestModel(t)
	iw := m.inputInnerWidth()
	maxHeightSeen := 1
	for i := 1; i <= iw*3+5; i++ {
		m = step(t, m, tea.KeyPressMsg{Code: 'a', Text: "a"})
		rows := inputContentRows(m.input.Value(), iw)
		if rows <= maxInputRows { // still growing: the box holds all its rows, so nothing scrolls
			if off := m.input.ScrollYOffset(); off != 0 {
				t.Fatalf("keystroke %d (rows=%d, height=%d): ScrollYOffset = %d, want 0 (stale scroll after grow)",
					i, rows, m.input.Height(), off)
			}
		}
		if h := m.input.Height(); h > maxHeightSeen {
			maxHeightSeen = h
		}
	}
	if maxHeightSeen < 3 {
		t.Fatalf("box never grew past %d rows; the test did not exercise auto-grow", maxHeightSeen)
	}
	// The caret is at the end of the value, and the last visual row it sits on must be visible.
	if got, want := m.input.Column(), iw*3+5; got != want {
		t.Errorf("caret column = %d, want %d (re-seat must not move the caret)", got, want)
	}
}

// TestPromptScrollClampAtMaxHeight pins the clamp formula: once the content exceeds maxInputRows
// the box stops growing and the textarea scrolls internally, so the offset is exactly
// contentRows - maxInputRows — keeping the caret (at the end) on the bottom visible row.
func TestPromptScrollClampAtMaxHeight(t *testing.T) {
	m := newTestModel(t)
	iw := m.inputInnerWidth()
	m = typeText(t, m, strings.Repeat("a", iw*12)) // ~12 rows of content, well past the 10-row cap
	rows := inputContentRows(m.input.Value(), iw)
	if rows <= maxInputRows {
		t.Fatalf("content only wrapped to %d rows; expected more than the %d cap", rows, maxInputRows)
	}
	if m.input.Height() != maxInputRows {
		t.Fatalf("box height = %d at max, want %d", m.input.Height(), maxInputRows)
	}
	if got, want := m.input.ScrollYOffset(), rows-maxInputRows; got != want {
		t.Errorf("ScrollYOffset at max = %d, want %d (contentRows %d - height %d)", got, want, rows, maxInputRows)
	}
}

// TestPromptScrollShrinkBack deletes a grown box back to a single line: the box shrinks and the
// re-seat clamps the offset back to 0 (a shrink must not strand a downward offset either).
func TestPromptScrollShrinkBack(t *testing.T) {
	m := newTestModel(t)
	iw := m.inputInnerWidth()
	m = typeText(t, m, strings.Repeat("a", iw*3))
	if m.input.Height() <= 1 {
		t.Fatalf("box did not grow before the shrink; height = %d", m.input.Height())
	}
	for m.input.Value() != "" {
		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if h, off := m.input.Height(), m.input.ScrollYOffset(); h != 1 || off != 0 {
		t.Errorf("after deleting back to empty: height = %d, ScrollYOffset = %d; want 1 and 0", h, off)
	}
}

// TestPromptScrollMultiLinePaste checks a multi-line paste (which auto-grows the box in one step)
// leaves the scroll clamped to 0 with the first pasted line visible — the paste path runs the same
// layout re-seat a keystroke does.
func TestPromptScrollMultiLinePaste(t *testing.T) {
	m := newTestModel(t)
	m = step(t, m, tea.PasteMsg{Content: "alpha\nbravo\ncharlie\ndelta"})
	if m.input.Height() < 4 {
		t.Fatalf("box did not grow for the four-line paste: height = %d", m.input.Height())
	}
	if off := m.input.ScrollYOffset(); off != 0 {
		t.Fatalf("ScrollYOffset after paste = %d, want 0", off)
	}
	if top := firstInputRow(m); !strings.Contains(top, "alpha") {
		t.Errorf("first visible input row = %q, want the first pasted line 'alpha'", top)
	}
}

// firstInputRow returns the plain text of the textarea's first rendered visual row.
func firstInputRow(m Model) string {
	return strings.Split(ansi.Strip(m.input.View()), "\n")[0]
}

// TestReseatPreservesStickyColumn guards the re-seat's height-change gate: vertical caret
// navigation does not change the box height, so the re-seat must not run and clobber the
// textarea's remembered goal column. Moving down through a short line and on to a long one lands
// the caret back near the original column — proof the gate left the widget's sticky column intact.
func TestReseatPreservesStickyColumn(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("aaaaaaaaaa\nbb\ncccccccccc")
	m.layout()
	m.input.MoveToBegin()
	m.input.SetCursorColumn(10) // end of the first long line

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown}) // onto the short line "bb"
	if m.input.Line() != 1 {
		t.Fatalf("after one Down: line = %d, want 1", m.input.Line())
	}
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown}) // onto the second long line
	if got := m.input.Column(); got != 10 {
		t.Errorf("caret column after crossing the short line = %d, want 10 (sticky column lost — the re-seat gate must skip a no-op height)", got)
	}
}

// TestDisplayModel proves the footer strips a discovered model path to just its name and drops a
// known weight-file extension, while leaving version dots ("qwen2.5") and bare ids untouched. The
// strip is display-only; opts.Model (sent to the server) is unaffected.
func TestDisplayModel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/Users/me/models/qwen2.5-coder-7b-instruct.gguf", "qwen2.5-coder-7b-instruct"},
		{"/opt/models/Llama-3.1-8B.GGUF", "Llama-3.1-8B"},
		{"model.safetensors", "model"},
		{"qwen2.5-coder", "qwen2.5-coder"}, // no weight extension: the version dot survives
		{"test-model", "test-model"},
		{"", "."}, // filepath.Base("") is "."; never reached in practice (nonEmpty-guarded)
	}
	for _, tc := range cases {
		if got := displayModel(tc.in); got != tc.want {
			t.Errorf("displayModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
