package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
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
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
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
// /version routing
// ----------------------------------------------------------------------------

// /version is synchronous like /clear: it records the resolved build version (Options.Version)
// as a transcript note and launches no worker.
func TestVersionCommandPrintsVersionNote(t *testing.T) {
	opts := testOpts
	opts.Version = "v1.2.3"
	m := newTestModelEng(t, &fakeEngine{}, opts)
	m.input.SetValue("/version")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateIdle {
		t.Errorf("state = %v, want idle (/version must not launch a worker)", m.state)
	}
	if cmd != nil {
		t.Error("/version returned a Cmd; it should not launch a worker")
	}
	if v := m.input.Value(); v != "" {
		t.Errorf("input not cleared: %q", v)
	}
	var note string
	for _, e := range m.transcript.entries {
		if e.kind == entryNote && strings.Contains(e.text, "v1.2.3") {
			note = e.text
		}
	}
	if note == "" {
		t.Errorf("no entryNote carries the version %q; entries = %+v", "v1.2.3", m.transcript.entries)
	}
	if want := "apogee v1.2.3"; note != want {
		t.Errorf("version note = %q, want %q", note, want)
	}
}

// ----------------------------------------------------------------------------
// The one-time start-up box (version-command-and-startup-box plan, item 3)
// ----------------------------------------------------------------------------

// newModel seeds exactly one entry — the start-up box at entries[0] — carrying the resolved
// host / model / context from Options and the CLEAN release version (Options.BaseVersion, no build
// provenance), and it is not a user block (so the sticky header never treats it as a prompt). The
// box reads BaseVersion, never the full Version that /version and --version show.
func TestNewModelSeedsStartupBox(t *testing.T) {
	opts := Options{
		Model:         "/models/gpt-oss-20b.gguf", // displayModel strips the path + weight extension
		Endpoint:      "http://localhost:1234",
		HostAlias:     "test-host",
		ContextWindow: 32768,                   // formatTokens → "32k"
		Version:       "v1.2.3+45.gdeadbeef01", // the full string /version shows — the box must NOT use it
		BaseVersion:   "v1.2.3",                // the clean release version the box displays
	}
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)

	if n := len(m.transcript.entries); n != 1 {
		t.Fatalf("newModel seeded %d entries, want exactly 1 (the start-up box)", n)
	}
	e := m.transcript.entries[0]
	if e.kind != entryStartup {
		t.Fatalf("entries[0].kind = %v, want entryStartup", e.kind)
	}
	if got, want := e.startup.Host, hostDisplay(opts); got != want {
		t.Errorf("startup host = %q, want %q (hostDisplay of Options)", got, want)
	}
	if got, want := e.startup.Model, displayModel(opts.Model); got != want {
		t.Errorf("startup model = %q, want %q (displayModel of Options.Model)", got, want)
	}
	if got, want := e.startup.Context, formatTokens(opts.ContextWindow); got != want {
		t.Errorf("startup context = %q, want %q (formatTokens of Options.ContextWindow)", got, want)
	}
	if got, want := e.startup.Version, opts.BaseVersion; got != want {
		t.Errorf("startup version = %q, want %q (Options.BaseVersion, the clean release version)", got, want)
	}
	if e.startup.Version == opts.Version {
		t.Errorf("startup version = %q equals the full Options.Version; the box must drop the build provenance", e.startup.Version)
	}
	if e.startup.Logo == "" || strings.HasSuffix(e.startup.Logo, "\n") {
		t.Errorf("startup logo = %q, want the embedded art with its trailing newline trimmed", e.startup.Logo)
	}
}

// newStartupView is the single source both newModel's seed and /clear's re-seed read, so the
// fresh-launch box and the post-/clear box can never drift. This pins the extraction: the value the
// helper builds must equal the startupView newModel actually seeded at entries[0], and its Version
// must be the clean BaseVersion — never the full provenance-tagged Options.Version the footer shows.
// (testOpts leaves both version fields empty, so a local opts with distinct Version/BaseVersion is
// used to prove the box drops the build provenance.)
func TestNewStartupViewMatchesSeed(t *testing.T) {
	opts := Options{
		Model:         "/models/gpt-oss-20b.gguf",
		Endpoint:      "http://localhost:1234",
		HostAlias:     "test-host",
		ContextWindow: 32768,
		Version:       "v1.2.3+45.gdeadbeef01", // the full string /version shows — the box must NOT use it
		BaseVersion:   "v1.2.3",                // the clean release version the box displays
	}
	m := newModel(context.Background(), &fakeEngine{}, opts, nil)

	seeded := m.transcript.entries[0].startup
	if got := newStartupView(opts); got != seeded {
		t.Errorf("newStartupView(opts) = %+v, want the seeded box %+v (extraction drifted)", got, seeded)
	}
	if got, want := newStartupView(opts).Version, opts.BaseVersion; got != want {
		t.Errorf("newStartupView version = %q, want %q (Options.BaseVersion, the clean release version)", got, want)
	}
	if newStartupView(opts).Version == opts.Version {
		t.Errorf("newStartupView version = %q equals the full Options.Version; the box must drop the build provenance", opts.Version)
	}
}

// A /clear starts a fresh session: it wipes the scrollback down to only the re-seeded start-up box,
// so the view is identical to a fresh launch. This inverts the prior "the box survives /clear"
// contract — the owner now wants /clear (and /new) to reprint the box and drop everything else.
func TestClearResetsToStartupBox(t *testing.T) {
	m := newTestModelEng(t, &fakeEngine{}, testOpts)
	seedConversation(&m)

	m.input.SetValue("/clear")
	m, _ = stepCmd(t, m, keyEnter())

	if n := len(m.transcript.entries); n != 1 {
		t.Fatalf("transcript has %d entries after /clear, want exactly 1 (only the re-seeded start-up box)", n)
	}
	if k := m.transcript.entries[0].kind; k != entryStartup {
		t.Errorf("entries[0].kind = %v after /clear, want entryStartup", k)
	}
	if got := plain(m.View()); strings.Contains(got, seededAssistantText) {
		t.Errorf("prior conversation still shown after /clear:\n%s", got)
	}
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
	if n := len(m.transcript.entries); n != 3 { // start-up box + user + assistant
		t.Errorf("transcript has %d entries, want 3 (start-up box + user + assistant)", n)
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
	// The gauge names the window it measures against: "<used>/<limit> <pct>%".
	if g := ansi.Strip(m.contextGauge()); !strings.Contains(g, "1k/32k") {
		t.Errorf("context gauge = %q, want the used/limit prefix %q", g, "1k/32k")
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
	if !strings.Contains(ansi.Strip(half), "16k/32k 50% ") {
		t.Errorf("gauge = %q, want the used/limit prefix %q", ansi.Strip(half), "16k/32k 50% ")
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
		m := newModel(context.Background(), eng, testOpts, nil)
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
		m := newModel(context.Background(), eng, testOpts, nil)
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
// The single-worker invariant
// ----------------------------------------------------------------------------
//
// ⏎ while a worker runs no longer does nothing: it STAGES the typed message as an interjection
// (ADR 0025), which launches no second worker and so keeps the invariant this section pins.
// TestEnterWhileRunningStagesRow (interject_test.go) replaced TestModelSubmitWhileRunningIsNoOp
// and asserts both halves — the row is staged, and no worker Cmd comes back.

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

// The rebuilt approval prompt paints through the popup module: the raw tool name in the title,
// the decision legend in the hint, and the pretty-printed args in the body (item 4; D7).
func TestModelApprovalPromptPopupChrome(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{
		Tool:      "write_file",
		Reason:    "write",
		Arguments: json.RawMessage(`{"path":"notes.txt"}`),
	})
	got := plain(m.View())
	for _, want := range []string{
		"approve write_file?",                             // title carries the raw tool name
		"a allow · d deny · s allow-session · esc cancel", // decision legend (the hint)
		"reason: write",                                   // reason on the body's lead line
		"notes.txt",                                       // pretty-printed args in the body
	} {
		if !strings.Contains(got, want) {
			t.Errorf("approval popup missing %q:\n%s", want, got)
		}
	}
}

// A Reason far longer than the window width wraps across body lines IN FULL — no word is lost to
// an ellipsis on this security surface (D7). Every word survives and no overflow marker appears.
func TestModelApprovalReasonWrapsInFull(t *testing.T) {
	words := []string{
		"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india",
		"juliet", "kilo", "lima", "mike", "november", "oscar", "papa", "quebec", "romeo",
		"sierra", "tango", "uniform", "victor", "whiskey", "xray", "yankee", "zulu",
	}
	reason := strings.Join(words, " ") // ~150 chars — wider than the window, so it must wrap
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{Request: domain.ApprovalRequest{Tool: "write_file", Reason: reason}, Reply: reply})

	view := plain(m.View())
	if strings.Contains(view, "more lines") {
		t.Fatalf("reason overflowed into the cap marker; it must fit and wrap in full:\n%s", view)
	}
	for _, w := range words {
		if !strings.Contains(view, w) {
			t.Errorf("reason word %q missing — the reason was truncated, not wrapped:\n%s", w, view)
		}
	}
}

// Every model-authored string (tool name, reason, args) is escape-stripped before rendering, so a
// model-authored ESC byte never reaches the terminal (D8, hardening). As in the ask case, the ESC
// is removed and the color code survives as INERT literal text — its presence in the stripped View
// is exactly the proof the strip happened at the call site (had the ESC survived, plain() would
// have swallowed the whole SGR sequence and the literal would be gone).
func TestModelApprovalEscapeStrips(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{
		Tool:      "write\x1b[31mfile",
		Reason:    "be\x1b[32mcareful",
		Arguments: json.RawMessage("{\"path\":\"x\x1b[33my\"}"), // a real ESC byte inside the args string
	})
	view := plain(m.View())
	for _, want := range []string{"write[31mfile", "be[32mcareful", "x[33my"} {
		if !strings.Contains(view, want) {
			t.Errorf("ESC not stripped at the source, expected inert literal %q:\n%s", want, view)
		}
	}
}

// The pretty-printed args keep their two-space JSON indentation on the rendered body lines
// (embedded-newline layout is preserved end to end, not collapsed by the wrap).
func TestModelApprovalArgsKeepIndentation(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "write_file", Arguments: json.RawMessage(`{"path":"notes.txt"}`)},
		Reply:   reply,
	})
	// prettyJSON indents the "path" line by two spaces; had the indent been collapsed, only the
	// popup's one-space padding would precede the quote. The two-space run proves it survived.
	if got := plain(m.View()); !strings.Contains(got, `  "path"`) {
		t.Errorf("args lost their two-space JSON indentation:\n%s", got)
	}
}

// An args body far taller than the screen caps with the explicit overflow marker and never pushes
// the input box off-screen (D2, the never-clip guarantee — the same as the ask long-question case).
func TestModelApprovalLongArgsCapsBody(t *testing.T) {
	vals := make([]string, 200)
	for i := range vals {
		vals[i] = "value"
	}
	raw, err := json.Marshal(vals) // a 200-element array → ~202 pretty-printed lines
	if err != nil {
		t.Fatalf("marshalling the oversized args: %v", err)
	}

	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "write_file", Arguments: json.RawMessage(raw)},
		Reply:   reply,
	})

	view := plain(m.View())
	if !strings.Contains(view, "more lines)") {
		t.Errorf("oversized args did not show the overflow marker:\n%s", view)
	}
	if !strings.Contains(view, "Send a message") {
		t.Errorf("input box (placeholder) clipped from the View by the oversized args:\n%s", view)
	}
}

// The approval prompt never hides its reason or arguments without saying so. Dropping the body
// floor to zero — which is what stopped a prose-bearing pane overflowing the shortest window it can
// be drawn in — left a 12-to-15-row terminal rendering this pane with the reason and the arguments
// gone and NO marker anywhere: a decision on what a tool is about to do, taken against text the
// pane had silently dropped. The two properties are pinned together here on purpose, because either
// one alone is satisfiable by breaking the other: the pane must fit the window it is drawn in AND
// account for every line it is not showing. Between 12 and 15 rows the budget grants no body row at
// all, so the count rides the title — the row the pane always has, beside the tool name the
// decision turns on.
//
// It runs at narrowOverlayWindow as well as at 80 columns, because the title row was the one place
// the accounting could still be lost: composed at full length and clipped to the pane's width, it
// dropped the count off its end at 42 columns and below, so a terminal that was short AND narrow —
// the same split pane — silently went back to the state this test exists to forbid.
func TestModelApprovalNamesTheProseItCannotShow(t *testing.T) {
	req := domain.ApprovalRequest{
		Tool:      "write_file",
		Reason:    strings.Repeat("this write needs explaining at some length. ", 8),
		Arguments: json.RawMessage(`{"path":"/ws/a/main.go","content":"package main"}`),
	}

	for _, width := range []int{80, narrowOverlayWindow} {
		for _, height := range []int{smallestOverlayWindow, 13, 14, 15, 16, 20, 24} {
			t.Run(fmt.Sprintf("%d×%d", width, height), func(t *testing.T) {
				m := modelWithOverlayRoomAt(t, width, height, Options{Workspace: "/ws/a"})
				pane := m.approvalPrompt(req)
				rows := strings.Split(ansiPattern.ReplaceAllString(pane, ""), "\n")
				flat := strings.Join(rows, "\n")

				if got := lipgloss.Height(pane); got > m.viewport.Height() {
					t.Errorf("approval pane is %d rows on a %d-row viewport (+%d): the input box goes off the frame\n%s",
						got, m.viewport.Height(), got-m.viewport.Height(), flat)
				}
				if !strings.Contains(flat, "approve write_file?") {
					t.Errorf("pane does not carry the tool name the decision turns on:\n%s", flat)
				}
				// Either the whole body is on the screen — its last line is the args' lone close
				// brace, the one tail no wrap can break up — or the pane counts out what is missing,
				// in whichever wording the width can pay for. Never neither.
				bodyComplete := slices.ContainsFunc(rows, func(r string) bool { return strings.Trim(r, "│ ") == "}" })
				if !bodyComplete && !elisionMarkerPattern.MatchString(flat) {
					t.Errorf("pane shows neither the whole body nor a marker for the lines it hid:\n%s", flat)
				}

				// On a window with no body budget the marker has nowhere to go but the title row, which
				// is exactly the case the finding was about — assert the placement, not just presence.
				if maxBody, _, _ := m.popupBudget(panePrompt, 0, 0, popupChrome); maxBody == 0 {
					if got, want := len(rows), 4; got != want { // 2 borders + title + hint
						t.Fatalf("pane with no body budget is %d rows, want %d:\n%s", got, want, flat)
					}
					title := strings.Trim(rows[1], "│ ")
					if !strings.HasPrefix(title, "approve write_file?") || !elisionMarkerPattern.MatchString(title) {
						t.Errorf("title row = %q, want the tool name followed by the elision marker", title)
					}
				}
			})
		}
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

// TestAskGivesTheBorrowedDraftBack pins the stash an ask_user question owes the box it BORROWS.
// The question empties the box so its choices are pre-selectable (D5), and until now that emptying
// simply threw away whatever unsent message the human was part-way through typing — no stash, no
// route back, the one kind of content in the whole TUI that cannot be re-derived from anything.
//
// Both ways the box stops being borrowed are covered, because they close the question through
// different seams: submitAnswer for an answer that goes out, and finishWorker for a question whose
// Exchange dies under it (Esc). On that second path the half-typed ANSWER is the human's too, so it
// is kept rather than clobbered — the draft goes back above it.
func TestAskGivesTheBorrowedDraftBack(t *testing.T) {
	const draft = "the message the question interrupted"

	raise := func(t *testing.T, typed string) (Model, chan domain.AskAnswer) {
		t.Helper()
		m := typeInput(t, newTestModel(t), typed)
		reply := make(chan domain.AskAnswer, 1)
		m = step(t, m, askReqMsg{Request: domain.AskRequest{Question: "which way?"}, Reply: reply})
		if m.state != stateAwaitingAsk {
			t.Fatalf("state = %v, want awaitingAsk", m.state)
		}
		if got := m.input.Value(); got != "" {
			t.Fatalf("the borrowed box holds %q, want it emptied for the answer (D5)", got)
		}
		return m, reply
	}

	t.Run("the answer goes out", func(t *testing.T) {
		m, reply := raise(t, draft)

		m = typeInput(t, m, "teal")
		m, _ = stepCmd(t, m, keyEnter())

		if got := takeAnswer(t, reply); got != "teal" {
			t.Errorf("answer = %q, want %q — the stash must not reach the model", got, "teal")
		}
		if got := m.input.Value(); got != draft {
			t.Errorf("box = %q, want the draft %q back once the question let go of it", got, draft)
		}
	})

	t.Run("the exchange dies under the question", func(t *testing.T) {
		m, _ := raise(t, draft)
		m.cancel = func() {}

		m = typeInput(t, m, "tea")
		m = step(t, m, keyEsc())
		m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})

		if want := draft + "\ntea"; m.input.Value() != want {
			t.Errorf("box = %q, want %q — neither half of what was typed may be discarded",
				m.input.Value(), want)
		}
	})

	// A question that borrowed an EMPTY box owes it nothing back: no phantom text, and the answer
	// path is byte-identical to what it always was.
	t.Run("nothing was borrowed", func(t *testing.T) {
		m, reply := raise(t, "")

		m = typeInput(t, m, "teal")
		m, _ = stepCmd(t, m, keyEnter())

		if got := takeAnswer(t, reply); got != "teal" {
			t.Errorf("answer = %q, want %q", got, "teal")
		}
		if got := m.input.Value(); got != "" {
			t.Errorf("box = %q, want it empty — nothing was borrowed", got)
		}
	})
}

// takeAnswer reads the one reply the model sends on submit, failing if none was sent.
func takeAnswer(t *testing.T, reply chan domain.AskAnswer) string {
	t.Helper()
	select {
	case got := <-reply:
		return got.Text
	default:
		t.Fatal("no answer sent on the reply channel")
		return ""
	}
}

// With choices offered, the popup renders the question body and every choice as a row with the
// first pre-selected; ↓ moves the highlight and ⏎ sends the highlighted label (D5/D9).
func TestModelAskChoicesRoundTrip(t *testing.T) {
	m, reply := newAskModel(t, domain.AskRequest{
		Question: "which one?",
		Choices:  []string{"alpha", "beta", "gamma"},
	})

	view := plain(m.View())
	for _, want := range []string{"which one?", "alpha", "beta", "gamma", "❯ alpha"} {
		if !strings.Contains(view, want) {
			t.Errorf("ask popup missing %q:\n%s", want, view)
		}
	}

	m = step(t, m, keyDown())
	m, _ = stepCmd(t, m, keyEnter())
	if got := takeAnswer(t, reply); got != "beta" {
		t.Errorf("answer = %q, want %q (the second choice)", got, "beta")
	}
	if m.state != stateRunning {
		t.Errorf("state = %v, want running after the pick", m.state)
	}
}

// Typing a custom answer drops the choice highlight (selected −1) and ⏎ sends the typed text;
// deleting back to empty restores the highlight and ⏎ then picks it (D5).
func TestModelAskTypedTextOverridesChoices(t *testing.T) {
	m, reply := newAskModel(t, domain.AskRequest{
		Question: "which one?",
		Choices:  []string{"alpha", "beta"},
	})

	typed := typeInput(t, m, "custom")
	if v := plain(typed.View()); strings.Contains(v, "❯ alpha") {
		t.Errorf("choice highlight still shown while typing a custom answer:\n%s", v)
	}

	back := typed
	for range "custom" {
		back = step(t, back, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if v := plain(back.View()); !strings.Contains(v, "❯ alpha") {
		t.Errorf("choice highlight not restored after deleting back to empty:\n%s", v)
	}
	_, _ = stepCmd(t, back, keyEnter())
	if got := takeAnswer(t, reply); got != "alpha" {
		t.Errorf("answer after restore = %q, want %q", got, "alpha")
	}
}

// ↑/↓ with text in the input drive the textarea cursor, not the choice highlight — the multi-line
// free-text answer must not be stolen from (D5).
func TestModelAskArrowsWithTextKeepSelection(t *testing.T) {
	m, _ := newAskModel(t, domain.AskRequest{
		Question: "which one?",
		Choices:  []string{"alpha", "beta", "gamma"},
	})
	m = typeInput(t, m, "x")
	before := m.askSel
	m = step(t, m, keyDown())
	m = step(t, m, keyUp())
	if m.askSel != before {
		t.Errorf("askSel moved to %d while text was in the input; want unchanged %d", m.askSel, before)
	}
}

// The question and choices are escape-stripped before rendering, so a model-authored ESC byte
// never reaches the terminal (D8, hardening). stripEscapes removes only the ESC byte, leaving the
// color code as INERT literal text ("[31mred"); had the ESC survived, plain() would have consumed
// the whole "\x1b[31m" as a real SGR sequence and the literal would be gone — so its presence in
// the stripped View is exactly the proof the strip happened at the call site.
func TestModelAskEscapeStrips(t *testing.T) {
	m, _ := newAskModel(t, domain.AskRequest{
		Question: "pick\x1b[31mred",
		Choices:  []string{"al\x1b[32mpha"},
	})
	view := plain(m.View())
	if !strings.Contains(view, "pick[31mred") {
		t.Errorf("question ESC not stripped at the source:\n%s", view)
	}
	if !strings.Contains(view, "[32mpha") {
		t.Errorf("choice ESC not stripped at the source:\n%s", view)
	}
}

// A question far taller than the screen caps its body with the explicit overflow marker and never
// pushes the input box off-screen (D2, the never-clip guarantee).
func TestModelAskLongQuestionCapsBody(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	reply := make(chan domain.AskAnswer, 1)
	m = step(t, m, askReqMsg{
		Request: domain.AskRequest{Question: strings.TrimSpace(strings.Repeat("word ", 400))},
		Reply:   reply,
	})

	view := plain(m.View())
	if !strings.Contains(view, "more lines)") {
		t.Errorf("long question did not show the overflow marker:\n%s", view)
	}
	if !strings.Contains(view, "Send a message") {
		t.Errorf("input box (placeholder) clipped from the View by the long question:\n%s", view)
	}
}

// ----------------------------------------------------------------------------
// Per-Turn session saves through the SessionHost seam (session-system plan §4)
// ----------------------------------------------------------------------------

// newSessionModel builds a ready, idle model wired to a persistence host.
func newSessionModel(t *testing.T, eng Engine, host SessionHost) Model {
	t.Helper()
	m := newModel(context.Background(), eng, Options{Sessions: host}, nil)
	return step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
}

// driveOneSave runs one full async save round-trip: a per-Turn snapshot schedules the save, the
// Cmd persists it, and the saveDoneMsg folds back (where a note transition, if any, lands).
func driveOneSave(t *testing.T, m Model, sess domain.Session) Model {
	t.Helper()
	m, cmd := stepCmd(t, m, turnSnapshotMsg{Sess: sess})
	if cmd == nil {
		t.Fatal("a per-Turn snapshot scheduled no save")
	}
	return step(t, m, cmdMsg(cmd)) // fold the saveDoneMsg
}

// maxWriteDrainSteps bounds runWrites so a queue that never settles fails the test instead of
// hanging it. A handful of writes is the most any test stacks up.
const maxWriteDrainSteps = 32

// runWrites drives the record-write queue to quiescence: it runs cmd, folds the completion Msg that
// comes back, and repeats with whatever that dispatched — the Update loop's job, done by hand.
// Record writes are ONE serialized stream (model.go), so a test that asserts on the store has to
// let the queue drain rather than run a single write Cmd in isolation: a rename scheduled while a
// save is in flight is waiting, not lost.
func runWrites(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for range maxWriteDrainSteps {
		if cmd == nil {
			return m
		}
		msg := cmdMsg(cmd)
		if msg == nil {
			return m
		}
		m, cmd = stepCmd(t, m, msg)
	}
	t.Fatal("the record-write queue never settled")
	return m
}

// isQuit reports whether cmd is tea.Quit (running it is safe — it only yields its Msg).
func isQuit(cmd tea.Cmd) bool {
	_, ok := cmdMsg(cmd).(tea.QuitMsg)
	return ok
}

// drainToQuit drives the record-write queue the way runWrites does, but for a quit whose exit is
// waiting on it: it fails unless the drain ends in tea.Quit. That is the clean quit's contract — the
// closing flush is a queued write now, so the program exits from the fold that finds the queue
// empty, not from the keypress.
func drainToQuit(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for range maxWriteDrainSteps {
		if cmd == nil {
			t.Fatal("the record-write queue settled without firing the deferred quit")
		}
		msg := cmdMsg(cmd)
		if _, quit := msg.(tea.QuitMsg); quit {
			return m
		}
		m, cmd = stepCmd(t, m, msg)
	}
	t.Fatal("the deferred quit never fired")
	return m
}

// saveNotes collects the transcript's save-pipeline notes (ok→fail failures and fail→ok
// recoveries), so a test can assert exactly which transitions were surfaced.
func saveNotes(m Model) []string {
	var out []string
	for _, e := range m.transcript.entries {
		if e.kind == entryNote && (strings.HasPrefix(e.text, "session save failed") || e.text == "session saving recovered") {
			out = append(out, e.text)
		}
	}
	return out
}

// A resumed run repaints the stored scrollback beneath the fresh start-up box, closes it with a
// "resumed: <title>" note, and relights the context gauge from the stored fill (item 5 startup
// replay). The start-up box still leads the transcript, so the view reads as a fresh launch with
// the history beneath it.
func TestNewModelReplaysResumedScrollback(t *testing.T) {
	t.Parallel()
	var src transcript
	src.addUser("first question", nil)
	src.addNote("a recorded note")
	blob, err := encodeTranscript(&src)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	m := newModel(context.Background(), &fakeEngine{}, Options{
		Resumed: &ResumedSession{Transcript: blob, Title: "first question", CtxUsed: 4096},
	}, nil)

	if m.ctxUsed != 4096 {
		t.Errorf("ctxUsed after resume = %d; want the stored 4096 (gauge relight)", m.ctxUsed)
	}
	if m.transcript.entries[0].kind != entryStartup {
		t.Errorf("first entry kind = %v; want the start-up box still seeded first", m.transcript.entries[0].kind)
	}
	if !hasEntry(m, entryUser, "first question") {
		t.Error("replayed user message not present in the resumed transcript")
	}
	last := m.transcript.entries[len(m.transcript.entries)-1]
	if last.kind != entryNote || last.text != "resumed: first question" {
		t.Errorf("last entry = {%v, %q}; want a 'resumed: first question' note", last.kind, last.text)
	}

	// The notice is display-only: the human sees it (above), but the next save must not carry it —
	// it is re-derived from the record on every resume, so persisting it would stack copies.
	if !last.ephemeral {
		t.Error("the resume notice is persistent; it must be ephemeral")
	}
	p, ok := m.snapshotPayload(domain.Session{})
	if !ok {
		t.Fatal("snapshotPayload failed on a resumed transcript")
	}
	if strings.Contains(string(p.transcript), "resumed:") {
		t.Errorf("the saved blob carries the resume notice: %s", p.transcript)
	}
	if !strings.Contains(string(p.transcript), "a recorded note") {
		t.Errorf("the saved blob dropped the conversation's own note: %s", p.transcript)
	}
}

// A corrupt (undecodable) blob is never fatal: no replayed entries land, the view is left fresh,
// and an honest degrade note says the model still remembers.
func TestNewModelResumeCorruptBlobDegrades(t *testing.T) {
	t.Parallel()
	m := newModel(context.Background(), &fakeEngine{}, Options{
		Resumed: &ResumedSession{Transcript: []byte("{ not json"), Title: "broken"},
	}, nil)

	if hasEntry(m, entryUser, "") {
		t.Error("a corrupt blob replayed a user entry; want none")
	}
	last := m.transcript.entries[len(m.transcript.entries)-1]
	want := "resumed: broken (no scrollback recorded — the model still remembers)"
	if last.kind != entryNote || last.text != want {
		t.Errorf("degrade note = {%v, %q}; want {note, %q}", last.kind, last.text, want)
	}
}

// hasEntry reports whether the transcript holds an entry of the given kind; a non-empty want must
// also match the entry text exactly.
func hasEntry(m Model, kind entryKind, want string) bool {
	for _, e := range m.transcript.entries {
		if e.kind == kind && (want == "" || e.text == want) {
			return true
		}
	}
	return false
}

// ----------------------------------------------------------------------------
// Interrupted-Exchange resume (session-system plan §8)
// ----------------------------------------------------------------------------

// A session restored mid-task (the engine reports InExchange) resumes on /continue via the
// step-only drive: it launches a worker but adds NO "/continue" user block — the interrupted note
// already stands and the transcript is left untouched. Contrast the canned path in
// TestContinueAfterLiveCancelStaysCanned, which DOES add the block.
func TestContinueOnInterruptedResumesStepOnly(t *testing.T) {
	eng := &fakeEngine{inExchange: true} // a snapshot restored mid-Exchange
	m := newTestModelEng(t, eng, testOpts)

	m.input.SetValue("/continue")
	m, cmd := stepCmd(t, m, keyEnter())

	if m.state != stateRunning {
		t.Fatalf("state = %v, want running (the resume worker launched)", m.state)
	}
	if cmd == nil {
		t.Error("interrupted /continue launched no worker Cmd")
	}
	if m.cancel == nil {
		t.Error("interrupted /continue did not store the worker CancelFunc")
	}
	if hasEntry(m, entryUser, "/continue") {
		t.Error("interrupted /continue added a /continue user block; the resume drive leaves the transcript untouched")
	}
	if eng.submits() != 0 {
		t.Errorf("Submit calls = %d, want 0 — a resume re-Steps the open Exchange, it never Submits", eng.submits())
	}
}

// A fresh message typed on an interrupted session supersedes the stale half-Exchange: the Model
// aborts the open Exchange first (synchronously, so a later Submit is accepted) and notes the
// discard, then records the message and launches the normal worker.
func TestSubmitOnInterruptedAbortsWithNote(t *testing.T) {
	eng := &fakeEngine{inExchange: true}
	m := newTestModelEng(t, eng, testOpts)

	m.input.SetValue("do something else instead")
	m, cmd := stepCmd(t, m, keyEnter())

	if eng.aborts() != 1 {
		t.Fatalf("AbortExchange calls = %d, want 1 (a fresh message discards the interrupted work)", eng.aborts())
	}
	// The abort is synchronous; the Submit rides the worker Cmd (not run here), so it is still 0 —
	// which is exactly what proves the abort precedes the Submit.
	if eng.submits() != 0 {
		t.Errorf("Submit calls = %d immediately after enter, want 0 (abort-then-submit ordering)", eng.submits())
	}
	if !hasEntry(m, entryNote, "discarded the interrupted work — continuing fresh from your message") {
		t.Error("the discard was not surfaced as a note")
	}
	if !hasEntry(m, entryUser, "do something else instead") {
		t.Error("the fresh message was not recorded as a user block")
	}
	if m.state != stateRunning || cmd == nil {
		t.Error("the fresh message did not launch the normal exchange worker")
	}
}

// /clear on an interrupted session scraps the open Exchange before clearing — ClearContext refuses
// mid-Exchange, so startNewSession aborts first — then resets the view to the re-seeded start-up box.
func TestClearOnInterruptedAbortsThenClears(t *testing.T) {
	eng := &fakeEngine{inExchange: true}
	m := newTestModelEng(t, eng, testOpts)
	seedConversation(&m)

	m.input.SetValue("/clear")
	m = step(t, m, keyEnter())

	if eng.aborts() != 1 {
		t.Fatalf("AbortExchange calls = %d, want 1 (an interrupted session must be scrapped before it can clear)", eng.aborts())
	}
	if eng.clearCalls != 1 {
		t.Fatalf("ClearContext calls = %d, want 1", eng.clearCalls)
	}
	if m.state != stateIdle {
		t.Errorf("state = %v, want idle after /clear", m.state)
	}
	if n := len(m.transcript.entries); n != 1 || m.transcript.entries[0].kind != entryStartup {
		t.Errorf("transcript = %d entries after /clear, want just the re-seeded start-up box", n)
	}
}

// A --resume/--continue start of a mid-task session (Options.Resumed.InExchange) appends the
// interrupted note at construction, so the human sees how to pick the work back up; a cleanly-closed
// resume gets no such note.
func TestResumedMidExchangeShowsInterruptedNote(t *testing.T) {
	opts := testOpts
	opts.Resumed = &ResumedSession{Title: "big task", InExchange: true}
	m := newTestModelEng(t, &fakeEngine{}, opts)
	if !hasEntry(m, entryNote, interruptedNote) {
		t.Error("a mid-Exchange resume at startup did not append the interrupted note")
	}

	clean := testOpts
	clean.Resumed = &ResumedSession{Title: "finished task", InExchange: false}
	cm := newTestModelEng(t, &fakeEngine{}, clean)
	if hasEntry(cm, entryNote, interruptedNote) {
		t.Error("a cleanly-closed resume wrongly showed the interrupted note")
	}
}

// After a LIVE cancel (Esc, not an interrupted restore) the Model has already aborted the Exchange,
// so InExchange is false and /continue stays the canned "Please continue" submit — it adds the
// /continue user block, the tell-tale of the canned path the interrupted path omits.
func TestContinueAfterLiveCancelStaysCanned(t *testing.T) {
	eng := &fakeEngine{}
	m := newTestModelEng(t, eng, testOpts)
	m.cancel = func() {} // stand in for a live worker
	m.state = stateRunning
	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})

	if eng.InExchange() {
		t.Fatal("engine still inExchange after a live cancel; /continue would wrongly take the resume path")
	}

	m.input.SetValue("/continue")
	m = step(t, m, keyEnter())
	if !hasEntry(m, entryUser, "/continue") {
		t.Error("/continue after a live cancel did not take the canned-submit path (no /continue user block)")
	}
}

// A clean quit (idle, with a non-empty conversation) flushes the Engine snapshot and the derived
// metadata through the SessionHost seam, then quits. The flush is a queued record write like any
// other, so the exit fires from the fold that finds the queue drained rather than from the keypress.
func TestModelFlushesThroughSeamOnCleanQuit(t *testing.T) {
	marker := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"saved":true}`)}
	eng := &fakeEngine{snapshotFn: func() (domain.Session, error) { return marker, nil }}
	host := &fakeSessionHost{}
	m := newSessionModel(t, eng, host)
	m.transcript.addUser("hello", nil) // give it content worth saving

	next, cmd := ctrlCQuit(t, m)
	// quit() only arms quitting on the branch that does NOT return tea.Quit, so this is the
	// deferred exit — asserted without running cmd, which is the flush itself.
	if !next.quitting {
		t.Fatal("a clean quit exited before its flush had reached the host")
	}
	if n := len(host.savedCalls()); n != 0 {
		t.Fatalf("Save calls before the flush Cmd ran = %d; want 0 — the flush is a queued write", n)
	}
	drainToQuit(t, next, cmd)

	calls := host.savedCalls()
	if len(calls) != 1 {
		t.Fatalf("Save calls on a clean quit = %d; want 1", len(calls))
	}
	if string(calls[0].sess.State) != string(marker.State) {
		t.Errorf("flushed snapshot = %q; want the Engine's snapshot %q", calls[0].sess.State, marker.State)
	}
	if calls[0].title != "hello" {
		t.Errorf("flushed title = %q; want the first user message", calls[0].title)
	}
}

// The clean quit's flush is SERIALIZED against the writes already in flight, which is the whole
// reason it stopped calling the host directly: the synchronous Save read the host's active session
// while a Rename was between its store write and mirroring the new title onto it, and wrote the
// pre-rename title straight back over the name the human had just chosen (audit 2026-08-01
// follow-up). Asserted at the fold layer — the recording host serialises its own calls under one
// mutex, so it cannot see the collision — by proving the flush WAITS for the rename rather than
// going out beside it, and that the exit waits for both.
func TestQuitFlushWaitsForAnInFlightRename(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "s1", "old title", "/ws", time.Now(), 0, nil)
	m := newSessionModel(t, &fakeEngine{}, host)
	m.transcript.addUser("hello", nil)

	// A rename goes out and is left in flight: its Cmd is held rather than run.
	renameCmd := m.renameSession("s1", "a better name")
	if renameCmd == nil {
		t.Fatal("the rename dispatched nothing")
	}

	next, cmd := ctrlCQuit(t, m)
	if cmd != nil {
		t.Fatal("the quit flush dispatched a Save beside the in-flight rename instead of queueing")
	}
	if !next.quitting {
		t.Fatal("the quit exited while a record write was still in flight")
	}
	if len(host.savedCalls()) != 0 {
		t.Fatalf("Saves during the in-flight rename = %+v; want none", host.savedCalls())
	}
	if len(next.pendingWrites) != 1 || next.pendingWrites[0].kind != writeSave {
		t.Fatalf("pending writes = %+v; want the closing flush waiting behind the rename", next.pendingWrites)
	}

	// Finishing the rename releases the flush, and finishing the flush fires the exit.
	next, cmd = stepCmd(t, next, cmdMsg(renameCmd))
	drainToQuit(t, next, cmd)
	if n := len(host.savedCalls()); n != 1 {
		t.Errorf("Save calls once the queue drained = %d; want the single closing flush", n)
	}
	want := []renameCall{{id: "s1", title: "a better name"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v; want %+v — the quit must not skip a write it was asked for", got, want)
	}
}

// An empty conversation is not worth a record — a per-Turn snapshot and a clean quit both save
// nothing when the transcript holds only the seeded start-up box.
func TestModelEmptyTranscriptNeverSaves(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)

	if _, cmd := stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{}}); cmd != nil {
		t.Error("a per-Turn snapshot scheduled a save for a transcript holding only the start-up box")
	}
	_, cmd := ctrlCQuit(t, m)
	if n := len(host.savedCalls()); n != 0 {
		t.Errorf("Save calls for an empty conversation = %d; want 0", n)
	}
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("quit did not exit")
	}
}

// A pre-prompt note is not a conversation. A launch spent on a slash command leaves a persisted,
// non-ephemeral note in the scrollback, but no prompt was ever sent — so neither the quit flush nor
// an idle boundary (both saveAtIdle) may file a record, or the history fills with "Session <date>"
// entries reading 0 messages. Sending a prompt opens both boundaries.
func TestModelPrePromptNoteNeverSaves(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	m.transcript.addNote("confinement: workspace (fs-fenced)") // e.g. the /confine status note

	if cmd := m.saveAtIdle(); cmd != nil {
		t.Error("an idle boundary scheduled a save before the first prompt")
	}
	// The quit exits on the spot: with nothing to flush there is no write to wait for.
	if _, cmd := ctrlCQuit(t, m); !isQuit(cmd) {
		t.Error("the quit flush scheduled a save before the first prompt")
	}
	if n := len(host.savedCalls()); n != 0 {
		t.Errorf("Save calls before the first prompt = %d; want 0 — a slash-command note is not a conversation", n)
	}

	m.transcript.addUser("now do something", nil)

	cmd := m.saveAtIdle()
	if cmd == nil {
		t.Fatal("an idle boundary scheduled no save after the first prompt")
	}
	m = step(t, m, cmdMsg(cmd)) // run the save Cmd so its Save reaches the host, and fold it
	m, quitCmd := ctrlCQuit(t, m)
	drainToQuit(t, m, quitCmd)
	if n := len(host.savedCalls()); n != 2 {
		t.Errorf("Save calls after the first prompt = %d; want 2 (the idle boundary and the quit flush)", n)
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
	host := &fakeSessionHost{}
	m := newSessionModel(t, eng, host)
	m.transcript.addUser("hi", nil)
	m.state = stateRunning
	m.cancel = func() {}

	next, cmd := ctrlCQuit(t, m)
	if snapshotted || len(host.savedCalls()) != 0 {
		t.Error("snapshotted while a worker was running (would race the single-goroutine Agent)")
	}
	// The exit is DEFERRED while busy: an immediate tea.Quit would race runRoot's Close()
	// teardown against the still-running worker.
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
		t.Error("ctrl+c×2 while busy quit immediately instead of waiting for the worker")
	}
	// The worker's terminal Msg fires the deferred quit — and still saves nothing: finishWorker's
	// quitting branch short-circuits before the idle finisher, so the deferred exit never
	// snapshots the cancelled boundary (the per-Turn saves already captured every completed Turn).
	_, doneCmd := stepCmd(t, next, cancelledMsg{})
	if snapshotted || len(host.savedCalls()) != 0 {
		t.Error("the deferred quit snapshotted the cancelled boundary; the busy path must not save")
	}
	if _, isQuit := cmdMsg(doneCmd).(tea.QuitMsg); !isQuit {
		t.Error("the worker's terminal Msg did not fire the deferred quit")
	}
}

// A nil host (session saving disabled) must not break the quit path.
func TestModelQuitWithoutSaver(t *testing.T) {
	m := newTestModel(t) // testOpts carries no Sessions
	m.transcript.addUser("hi", nil)
	_, cmd := ctrlCQuit(t, m)
	if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); !isQuit {
		t.Error("quit with no saver did not exit")
	}
}

// A completed per-Turn snapshot persists the engine snapshot, the encoded scrollback, and the
// derived metadata through the seam.
func TestModelPerTurnSaveEncodesTranscript(t *testing.T) {
	marker := domain.Session{Version: domain.SessionVersion, State: json.RawMessage(`{"turn":1}`)}
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	m.transcript.addUser("summarise the plan", nil)
	m.ctxUsed = 4096

	m, cmd := stepCmd(t, m, turnSnapshotMsg{Sess: marker})
	if cmd == nil {
		t.Fatal("a per-Turn snapshot scheduled no save")
	}
	done, ok := cmdMsg(cmd).(saveDoneMsg)
	if !ok || done.Err != nil {
		t.Fatalf("save Cmd yielded %T (err %v); want a clean saveDoneMsg", done, done.Err)
	}

	calls := host.savedCalls()
	if len(calls) != 1 {
		t.Fatalf("Save calls = %d; want 1 per Turn", len(calls))
	}
	got := calls[0]
	if string(got.sess.State) != string(marker.State) {
		t.Errorf("saved snapshot = %q; want the worker's snapshot %q", got.sess.State, marker.State)
	}
	if got.title != "summarise the plan" {
		t.Errorf("title = %q; want the first user message", got.title)
	}
	if got.userMsgs != 1 {
		t.Errorf("userMsgs = %d; want 1", got.userMsgs)
	}
	if got.ctxUsed != 4096 {
		t.Errorf("ctxUsed = %d; want the live context fill 4096", got.ctxUsed)
	}
	entries, err := decodeTranscript(got.transcript)
	if err != nil {
		t.Fatalf("decode persisted transcript: %v", err)
	}
	if len(entries) != 1 || entries[0].kind != entryUser || entries[0].text != "summarise the plan" {
		t.Errorf("persisted transcript = %+v; want the single user entry", entries)
	}
}

// A completed Exchange takes the Model's OWN snapshot at idle (finishWorker → saveAtIdle) and
// persists it — the closing-boundary save that catches state after the last per-Turn snapshot.
func TestModelSavesAtIdleOnExchangeDone(t *testing.T) {
	marker := domain.Session{State: json.RawMessage(`{"idle":true}`)}
	eng := &fakeEngine{snapshotFn: func() (domain.Session, error) { return marker, nil }}
	host := &fakeSessionHost{}
	m := newSessionModel(t, eng, host)
	m.transcript.addUser("hello", nil)
	m.state = stateRunning
	m.cancel = func() {}

	_, cmd := stepCmd(t, m, exchangeDoneMsg{Result: domain.StepResult{Status: domain.StatusExchangeComplete}})
	if cmd == nil {
		t.Fatal("the idle finisher scheduled no save")
	}
	cmdMsg(cmd)
	calls := host.savedCalls()
	if len(calls) != 1 || string(calls[0].sess.State) != string(marker.State) {
		t.Fatalf("idle save = %+v; want one save of the Model's own snapshot", calls)
	}
}

// Single-flight: snapshots that arrive while a save is in flight coalesce (latest-wins), so
// exactly one is dispatched when the running save reports back — the older intermediate is dropped.
func TestModelSaveSingleFlightCoalesces(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	m.transcript.addUser("hi", nil)

	s1 := domain.Session{State: json.RawMessage(`{"n":1}`)}
	s2 := domain.Session{State: json.RawMessage(`{"n":2}`)}
	s3 := domain.Session{State: json.RawMessage(`{"n":3}`)}

	// The first snapshot schedules a save (busy); leave its Cmd unrun so the next two coalesce.
	m, cmd1 := stepCmd(t, m, turnSnapshotMsg{Sess: s1})
	if cmd1 == nil {
		t.Fatal("the first snapshot scheduled no save")
	}
	m, cmd2 := stepCmd(t, m, turnSnapshotMsg{Sess: s2})
	m, cmd3 := stepCmd(t, m, turnSnapshotMsg{Sess: s3})
	if cmd2 != nil || cmd3 != nil {
		t.Fatal("a snapshot while a save was in flight dispatched instead of coalescing")
	}

	// Finish the in-flight save, then fold its saveDoneMsg: the latest coalesced payload (s3)
	// dispatches; s2 was superseded.
	m, cmd4 := stepCmd(t, m, cmdMsg(cmd1)) // cmdMsg(cmd1) runs Save(s1) and yields its saveDoneMsg
	if cmd4 == nil {
		t.Fatal("the coalesced save was not dispatched after saveDoneMsg")
	}
	cmdMsg(cmd4) // runs Save(s3)

	calls := host.savedCalls()
	if len(calls) != 2 {
		t.Fatalf("Save calls = %d; want 2 (s1, then the coalesced s3)", len(calls))
	}
	if string(calls[0].sess.State) != `{"n":1}` {
		t.Errorf("first save = %q; want s1", calls[0].sess.State)
	}
	if string(calls[1].sess.State) != `{"n":3}` {
		t.Errorf("second save = %q; want the latest coalesced s3 (s2 dropped)", calls[1].sess.State)
	}
}

// The browser's own record writes join the same single-flight queue as the per-Turn save, because
// they write the same file: Store.Rename re-reads and re-writes the whole record, and Delete removes
// it, so either one running beside an in-flight Save loses a Turn (audit 2026-08-01). The queue is
// asserted at the FOLD layer — which Cmd was dispatched and what is still waiting — because the
// recording host serialises every call under one mutex of its own and so cannot see the collision.
func TestSessionBrowserWritesQueueBehindAnInFlightSave(t *testing.T) {
	host := &fakeSessionHost{}
	storeMeta(host, "s1", "old title", "/ws", time.Now(), 0, nil)
	m := newSessionModel(t, &fakeEngine{}, host)
	m.transcript.addUser("hi", nil)

	// A per-Turn save goes out and is left in flight: its Cmd is held rather than run.
	m, saveCmd := stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{State: json.RawMessage(`{"n":1}`)}})
	if saveCmd == nil {
		t.Fatal("the per-Turn snapshot scheduled no save")
	}

	// Both browser verbs are asked for while it runs. Each must WAIT, in the order asked.
	cmd := m.renameSession("s1", "a better name")
	if cmd != nil {
		t.Fatal("a browser rename during an in-flight save dispatched a parallel record write")
	}
	cmd = m.deleteSession("s2")
	if cmd != nil {
		t.Fatal("a browser delete during an in-flight save dispatched a parallel record write")
	}
	if len(host.renamedTitles()) != 0 {
		t.Fatalf("renames = %+v; want none while a save is in flight", host.renamedTitles())
	}
	if len(m.pendingWrites) != 2 {
		t.Fatalf("pending writes = %+v; want the rename and the delete both waiting", m.pendingWrites)
	}
	if m.pendingWrites[0].kind != writeRename || m.pendingWrites[1].kind != writeDelete {
		t.Errorf("queue order = %+v; want the writes in the order they were asked for", m.pendingWrites)
	}

	// Finishing the save releases exactly one write, and finishing that one releases the next.
	m, cmd = stepCmd(t, m, cmdMsg(saveCmd))
	msg := cmdMsg(cmd)
	if _, ok := msg.(recordWriteDoneMsg); !ok {
		t.Fatalf("the save-complete dispatched %T; want the single queued rename", msg)
	}
	m, cmd = stepCmd(t, m, msg)
	m = runWrites(t, m, cmd)
	if len(m.pendingWrites) != 0 {
		t.Errorf("pending writes after the drain = %+v; want an empty queue", m.pendingWrites)
	}
	want := []renameCall{{id: "s1", title: "a better name"}}
	if got := host.renamedTitles(); !reflect.DeepEqual(got, want) {
		t.Errorf("renames = %+v, want %+v once the save had finished", got, want)
	}
}

// Saves coalesce latest-wins, but never ACROSS a retarget. Rotate and Activate move which record
// later saves resolve against, so a save queued before one and a save queued after it describe
// DIFFERENT records: letting the newer supersede the older would write the incoming conversation
// into the outgoing record and drop the outgoing conversation's own last state entirely (audit
// 2026-08-01 follow-up). Within one segment the coalescing is exactly as it was.
func TestSavesDoNotCoalesceAcrossARetarget(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	m.transcript.addUser("hi", nil)

	// One save in flight, a second coalescing behind it, then the retarget, then a third save — which
	// belongs to the session the retarget opens, not to the one the first two describe.
	m, saveCmd := stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{State: json.RawMessage(`{"n":1}`)}})
	if saveCmd == nil {
		t.Fatal("the first snapshot scheduled no save")
	}
	m, _ = stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{State: json.RawMessage(`{"n":2}`)}})
	m.queueWrite(recordWrite{kind: writeRotate})
	m, _ = stepCmd(t, m, turnSnapshotMsg{Sess: domain.Session{State: json.RawMessage(`{"n":3}`)}})

	wantKinds := []recordWriteKind{writeSave, writeRotate, writeSave}
	if len(m.pendingWrites) != len(wantKinds) {
		t.Fatalf("pending writes = %+v; want a save, the rotate, and a second save", m.pendingWrites)
	}
	for i, want := range wantKinds {
		if got := m.pendingWrites[i].kind; got != want {
			t.Fatalf("pending write %d = kind %d; want kind %d (queue %+v)", i, got, want, m.pendingWrites)
		}
	}
	if got := string(m.pendingWrites[0].payload.sess.State); got != `{"n":2}` {
		t.Errorf("pre-rotate save = %s; want the latest of the two that precede it", got)
	}
	if got := string(m.pendingWrites[2].payload.sess.State); got != `{"n":3}` {
		t.Errorf("post-rotate save = %s; want the snapshot taken after the retarget", got)
	}

	// Drained, that is one record per conversation: the outgoing one keeps its own last save, and the
	// save taken after the rotate mints a fresh id rather than overwriting it.
	m, cmd := stepCmd(t, m, cmdMsg(saveCmd))
	m = runWrites(t, m, cmd)
	calls := host.savedCalls()
	if len(calls) != 3 {
		t.Fatalf("saves = %+v; want three (n1, the coalesced n2, then n3)", calls)
	}
	if calls[0].id != "s1" || calls[1].id != "s1" {
		t.Errorf("pre-rotate save ids = %q/%q; want both under the outgoing record", calls[0].id, calls[1].id)
	}
	if string(calls[1].sess.State) != `{"n":2}` {
		t.Errorf("last pre-rotate save = %s; want n2", calls[1].sess.State)
	}
	if calls[2].id == "s1" || string(calls[2].sess.State) != `{"n":3}` {
		t.Errorf("post-rotate save = %s under %q; want n3 under a freshly minted id", calls[2].sess.State, calls[2].id)
	}
}

// A save failure is soft: the ok→fail edge and the fail→ok recovery each note exactly once, and
// nothing else interrupts the conversation.
func TestModelSaveFailureNotesTransitions(t *testing.T) {
	host := &fakeSessionHost{}
	m := newSessionModel(t, &fakeEngine{}, host)
	m.transcript.addUser("hi", nil)
	sess := domain.Session{State: json.RawMessage(`{}`)}

	m = driveOneSave(t, m, sess) // save 1 succeeds — no note
	host.saveErr = errors.New("disk full")
	m = driveOneSave(t, m, sess) // save 2 fails — ok→fail note
	m = driveOneSave(t, m, sess) // save 3 fails again — no second failure note
	host.saveErr = nil
	m = driveOneSave(t, m, sess) // save 4 succeeds — fail→ok note

	notes := saveNotes(m)
	if len(notes) != 2 {
		t.Fatalf("save notes = %d %q; want exactly two (ok→fail, fail→ok)", len(notes), notes)
	}
	if !strings.Contains(notes[0], "session save failed: disk full") {
		t.Errorf("first note = %q; want the ok→fail failure note", notes[0])
	}
	if notes[1] != "session saving recovered" {
		t.Errorf("second note = %q; want the fail→ok recovery note", notes[1])
	}
}

// The session-title heuristic mirrors apogee-code: the first line kept when it fits, truncated at
// a word boundary with an ellipsis when it does not, and a dated fallback for an empty message or
// one that opens a code fence.
func TestSessionTitle(t *testing.T) {
	long := "The quick brown fox jumps over the lazy dog and then keeps running"
	cases := []struct {
		name, in, want string
		prefix         bool // want is a prefix match (the dated fallback carries today's date)
	}{
		{name: "short line kept", in: "fix the login bug", want: "fix the login bug"},
		{name: "first line only", in: "rename the store\nand add tests", want: "rename the store"},
		{name: "word-boundary truncate", in: long, want: "The quick brown fox jumps over the lazy dog and…"},
		{name: "code fence falls back", in: "```go\nfunc main() {}\n```", want: "Session ", prefix: true},
		{name: "empty falls back", in: "   ", want: "Session ", prefix: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sessionTitle(tc.in)
			if tc.prefix {
				if !strings.HasPrefix(got, tc.want) {
					t.Errorf("sessionTitle(%q) = %q; want a %q… fallback", tc.in, got, tc.want)
				}
				return
			}
			if got != tc.want {
				t.Errorf("sessionTitle(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Layout: the status line and resizing
// ----------------------------------------------------------------------------

// The footer carries the host alias, model, workspace directory, and mode. The full endpoint
// URL is no longer shown — the footer uses the host alias — and neither is the context window:
// that is the gauge's fact now, measured live against what the conversation has spent, so the
// footer's last segment is the local directory the session is rooted in. (The status line's own
// left slot is the live activity, covered by TestModelStatusLineActivity; at idle it is empty.)
func TestModelStatusLine(t *testing.T) {
	opts := testOpts
	opts.Workspace = "/ws/proj"
	m := step(t, newModel(context.Background(), &fakeEngine{}, opts, nil), tea.WindowSizeMsg{Width: 80, Height: 24})

	got := plain(m.View())
	// The footer renders the mode as a friendly, spaced label (ask-before → "ask before").
	for _, want := range []string{"test-host", "test-model", "/ws/proj", "ask before"} {
		if !strings.Contains(got, want) {
			t.Errorf("status/footer missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "http://localhost:1234") {
		t.Errorf("footer shows the full endpoint URL; want the host alias instead:\n%s", got)
	}
	// The start-up box still states the window, so the window's departure is asserted against the
	// footer line itself rather than the whole view.
	if footer := ansiPattern.ReplaceAllString(m.footerContent(m.width), ""); strings.Contains(footer, formatTokens(opts.ContextWindow)) {
		t.Errorf("footer = %q, want the context window gone — the gauge states it now", footer)
	}
}

// TestFooterViewIsOneFramelessLine pins the footer's shape once the prompt box took its own bottom
// edge back. The footer used to be three rows of chrome — a ├──┤ divider standing in for the edge
// the box was missing, the content line between two │ bars, and a ╰──╯ rule closing the pair — and
// it is now ONE row carrying no border glyph of any kind. What it carries instead is the STATUS
// LINE's posture, one row below the box rather than one row above it: a bodyIndent lead, the black
// field filled to the window's full width, and the mode marker ending bodyIndent short of the edge
// — the very column the gauge above it ends in (layout.md, "The status line's right slot").
//
// The heavy horizontal is still named here, built from its code point so this source file stays
// clear of the literal glyph: it was the retired mixed-weight rule rune, and "no border glyphs at
// all" is the stronger form of the thin-rule property this test used to hold.
func TestFooterViewIsOneFramelessLine(t *testing.T) {
	m := newTestModel(t)
	view := m.footerView()

	if got := lipgloss.Height(view); got != footerHeight {
		t.Fatalf("footer is %d rows, want %d — the one frameless content line", got, footerHeight)
	}

	flat := ansiPattern.ReplaceAllString(view, "")
	for _, glyph := range []string{"├", "┤", "╰", "╯", "│", "─", string(rune(0x2501))} {
		if strings.Contains(flat, glyph) {
			t.Errorf("footer row carries the border glyph %q; the box closes its own frame now: %q", glyph, flat)
		}
	}
	if got, want := leadingColumns(t, view), len(bodyIndent); got != want {
		t.Errorf("footer opens in column %d, want the %d-column body lead the status line leads with", got, want)
	}
	if got := m.th.measure.Width(view); got != m.width {
		t.Errorf("footer renders %d columns, want the full %d — one unbroken black field", got, m.width)
	}
	if margin := m.th.footerText.Render(bodyIndent); !strings.HasSuffix(view, margin) {
		t.Errorf("footer does not end with the styled %q margin: %q", bodyIndent, view)
	}
	if want := modeLabel(m.opts.Mode) + bodyIndent; !strings.HasSuffix(flat, want) {
		t.Errorf("footer row = %q, want it to end %q — the column the gauge above it ends in", flat, want)
	}
}

// TestTopRuleHairlineRow proves the ▔ top-edge hairline is a standalone full-width row (topRule)
// that caps the bottom chrome ABOVE the status line — the input box no longer carries it — so
// the status line sits directly above the input box (the owner's requested ordering). It also
// checks layout budgets the row so the viewport shrinks by it. Styling is stripped first so the
// assertions are over the runes, not the black-field escapes.
//
// The row is UNBROKEN here because newTestModel's session is unnamed and its transcript carries no
// user text, so it has no name to wear (sessionRuleName). What the row looks like once a session HAS
// a name is TestTopRuleCarriesSessionName's business; this test is about the row's position.
func TestTopRuleHairlineRow(t *testing.T) {
	m := newTestModel(t)

	if got, want := ansi.Strip(m.topRule()), strings.Repeat("▔", m.width); got != want {
		t.Errorf("top rule = %q, want %d ▔ runes spanning the window", got, m.width)
	}
	// The hairline moved out of the input box; the box's own render must no longer contain it.
	if strings.Contains(ansi.Strip(m.inputView()), "▔") {
		t.Errorf("input box still contains a ▔ hairline; it must live in topRule above the status line")
	}

	// Full-view ordering: the ▔ rule, then the status line, then the input box's top border — so
	// the rule is one row above the status line and the status line hugs the box.
	lines := strings.Split(plain(m.View()), "\n")
	ruleRow := -1
	for i, ln := range lines {
		if strings.Contains(ln, "▔") {
			ruleRow = i
			break
		}
	}
	if ruleRow < 0 {
		t.Fatalf("no ▔ top-edge hairline found in the rendered view")
	}
	boxTop := -1
	for i := ruleRow + 1; i < len(lines); i++ {
		if strings.Contains(lines[i], "╭") {
			boxTop = i
			break
		}
	}
	if boxTop != ruleRow+2 {
		t.Errorf("input box top border at row %d, want %d (▔ rule, then the status line, then the box)", boxTop, ruleRow+2)
	}

	want := m.height - topRuleHeight - statusHeight - gapHeight -
		(m.inputRows() + inputBorderRows) - footerHeight - bottomRuleHeight
	if got := m.viewport.Height(); got != want {
		t.Errorf("viewport height = %d, want %d (window less both rules, status, gap, input box, footer)", got, want)
	}
}

// blankRow reports whether a composed frame row carries nothing but spaces — the form the frame's
// gap row takes once styling is stripped.
func blankRow(s string) bool { return strings.TrimSpace(s) == "" }

// frameRule returns the composed frame's rows and the index of its ▔ top-edge hairline — the row
// that caps the bottom chrome, and so the row every transcript-side pane is measured against.
func frameRule(t *testing.T, m Model) ([]string, int) {
	t.Helper()
	rows := strings.Split(plain(m.View()), "\n")
	for i, ln := range rows {
		if strings.Contains(ln, "▔") {
			return rows, i
		}
	}
	t.Fatalf("no ▔ top-edge hairline in the composed frame:\n%s", strings.Join(rows, "\n"))
	return nil, -1
}

// TestOverlayPaneSitsFlushOnBottomChrome pins where the frame's single blank gap row falls relative
// to the transcript-side overlay slot: ABOVE it. The /sessions browser, the /model | /server picker
// and the approval prompt used to be stacked between the transcript and that gap row, so each of
// them painted one empty row between its bottom border and the ▔ hairline — while the autocomplete
// dropdown and the staged-interjection band, which sit in the OTHER slot, hugged the input box with
// no such spacer. That asymmetry is the reported inconsistency (ISSUES.md); the panes now seat flush
// on the chrome and the gap separates the session area from the pane instead.
//
// The frame's total height is asserted with it: this is a stacking reorder and nothing else, so the
// frame still fills the window exactly (D2) and the row arithmetic behind it is untouched.
func TestOverlayPaneSitsFlushOnBottomChrome(t *testing.T) {
	servers := []ServerChoice{
		{Name: "host-a", Endpoint: "http://192.168.64.1:1111"},
		{Name: "host-b", Endpoint: "http://192.168.64.2:1111"},
	}

	panes := []struct {
		name string
		open func(t *testing.T) Model
	}{
		{"session browser", func(t *testing.T) Model {
			m := modelWithOverlayRoomAt(t, 80, 24, Options{Workspace: "/ws/a"})
			m.sessionBrowser = browserWithSessions(4)
			m.layout()
			return m
		}},
		{"server picker", func(t *testing.T) Model {
			m := modelWithOverlayRoomAt(t, 80, 24, Options{Workspace: "/ws/a", Servers: servers})
			m.picker = picker{open: true, kind: pickerServer}
			m.layout()
			return m
		}},
		{"approval prompt", func(t *testing.T) Model {
			m := modelWithOverlayRoomAt(t, 80, 24, Options{Workspace: "/ws/a"})
			m.state = stateAwaitingApproval
			m.pending = &approvalReqMsg{Request: domain.ApprovalRequest{
				Tool:      "write_file",
				Reason:    "the file has to exist before the build can run",
				Arguments: []byte(`{"path":"/ws/a/main.go","content":"package main"}`),
			}}
			m.layout()
			return m
		}},
	}

	for _, pane := range panes {
		t.Run(pane.name, func(t *testing.T) {
			m := pane.open(t)
			rows, ruleRow := frameRule(t, m)

			// (a) Flush: the pane's bottom border is the row directly above the hairline.
			if !strings.Contains(rows[ruleRow-1], "╰") {
				t.Errorf("row above the ▔ hairline = %q, want the pane's ╰ bottom border — no spacer against the chrome\n%s",
					rows[ruleRow-1], strings.Join(rows, "\n"))
			}
			// (b) The gap row is the one directly above the pane's ╭ title row, and it is the ONLY
			// blank row between the transcript block and the chrome.
			paneTop := -1
			for i := range ruleRow {
				if strings.Contains(rows[i], "╭") {
					paneTop = i
					break
				}
			}
			if paneTop < 1 {
				t.Fatalf("pane top border at row %d, want it below at least the gap row\n%s", paneTop, strings.Join(rows, "\n"))
			}
			if !blankRow(rows[paneTop-1]) {
				t.Errorf("row above the pane's ╭ = %q, want the frame's blank gap row", rows[paneTop-1])
			}
			for i := paneTop; i < ruleRow; i++ {
				if blankRow(rows[i]) {
					t.Errorf("blank row %d between the pane and the ▔ hairline; the frame's one gap row belongs above the pane\n%s",
						i, strings.Join(rows, "\n"))
				}
			}
			// D2: the reorder must not change the frame's total rows.
			if got := lipgloss.Height(m.View().Content); got != m.height {
				t.Errorf("composed frame = %d rows, want %d (the whole window, no more and no less)", got, m.height)
			}
		})
	}
}

// TestFrameGapRowWithoutOverlay is the reorder's other half: with nothing open in the
// transcript-side slot the frame is exactly what it always was — one blank row directly above the
// ▔ hairline — so moving the gap above the slot changed the idle frame not at all.
func TestFrameGapRowWithoutOverlay(t *testing.T) {
	m := modelWithOverlayRoomAt(t, 80, 24, Options{Workspace: "/ws/a"})

	rows, ruleRow := frameRule(t, m)
	if ruleRow < 1 {
		t.Fatalf("▔ hairline at row %d, want the gap row above it", ruleRow)
	}
	if !blankRow(rows[ruleRow-1]) {
		t.Errorf("row above the ▔ hairline = %q, want the frame's blank gap row", rows[ruleRow-1])
	}
	if got := lipgloss.Height(m.View().Content); got != m.height {
		t.Errorf("composed frame = %d rows, want %d (the whole window, no more and no less)", got, m.height)
	}
}

// TestBottomRuleHairlineRow is the top rule's mirror. Losing the footer's ╰──╯ left the workdir
// line flush against the terminal's last row with nothing under it, so a ▁ hairline — the ▔
// INVERTED, upper one-eighth block against lower — closes the screen below it. The two are one
// role and are asserted as one: the same recessive style, the same full width, and the frame's
// LAST row is the bottom one, with the footer directly above it and no border glyph between them.
func TestBottomRuleHairlineRow(t *testing.T) {
	m := newTestModel(t)

	if got, want := ansi.Strip(m.bottomRule()), strings.Repeat("▁", m.width); got != want {
		t.Errorf("bottom rule = %q, want %d ▁ runes spanning the window", got, m.width)
	}
	// One role, two positions: a hairline that recedes differently at one end would read as chrome
	// of two different weights rather than as one bracket around the bottom section.
	if top, bot := m.th.hairline.Render("x"), m.th.hairline.Render("x"); top != bot {
		t.Errorf("the two hairlines do not share a style: %q vs %q", top, bot)
	}

	lines := strings.Split(plain(m.View()), "\n")
	last := len(lines) - 1
	if got := lines[last]; strings.TrimRight(got, " ") != strings.Repeat("▁", m.width) {
		t.Errorf("frame's last row = %q, want the full-width ▁ hairline", got)
	}
	// The footer is the row directly above it — the hairline closes the chrome, it does not re-box
	// the footer, so nothing bordered may creep back in between the two.
	footer := lines[last-1]
	if strings.ContainsAny(footer, "╰╯│├┤") {
		t.Errorf("footer row = %q, want it frameless under the hairline", footer)
	}
	if !strings.Contains(footer, modeLabel(m.opts.Mode)) {
		t.Errorf("row above the bottom hairline = %q, want the footer's content line", footer)
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

// leadingColumns counts the blank columns a rendered line opens with, styling stripped — the
// column its first painted glyph sits in.
func leadingColumns(t *testing.T, line string) int {
	t.Helper()
	plainLine := ansiPattern.ReplaceAllString(line, "")
	return len(plainLine) - len(strings.TrimLeft(plainLine, " "))
}

// TestStatusLineAlignsWithTranscriptText proves the status line hangs in the transcript's own
// text column instead of flush left: the spinner opens in the column a wrapped assistant line's
// text opens in, so the whole left slot (spinner, phrase, clock) lines up with the body above
// it (layout.md). Measured against a really-rendered block, not against the constant, so a
// change to the marker or the hanging indent fails here rather than drifting silently.
func TestStatusLineAlignsWithTranscriptText(t *testing.T) {
	const wrapWidth = 8 // narrow enough that "alpha beta" wraps onto a continuation line
	body := renderEntryLines(newTheme(), entry{kind: entryAssistant, text: "alpha beta"}, wrapWidth, false).lines
	if len(body) < 2 {
		t.Fatalf("assistant block did not wrap at width %d: %q", wrapWidth, body)
	}
	want := leadingColumns(t, body[1]) // the hanging indent — the transcript's text column

	m := newTestModel(t)
	m.input.SetValue("hello")
	m = step(t, m, keyEnter()) // running: the left slot carries the spinner and the phrase

	if got := leadingColumns(t, m.statusLine()); got != want {
		t.Errorf("status line starts in column %d; want %d (the transcript's text column)", got, want)
	}
}

// The indent is part of the status line's width budget, not an overhang: a window too narrow
// for the line clips it to the window rather than wrapping onto a second row.
func TestStatusLineIndentFitsNarrowWindow(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("hello")
	m = step(t, m, keyEnter())

	for _, width := range []int{0, 1, 2, 3, 10, 40} {
		m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 24})

		if got := ansi.StringWidth(m.statusLine()); got > width {
			t.Errorf("status line at width %d renders %d columns; want at most %d", width, got, width)
		}
	}
}

// statusCells renders the model's status line with styling stripped and NOTHING trimmed — the
// row's cells exactly as they land, so an assertion can read the columns at its right edge.
func statusCells(t *testing.T, m Model) string {
	t.Helper()
	return ansiPattern.ReplaceAllString(m.statusLine(), "")
}

// TestStatusLineGaugeEndsShortOfEdge proves the context gauge stops bodyIndent columns short of
// the window edge: the pre-styled bar is followed by the status bar's own black margin (asserted
// as the rendered suffix, not as SGR bytes), so the track's last cell sits above the footer mode
// marker's last character instead of against the terminal edge — and the margin comes out of the
// row's width budget, which is still exactly one window.
func TestStatusLineGaugeEndsShortOfEdge(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("hello")
	m = step(t, m, keyEnter()) // running, so the gauge displaces a hint that would otherwise show
	m = step(t, m, eventMsg{Event: domain.UsageEvent{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200}})
	if m.contextGauge() == "" {
		t.Fatal("context gauge unlit after usage: nothing in the right slot to inset")
	}

	line := m.statusLine()
	if margin := m.th.statusBar.Render(bodyIndent); !strings.HasSuffix(line, margin) {
		t.Errorf("status line does not end with the styled margin %q: %q", margin, line)
	}
	if got := ansi.StringWidth(line); got != m.width {
		t.Errorf("status line renders %d columns, want exactly %d (the margin rides inside the width)", got, m.width)
	}
}

// assertStatusRightTail pins the right slot's last columns, styling stripped and untrimmed, and
// re-checks that the margin is paid for out of the window rather than overhanging it.
func assertStatusRightTail(t *testing.T, m Model, want string) {
	t.Helper()
	if got := statusCells(t, m); !strings.HasSuffix(got, want) {
		t.Errorf("status line %q does not end with %q", got, want)
	}
	if got := ansi.StringWidth(m.statusLine()); got != m.width {
		t.Errorf("status line renders %d columns, want exactly %d", got, m.width)
	}
}

// TestStatusLineRightSlotOccupantsShareTheMargin proves the WHOLE right slot moved, not the gauge
// alone: every occupant that time-shares the slot — key hint, copy flash, primed Ctrl+C — ends in
// the same column, bodyIndent short of the edge, which is what "aligned with everything else
// printed there" asks for. An empty slot keeps no phantom margin: the justify gap already paints
// the black band to the last column.
func TestStatusLineRightSlotOccupantsShareTheMargin(t *testing.T) {
	t.Run("running hint", func(t *testing.T) {
		m := newTestModel(t)
		m.input.SetValue("hello")
		m = step(t, m, keyEnter()) // no usage yet, so the hint holds the slot
		assertStatusRightTail(t, m, "esc stop"+bodyIndent)
	})

	t.Run("errored hint", func(t *testing.T) {
		m := newTestModel(t)
		m.state = stateErrored
		m.lastErr = errors.New("boom")
		assertStatusRightTail(t, m, "enter dismiss"+bodyIndent)
	})

	t.Run("copy flash", func(t *testing.T) {
		m := newTestModel(t)
		m.flash = "copied 5 chars"
		assertStatusRightTail(t, m, m.flash+bodyIndent)
	})

	t.Run("empty slot", func(t *testing.T) {
		m := newTestModel(t) // idle, no usage: the slot has no occupant to inset
		if got := statusCells(t, m); strings.TrimSpace(got) != "" {
			t.Errorf("idle status line = %q, want nothing but the black band's fill", got)
		}
		if got := ansi.StringWidth(m.statusLine()); got != m.width {
			t.Errorf("idle status line renders %d columns, want exactly %d", got, m.width)
		}
	})
}

// transcriptRows returns the composed View's transcript rows with styling stripped — the
// viewport's own rows, scroll-bar column included, before the gap row and the bottom chrome.
func transcriptRows(t *testing.T, m Model) []string {
	t.Helper()
	rows := strings.Split(plain(m.View()), "\n")
	if len(rows) < m.viewport.Height() {
		t.Fatalf("view has %d rows, fewer than the viewport's %d", len(rows), m.viewport.Height())
	}
	return rows[:m.viewport.Height()]
}

// TestTranscriptBodyLeavesRightGutter proves the chat body wraps short of its right edge: no
// rendered body line comes within bodyRightGutter columns of the scroll-bar column beside it, so
// there is one free column next to a painted bar and two to the window edge while the gutter
// is blank. Measured on the really-composed View (not on the renderer alone) so the wrap width
// and the reserved scroll-bar column are pinned together, mirroring bodyIndent on the left.
func TestTranscriptBodyLeavesRightGutter(t *testing.T) {
	const width = 80
	m := newTestModel(t)
	// Single-character words pack every wrapped line flush to the wrap limit, so the widest row
	// measured below is exactly the wrap width rather than wherever a word break happened to fall.
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: strings.Repeat("x ", 400)}})

	widest := 0
	for _, row := range transcriptRows(t, m) {
		// Cut the reserved scroll-bar column off the right before measuring: the bar is chrome,
		// not body text, and it paints in that column only while there is something to scroll.
		body := strings.TrimRight(ansi.Truncate(row, width-scrollbarWidth, ""), " ")
		widest = max(widest, ansi.StringWidth(body))
	}

	if widest == 0 {
		t.Fatal("no body text rendered")
	}
	if want := width - scrollbarWidth - bodyRightGutter; widest > want {
		t.Errorf("body reaches %d columns at window width %d; want at most %d (a %d-column right gutter)",
			widest, width, want, bodyRightGutter)
	}
}

// TestHiddenScrollbarYieldsTheColumn is the inverse of the two pinning tests above: with
// `ui.show-scrollbar` off (Options.HideScrollbar, the inverted form the composition root passes)
// the bar's column goes back to the body rather than sitting reserved and blank. The transcript
// overflows its viewport, so the shown state would certainly paint a track and a thumb here — and
// hiding the bar hides the BAR, not the scrolling, which the wheel at the end proves.
func TestHiddenScrollbarYieldsTheColumn(t *testing.T) {
	const width = 80
	opts := testOpts
	opts.HideScrollbar = true
	m := newTestModelEng(t, &fakeEngine{}, opts)
	// Single-character words wrap flush to the limit, and this many of them overflow the viewport
	// several times over.
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: strings.Repeat("x ", 1200)}})

	if m.viewport.Width() != width {
		t.Errorf("viewport width = %d, want the full window's %d — the hidden bar still holds a column",
			m.viewport.Width(), width)
	}
	if want := width - bodyRightGutter; m.transcriptWidth() != want {
		t.Errorf("transcript wraps to %d columns, want %d (the window less its right gutter)",
			m.transcriptWidth(), want)
	}
	if total, h := m.viewport.TotalLineCount(), m.viewport.Height(); total <= h {
		t.Fatalf("transcript is %d lines in a %d-row viewport — the fixture does not overflow, so a "+
			"shown bar would be blank and this proves nothing", total, h)
	}

	for i, row := range transcriptRows(t, m) {
		if strings.ContainsAny(row, glyphScrollTrack+glyphScrollThumb) {
			t.Errorf("transcript row %d carries a scroll-bar glyph with the bar hidden: %q", i, row)
		}
	}

	before := m.viewport.YOffset()
	m = step(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if m.viewport.YOffset() >= before {
		t.Errorf("the wheel did not scroll with the bar hidden: offset %d → %d", before, m.viewport.YOffset())
	}
}

// The shown bar is one axis in two weights: thumb and track paint the same column and differ only
// in stroke. They are a single codepoint apart (U+2503 / U+2502), so a thumb that slipped to the
// track's glyph would erase the position marker without moving one column — every geometry test
// here would stay green while the bar stopped saying where the view sits. This pins the contrast
// itself, on a really-composed frame: the bar's column carries exactly the two glyphs, both of them.
func TestScrollbarThumbReadsAgainstItsTrack(t *testing.T) {
	m := newTestModel(t)
	// Single-character words wrap flush to the limit, and this many of them overflow the viewport
	// several times over — so the bar is painted rather than blank.
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: strings.Repeat("x ", 1200)}})
	if total, h := m.viewport.TotalLineCount(), m.viewport.Height(); total <= h {
		t.Fatalf("transcript is %d lines in a %d-row viewport — the fixture does not overflow, so the "+
			"bar is blank and this proves nothing", total, h)
	}

	cells := map[string]int{}
	for i, row := range transcriptRows(t, m) {
		runes := []rune(row)
		if len(runes) == 0 {
			t.Fatalf("transcript row %d is empty — it carries no scroll-bar cell", i)
		}
		cells[string(runes[len(runes)-1])]++ // the bar hangs off the row's right edge (joinScrollbar)
	}

	if cells[glyphScrollThumb] == 0 {
		t.Errorf("no thumb %q in the bar's column: %v", glyphScrollThumb, cells)
	}
	if cells[glyphScrollTrack] == 0 {
		t.Errorf("no track %q in the bar's column: %v", glyphScrollTrack, cells)
	}
	if len(cells) != 2 {
		t.Errorf("the bar's column carries %d distinct glyphs, want the thumb and the track only: %v",
			len(cells), cells)
	}
}

// The gutter is a floor, not a bare subtraction: at window widths too small to hold it the body
// still wraps to at least one column, and the transcript rows stay inside the viewport plus its
// reserved scroll-bar column (both floored at one, so a 0/1-column window still draws two).
func TestTranscriptBodyWidthAtTinyWindows(t *testing.T) {
	m := newTestModel(t)
	m = step(t, m, eventMsg{Event: domain.MessageEvent{Text: "a message long enough to wrap hard at a tiny width"}})

	for _, width := range []int{0, 1, 2, 3, 5} {
		m = step(t, m, tea.WindowSizeMsg{Width: width, Height: 24})

		body := m.transcriptWidth()
		if body < 1 {
			t.Errorf("transcript width = %d at window width %d; want at least 1", body, width)
		}
		if body > m.viewport.Width() {
			t.Errorf("transcript width = %d at window width %d; want at most the viewport's %d",
				body, width, m.viewport.Width())
		}
		limit := m.viewport.Width() + scrollbarWidth
		for i, row := range transcriptRows(t, m) {
			// Trailing blanks are not transcript content: JoinVertical pads every row of the
			// composed view out to the widest block, which on a sub-minimal window is the input
			// box's own border floor rather than anything the transcript drew.
			if got := ansi.StringWidth(strings.TrimRight(row, " ")); got > limit {
				t.Errorf("transcript row %d at window width %d renders %d columns; want at most %d",
					i, width, got, limit)
			}
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
		// The viewport gives up one column to the scroll-bar gutter (floored at 1 on a tiny window).
		if want := max(1, size.w-scrollbarWidth); m.viewport.Width() != want {
			t.Errorf("viewport width = %d at window width %d, want %d", m.viewport.Width(), size.w, want)
		}
	}
}

// Before the first WindowSizeMsg the view is a placeholder, not a panic.
func TestModelViewBeforeReady(t *testing.T) {
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
	if m.ready {
		t.Fatal("model ready before any WindowSizeMsg")
	}
	if got := plain(m.View()); !strings.Contains(got, "apogee") {
		t.Errorf("pre-ready placeholder unexpected:\n%s", got)
	}
}

// Every frame apogee emits lives on the alternate screen — the pre-ready placeholder as much as a
// laid-out one. A frame that left AltScreen unset would paint on the primary screen, push its lines
// into the terminal's scrollback and keep the terminal's own scrollbar visible for the whole run.
func TestViewStaysOnAltScreen(t *testing.T) {
	m := newModel(context.Background(), &fakeEngine{}, testOpts, nil)
	if m.ready {
		t.Fatal("model ready before any WindowSizeMsg")
	}
	if !m.View().AltScreen {
		t.Error("pre-ready placeholder frame is not on the alternate screen")
	}
	m = step(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.ready {
		t.Fatal("model not ready after a WindowSizeMsg")
	}
	if !m.View().AltScreen {
		t.Error("laid-out frame is not on the alternate screen")
	}
}

// The scroll bar macOS Terminal.app lights when a program takes the alternate screen goes out only
// if the saved lines are erased AFTER the switch — sent first, the erase clears a scrollback the
// switch immediately refills, and the bar stays lit for the whole run. Both halves of that order
// are pinned here, exactly, because nothing downstream can observe it: the sequences leave for a
// real terminal and no test can watch what Terminal.app does with them.
func TestClaimAltScreenErasesTheScrollbackAfterTheSwitch(t *testing.T) {
	var buf bytes.Buffer
	if err := claimAltScreen(&buf); err != nil {
		t.Fatalf("claimAltScreen: %v", err)
	}
	if got, want := buf.String(), ansiEnterAltScreen+ansiEraseScrollback; got != want {
		t.Errorf("claimAltScreen wrote %q, want the switch then the erase: %q", got, want)
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
// Transcript scroll-follow, sticky header and auto-grow input (P2.7 — TUI presentation pass)
// ----------------------------------------------------------------------------

// firstVisibleLine returns the viewport's top visible line, styling stripped.
func firstVisibleLine(vp viewport.Model) string {
	return strings.SplitN(ansiPattern.ReplaceAllString(vp.View(), ""), "\n", 2)[0]
}

// A reply longer than the screen keeps its tail in view as it streams — the transcript follows
// generated output — with the prompt it belongs to overlaid at the top row as the sticky header.
// This is the reported bug: the reply used to stream out of sight below a prompt pinned to the top.
func TestFollowsTailOfLongStreamedReply(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.input.SetValue("FOLLOW-PROMPT")
	m = step(t, m, keyEnter()) // the real submit path: records the prompt and re-arms follow

	// A reply far taller than the window, streamed token by token through Update.
	for i := 0; i < 40; i++ {
		m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: strings.Repeat("x", 60) + " "}})
	}
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "THE-TAIL"}})

	if !m.viewport.AtBottom() {
		t.Errorf("viewport left at offset %d, not at the bottom; the stream is running out of view",
			m.viewport.YOffset())
	}
	if got := plain(m.View()); !strings.Contains(got, "THE-TAIL") {
		t.Errorf("the streamed tail is off screen:\n%s", got)
	}
	if top := firstViewLine(m); !strings.Contains(top, "FOLLOW-PROMPT") {
		t.Errorf("top line = %q; want the owning prompt overlaid as the sticky header", top)
	}
}

// A human who scrolled away is not yanked back: while detached, a repaint that appends content
// leaves the scroll offset exactly where they put it.
func TestDetachedRepaintHoldsPosition(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("first question", nil)
	m.transcript.commitAssistant(strings.Repeat("filler above. ", 80), 0)
	m.transcript.addUser("STICKY-PROMPT", nil)
	for i := 0; i < 30; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()

	m.detached = true
	m.viewport.SetYOffset(5) // up in the history, well off the bottom
	off := m.viewport.YOffset()

	m.transcript.commitAssistant("more streamed content", 0)
	m.refreshViewport()

	if m.viewport.YOffset() != off {
		t.Errorf("a detached viewport was moved by a repaint: offset %d → %d", off, m.viewport.YOffset())
	}
	if !m.detached {
		t.Error("appending below a held offset re-attached; only a view back at the bottom may")
	}
}

// Content shrinking under a held offset is clamped back to the bottom by SetContentLines, so
// following resumes — the invariant "detached ⇔ off the bottom" stays total.
func TestShrinkingContentReattachesFollow(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("a question", nil)
	for i := 0; i < 30; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()
	m.detached = true
	m.viewport.SetYOffset(20)
	if m.viewport.AtBottom() {
		t.Fatal("precondition: the held offset is already at the bottom")
	}

	m.transcript.reset() // the transcript shrinks away under the offset, as /clear does
	m.transcript.addUser("a fresh question", nil)
	m.refreshViewport()

	if m.detached {
		t.Error("the clamp left the view at the bottom; follow must resume there")
	}
	if !m.viewport.AtBottom() {
		t.Errorf("viewport at offset %d is not at the bottom after the clamp", m.viewport.YOffset())
	}
}

// Submitting re-arms follow: sending a prompt means the human is done reading history.
func TestSubmitReattachesFollow(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("old question", nil)
	for i := 0; i < 30; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()
	m.detached = true
	m.viewport.SetYOffset(3)

	m.input.SetValue("a new question")
	m = step(t, m, keyEnter())

	if m.detached {
		t.Error("submit did not re-arm follow; the reply would stream out of view")
	}
	if !m.viewport.AtBottom() {
		t.Errorf("after submit the viewport sits at %d, not at the bottom (its new prompt)",
			m.viewport.YOffset())
	}
}

// The mouse wheel scrolls the transcript in every state, including idle. The keyboard scroll
// path is state-gated (idle feeds the input box), so without the MouseWheelMsg route in Update a
// finished reply could not be scrolled back — the "scrolling only works intermittently" bug. A
// wheel-up that leaves the bottom detaches: new content must not yank the history back.
func TestMouseWheelScrollsWhileIdle(t *testing.T) {
	m := newTestModel(t) // 80x24, stateIdle
	m.transcript.addUser("question", nil)
	for i := 0; i < 40; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport() // follows the tail: the view opens at the bottom, attached

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
	if m.viewport.AtBottom() {
		t.Fatalf("precondition: the wheel-up left the view at the bottom (offset %d)", m.viewport.YOffset())
	}
	if !m.detached {
		t.Error("a wheel scroll off the bottom did not detach the transcript; new content would yank history back")
	}
}

// Detach is positional, not a latch: wheeling back down to the very bottom resumes following, and
// the token streamed next lands in view.
func TestWheelBackToBottomReattachesFollow(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.input.SetValue("a question")
	m = step(t, m, keyEnter())
	for i := 0; i < 40; i++ {
		m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: strings.Repeat("x", 60) + " "}})
	}

	m = step(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	if !m.detached {
		t.Fatalf("precondition: a wheel-up did not detach (offset %d)", m.viewport.YOffset())
	}

	for i := 0; i < 10 && !m.viewport.AtBottom(); i++ {
		m = step(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelDown})
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("precondition: wheeling down did not reach the bottom (offset %d)", m.viewport.YOffset())
	}
	if m.detached {
		t.Fatal("scrolling back to the bottom did not re-attach follow")
	}

	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "THE-TAIL"}})

	if !m.viewport.AtBottom() {
		t.Errorf("after re-attaching, a streamed token left the view at offset %d, not the bottom",
			m.viewport.YOffset())
	}
	if got := plain(m.View()); !strings.Contains(got, "THE-TAIL") {
		t.Errorf("the token streamed after re-attaching is off screen:\n%s", got)
	}
}

// The keyboard funnel carries the same policy: PgDn back to the bottom re-attaches, PgUp off it
// detaches. PgUp/PgDn are intercepted in every state, idle included.
func TestPageDownToBottomReattachesFollow(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("a question", nil)
	for i := 0; i < 40; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()

	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if !m.detached {
		t.Fatalf("precondition: PgUp did not detach (offset %d)", m.viewport.YOffset())
	}

	for i := 0; i < 5 && !m.viewport.AtBottom(); i++ {
		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	if !m.viewport.AtBottom() {
		t.Fatalf("precondition: PgDn did not reach the bottom (offset %d)", m.viewport.YOffset())
	}
	if m.detached {
		t.Error("PgDn back to the bottom did not re-attach follow")
	}
}

// A scroll that lands mid-history holds exactly there: content appended below does not move the
// view, and does not re-attach it either.
func TestScrollMidHistoryHoldsPositionOnAppend(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("a question", nil)
	for i := 0; i < 60; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()

	for i := 0; i < 5; i++ {
		m = step(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})
	}
	if !m.detached || m.viewport.AtBottom() {
		t.Fatalf("precondition: not parked mid-history (detached=%v, offset %d)",
			m.detached, m.viewport.YOffset())
	}
	off := m.viewport.YOffset()

	m.transcript.commitAssistant("more streamed content", 0)
	m.refreshViewport()

	if m.viewport.YOffset() != off {
		t.Errorf("an append moved the held view: offset %d → %d", off, m.viewport.YOffset())
	}
	if !m.detached {
		t.Error("an append below the held offset re-attached follow")
	}
}

// A transcript shorter than the window has no bottom to scroll off, so a wheel event over it can
// never detach — the old offset-delta latch could be tripped by any stray offset jiggle.
func TestWheelOnShortTranscriptDoesNotDetach(t *testing.T) {
	m := newTestModel(t) // 80x24; the start-up box alone, far shorter than the window
	m.refreshViewport()
	if !m.viewport.AtBottom() {
		t.Fatalf("precondition: a transcript shorter than the window sits at offset %d, not the bottom",
			m.viewport.YOffset())
	}

	m = step(t, m, tea.MouseWheelMsg{Button: tea.MouseWheelUp})

	if m.detached {
		t.Errorf("a wheel event on a transcript that fits the window detached follow (offset %d)",
			m.viewport.YOffset())
	}
}

// firstViewLine returns the top line of the full View (styling stripped). The sticky-header
// overlay writes to View, not the viewport, so firstVisibleLine (viewport-only) cannot see it.
func firstViewLine(m Model) string {
	return strings.SplitN(plain(m.View()), "\n", 2)[0]
}

// A short exchange stays whole at the tail: no prompt is hoisted to the top of an emptied
// screen, so the earlier turn is still on screen and no blank padding was appended below the
// reply. The prompt reaches the top row only naturally, once a reply has grown a screenful.
func TestShortReplyKeepsTheExchangeAtTheTail(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("FIRST-QUESTION", nil)
	m.transcript.commitAssistant("a prior short answer", 0)
	m.transcript.addUser("LATEST-PROMPT", nil)
	m.transcript.commitAssistant("a short reply", 0)
	m.refreshViewport()

	view := plain(m.View())
	if !strings.Contains(view, "FIRST-QUESTION") {
		t.Errorf("the earlier turn was scrolled out of sight by a short reply:\n%s", view)
	}
	if !strings.Contains(view, "LATEST-PROMPT") || !strings.Contains(view, "a short reply") {
		t.Errorf("the latest exchange is not on screen:\n%s", view)
	}
	if n := m.viewport.TotalLineCount(); n != len(m.lines) {
		t.Errorf("viewport holds %d rows for %d rendered lines; the content was padded", n, len(m.lines))
	}
}

// The reported jump: submitting a prompt into a history taller than the window must append it
// at the true tail — no blank padding below it, the prompt on the last content rows — with the
// history still one page-up away, rather than opening alone at the top of an emptied screen.
func TestSubmitAppendsAtTheTailWithoutJumping(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("OLDEST-PROMPT", nil)
	for i := 0; i < 40; i++ {
		m.transcript.commitAssistant("history paragraph "+strings.Repeat("x", 10), 0)
	}
	m.refreshViewport()

	m.input.SetValue("NEW-PROMPT")
	m = step(t, m, keyEnter())

	if !m.viewport.AtBottom() {
		t.Errorf("after submit the viewport sits at %d, not at the bottom", m.viewport.YOffset())
	}
	if n := m.viewport.TotalLineCount(); n != len(m.lines) {
		t.Errorf("viewport holds %d rows for %d rendered lines; the submit pad is back", n, len(m.lines))
	}
	if last := strings.TrimRight(m.lines[len(m.lines)-1], " "); last == "" {
		t.Error("the content ends on a blank row; the tail must be the transcript's own last line")
	}
	if !strings.Contains(plain(m.View()), "NEW-PROMPT") {
		t.Error("the submitted prompt is not on screen")
	}
	// The history is not gone, only above: one page up brings the previous turn back.
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if got := plain(m.View()); !strings.Contains(got, "history paragraph") {
		t.Errorf("one page up did not reveal the prior history:\n%s", got)
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
	one := m.userBlocks[0]                   // section one's user-block range (below the seeded start-up box)
	two := m.userBlocks[len(m.userBlocks)-1] // section two's user-block range in the stashed lines

	// Scrolled a few rows past section one's prompt, into its reply: its prompt is above the top,
	// so it is drawn as the sticky header. The offset is relative to the block (not an absolute
	// row) because the one-time start-up box seeded at entries[0] sits above section one.
	m.detached = true
	m.viewport.SetYOffset(one.start + 3)
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

// The sticky header shows the block's RENDERED state and special-cases nothing (layout.md,
// "Collapsed and expanded blocks"): a huge prompt that collapsed to its three-row shape sticks as
// those three rows, hidden body and all, and one deliberately expanded sticks expanded — the
// overlay simply paints the block's own lines, however many the painter made.
func TestStickyHeaderShowsTheCollapsedPromptShape(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("alpha\nbravo\ncharlie\ndelta\necho\nfoxtrot", nil)
	for i := 0; i < 30; i++ { // reply enough to scroll the prompt off the top
		m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), 0)
	}
	m.refreshViewport()

	// Scrolled into the reply, so the prompt is above the top row and is drawn as the header.
	block := m.userBlocks[len(m.userBlocks)-1]
	m.detached = true
	m.viewport.SetYOffset(block.start + block.count + 3)

	start, count := m.stickyHeaderSpan()
	if count != promptCollapsedRows {
		t.Fatalf("the sticky header spans %d rows; want the collapsed prompt's %d", count, promptCollapsedRows)
	}
	head := strip(strings.Join(m.lines[start:start+count], "\n"))
	if !strings.Contains(head, "❯ alpha") || !strings.Contains(head, "see more") {
		t.Errorf("the stuck header is not the collapsed shape:\n%s", head)
	}
	if strings.Contains(head, "foxtrot") {
		t.Errorf("the stuck header shows body rows the collapsed block hides:\n%s", head)
	}
	if top := firstViewLine(m); !strings.Contains(top, "❯ alpha") {
		t.Errorf("top line = %q; want the collapsed prompt stuck to the top", top)
	}

	// Expanded, the same block sticks as everything it now paints — six body rows and the see-less
	// row that closes it.
	if !m.transcript.setExpanded(0, true) {
		t.Fatal("setExpanded(0, true) = false; want the prompt expanded")
	}
	m.refreshViewport()
	if _, count := m.stickyHeaderSpan(); count != 7 {
		t.Errorf("the expanded prompt sticks as %d rows; want its six body rows plus the see-less row", count)
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

// TestInputBoxCountsTheDraftRowsItCannotShow is the box's half of "hiding is never silent"
// (FOLLOW-UP-K). The box is now bounded by the FRAME as well as by its own ten-row taste, so on a
// short terminal it draws a WINDOW onto the draft — and a window the human cannot see the edges of
// is exactly the silence every pane above the box was fixed for. The count therefore rides the one
// row the box always owns and that carries nothing else, its top border, in the SAME marker and the
// same narrow ladder a pane's title row uses: full phrase where the width pays for it, the short
// "… +N" where it does not.
//
// The two caps are both here because they are one rule: the box shows what the frame lets it and
// says how much it is not showing, whether the bound came from the window or from maxInputRows.
func TestInputBoxCountsTheDraftRowsItCannotShow(t *testing.T) {
	cases := []struct {
		name       string
		width      int
		height     int
		draft      int
		wantHidden int
		wantMarker string
	}{
		// Room to spare: the box draws the whole draft and its border says nothing.
		{"draft fits", 80, 30, 4, 0, ""},
		// The box's own taste is the bound — a 30-row window can pay for far more than ten rows.
		{"box at its ten-row taste cap", 80, 30, 15, 5, "… (+5 more lines)"},
		// The FRAME is the bound: at twelve rows the box may keep five, and this is the case that
		// composed a 14-row frame on a 12-row terminal before the cap existed.
		{"box at the frame's cap", 80, 12, 8, 3, "… (+3 more lines)"},
		// The frame's floor, where the box is one row and the draft is eleven rows of nowhere.
		{"box at its one-row floor", 80, frameFloorRows, 12, 11, "… (+11 more lines)"},
		// A short window is usually a narrow one: the phrase sheds its noun rather than the row
		// shedding the count off its end.
		{"too narrow for the phrase", 20, 12, 8, 3, "… +3"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := withDraft(t, modelWithOverlayRoomAt(t, c.width, c.height, testOpts), c.draft)

			if got := m.hiddenDraftRows(); got != c.wantHidden {
				t.Fatalf("hiddenDraftRows = %d, want %d (box %d rows)", got, c.wantHidden, m.input.Height())
			}
			// The marker goes on the box's TOP BORDER — the box's title row in every way that counts —
			// so it costs the draft no row of its own.
			edge := ansi.Strip(strings.Split(m.inputView(), "\n")[0])
			if c.wantMarker == "" {
				if elisionMarkerPattern.MatchString(edge) {
					t.Errorf("box border = %q, want no marker: the draft is fully drawn", edge)
				}
			} else if !strings.Contains(edge, c.wantMarker) {
				t.Errorf("box border = %q, want it to carry %q", edge, c.wantMarker)
			}
			// The border row is still a border row: exactly the window's width, corner to corner.
			if got := m.th.measure.Width(edge); got != c.width {
				t.Errorf("box border is %d cells wide, want %d", got, c.width)
			}
			if !strings.HasPrefix(edge, "╭") || !strings.HasSuffix(edge, "╮") {
				t.Errorf("box border = %q, want it to keep its rounded corners", edge)
			}
			// The DRAFT itself is untouched — it is what the human typed, not prose apogee derived.
			// Which rows of it the box draws is the caret's business
			// (TestInputBoxWindowFollowsTheCaret); that it still holds all of them is this one's.
			if got := m.input.Value(); got != draftLines(c.draft) {
				t.Errorf("the draft changed to %q; only the rows it is DRAWN on may give way", got)
			}
		})
	}
}

// TestInputBoxWindowFollowsTheCaret is the other half of the box's contract under the frame's cap: a
// capped box is a WINDOW onto the draft, and a window that does not move is a box that hides the
// line being typed on. The frame's cap can put the box at one row on a short terminal, so the rule
// has to hold at the tightest bound there is, not just at the ten-row taste cap.
//
// It types the draft rather than assigning it, which is both how a human makes one and what makes
// the assertion meaningful: the widget re-clamps its own scroll on each edit, and layout's re-seat
// (reseatInput, ISSUES #2) re-clamps it whenever the cap changes the box's height under it.
func TestInputBoxWindowFollowsTheCaret(t *testing.T) {
	for _, height := range []int{frameFloorRows, 10, smallestOverlayWindow, 16, 24} {
		t.Run(fmt.Sprintf("%d rows", height), func(t *testing.T) {
			m := modelWithOverlayRoomAt(t, 80, height, testOpts)
			iw := m.inputInnerWidth()
			m = typeText(t, m, strings.Repeat("a", iw*12)) // ~12 rows: past every cap these windows allow

			rows := inputContentRows(m.input.Value(), iw)
			if rows <= m.input.Height() {
				t.Fatalf("the box drew all %d rows at %d rows of terminal — test premise broken", rows, height)
			}
			// The caret is at the end of the value, so the least scroll that keeps it visible puts the
			// LAST row on the box's bottom line — the widget's own clamp, at whichever height the cap
			// left the box.
			if got, want := m.input.ScrollYOffset(), rows-m.input.Height(); got != want {
				t.Errorf("ScrollYOffset = %d, want %d (%d content rows in a %d-row box)",
					got, want, rows, m.input.Height())
			}
			// …and what scrolled out of sight is exactly what the border row counts.
			if got, want := m.hiddenDraftRows(), rows-m.input.Height(); got != want {
				t.Errorf("hiddenDraftRows = %d, want %d", got, want)
			}
			if frame := strings.Split(m.View().Content, "\n"); len(frame) > height {
				t.Errorf("composed frame is %d rows on a %d-row terminal (+%d)", len(frame), height, len(frame)-height)
			}
		})
	}
}

// TestInputBoxTooNarrowForTheCountKeepsAPlainBorder pins the ONE place the box's marker differs
// from a pane's title row. A pane sheds its NAME to keep its number; this row has no name to shed,
// so under the width even "… +N" needs it stays a plain border rather than drawing a clipped count —
// "… +1" of "… +19" is not a quieter statement of the fact, it is a false one.
func TestInputBoxTooNarrowForTheCountKeepsAPlainBorder(t *testing.T) {
	m := withDraft(t, modelWithOverlayRoomAt(t, 9, 12, testOpts), 8)

	if m.hiddenDraftRows() == 0 {
		t.Fatalf("the box drew the whole draft at 9 columns — test premise broken (height %d)", m.input.Height())
	}
	edge := ansi.Strip(strings.Split(m.inputView(), "\n")[0])
	if strings.ContainsAny(edge, "…+") {
		t.Errorf("box border = %q, want a plain border: no honest count fits this width", edge)
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

// reseatDeadline bounds the one keystroke TestPromptScrollReseatCannotSpin drives. It is absurdly
// generous for a walk of three logical lines — the point is only that a walk which cannot advance
// FAILS the test instead of wedging the whole suite.
const reseatDeadline = 10 * time.Second

// TestPromptScrollReseatCannotSpin pins termination on the geometry that used to wedge the re-seat.
// The middle line fills a visual row exactly and ends with a space, so bubbles' wrap gives it a
// PHANTOM trailing sub-line that CursorDown's column clamp can never enter: a walk of bare
// CursorDowns toward a caret BELOW that line never advances and never stops. An ordinary keystroke
// that grows the box runs that walk (layout re-seats on a height change), so this drives exactly
// that — one printable rune typed on the last line — behind a deadline. A regression fails here
// rather than hanging `go test`; the caret must also come back where it was, since a walk that
// merely terminated early would strand it on the wrong line.
func TestPromptScrollReseatCannotSpin(t *testing.T) {
	m := newTestModel(t) // 80 columns ⇒ a 76-column text area
	iw := m.inputInnerWidth()
	middle := strings.Repeat("aaa ", 19) // 76 chars: fills one row exactly, ends with a space
	if len(middle)%iw != 0 {
		t.Fatalf("middle line is %d chars at width %d; it must fill its last row exactly", len(middle), iw)
	}
	// The last line is one column short of the wrap, so the keystroke grows the box — which is what
	// makes layout run the re-seat with the caret BELOW the phantom-wrapped line.
	value := "head\n" + middle + "\n" + strings.Repeat("b", iw-1)

	// Both steps run off-goroutine, because BOTH grow the box and so both re-seat: the paste seeds
	// the draft (production path — the box grows from one row to the draft's) and the keystroke
	// then grows it by the row it wraps to. A walk that cannot advance fails here instead of
	// wedging the suite.
	type outcome struct {
		m      Model
		before int
	}
	done := make(chan outcome, 1)
	go func() {
		next, _ := m.Update(tea.PasteMsg{Content: value})
		seeded := next.(Model)
		before := seeded.input.Height()
		next, _ = seeded.Update(keyRune('x'))
		done <- outcome{next.(Model), before}
	}()
	var got outcome
	select {
	case got = <-done:
	case <-time.After(reseatDeadline):
		t.Fatalf("a keystroke on a phantom-wrapped draft did not return within %s: the re-seat is spinning", reseatDeadline)
	}
	m = got.m

	if m.input.Height() == got.before {
		t.Fatalf("box height stayed %d; the keystroke never triggered the re-seat", got.before)
	}
	if v, want := m.input.Value(), value+"x"; v != want {
		t.Fatalf("value = %q, want %q", v, want)
	}
	if off, want := m.caretByteOffset(), len(value)+1; off != want {
		t.Errorf("caret at byte %d, want %d — the re-seat moved it off the end", off, want)
	}
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
// strip is display-only; opts.Model (sent to the server) is unaffected. The no-model case — once a
// "never reached in practice" row here, now a real state at every cold start — has its own test,
// TestDisplayModelEmpty (heartbeat_test.go).
func TestDisplayModel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/Users/me/models/qwen2.5-coder-7b-instruct.gguf", "qwen2.5-coder-7b-instruct"},
		{"/opt/models/Llama-3.1-8B.GGUF", "Llama-3.1-8B"},
		{"model.safetensors", "model"},
		{"qwen2.5-coder", "qwen2.5-coder"}, // no weight extension: the version dot survives
		{"test-model", "test-model"},
	}
	for _, tc := range cases {
		if got := displayModel(tc.in); got != tc.want {
			t.Errorf("displayModel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestPopupBudgetShrinksToNothing pins the D2 floor change (item 7) and the body floor that
// followed it. The row budget used to bottom out at six rows — `max(6, viewport.Height()-3)` — so
// on a window with four rows to give it promised a pane the frame could not hold, and the surplus
// came off the bottom: the input box and the footer, off the alternate screen. The BODY budget
// then repeated the mistake one row smaller, bottoming out at one: a pane carrying prose was five
// rows tall where a boxed overlay's chrome is four, so the ask and approval prompts overflowed the
// shortest window a pane fits in at all while the row-only browser fitted. Both floors are zero
// now, so a short window's budget shrinks to nothing and the transcript is what gives way.
func TestPopupBudgetShrinksToNothing(t *testing.T) {
	// The body's forty wrapped lines always exceed the granted budget, so a body-bearing pane is
	// exactly as tall as its budget allows and the arithmetic below is an equality, not a bound.
	bodyLines := make([]string, 40)
	for i := range bodyLines {
		bodyLines[i] = fmt.Sprintf("body line %02d", i)
	}
	longBody := strings.Join(bodyLines, "\n")
	eightRows := singleCellRows([]string{"a", "b", "c", "d", "e", "f", "g", "h"})

	cases := []struct {
		height   int // the terminal's rows
		wantRows int // the row window popupBudget grants an eight-row offering
		wantBody int // the body rows it grants alongside them
	}{
		{8, 0, 0},   // viewport 1 row: nothing to spend at all
		{12, 0, 0},  // viewport 4: the chrome alone is the whole budget — no rows, no prose
		{16, 0, 1},  // viewport 8: short of the chrome + a row, but one body row fits
		{20, 4, 1},  // viewport 12: four rows fit above the input box
		{24, 8, 1},  // viewport 16: the overlay's own cap binds first
		{40, 8, 17}, // roomy: the row cap still binds and the body takes the rest
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%d rows", c.height), func(t *testing.T) {
			m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 80, Height: c.height})
			body, rows, _ := m.popupBudget(paneBrowser, 8, maxSessionRows, popupChrome)
			if rows != c.wantRows {
				t.Errorf("popupBudget row window = %d, want %d (viewport %d rows)", rows, c.wantRows, m.viewport.Height())
			}
			if body != c.wantBody {
				t.Errorf("popupBudget body budget = %d, want %d (viewport %d rows)", body, c.wantBody, m.viewport.Height())
			}

			// A granted window of zero must mean NO rows and NO body, not every row and every line:
			// the pane's whole reason for asking is that the frame has no space for them.
			const chrome = 4 // 2 borders + the title + the hint
			rowPane := renderPopup(m.th, popupSpec{
				title:   "saved sessions",
				rows:    eightRows,
				hint:    "esc close",
				maxRows: rows,
			}, m.width)
			if got, want := lipgloss.Height(rowPane), chrome+c.wantRows; got != want {
				t.Errorf("row-only pane is %d rows tall, want %d (chrome %d + %d granted rows)", got, want, chrome, c.wantRows)
			}
			bodyPane := renderPopup(m.th, popupSpec{
				title:       "approve write_file?",
				body:        longBody,
				maxBodyRows: body,
				rows:        eightRows,
				hint:        "a allow · d deny",
				maxRows:     rows,
			}, m.width)
			if got, want := lipgloss.Height(bodyPane), chrome+c.wantBody+c.wantRows; got != want {
				t.Errorf("body-bearing pane is %d rows tall, want %d (chrome %d + %d body + %d rows)",
					got, want, chrome, c.wantBody, c.wantRows)
			}

			// The finding this closes: on every window a pane can be drawn in at all, a pane carrying
			// prose fits the viewport just as the row-only one does.
			if c.height >= smallestOverlayWindow && lipgloss.Height(bodyPane) > m.viewport.Height() {
				t.Errorf("body-bearing pane is %d rows tall on a %d-row viewport (+%d)",
					lipgloss.Height(bodyPane), m.viewport.Height(), lipgloss.Height(bodyPane)-m.viewport.Height())
			}
		})
	}
}
