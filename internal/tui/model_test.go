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

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/format"
	"github.com/airiclenz/apogee/internal/heartbeat"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/scheme"
	"github.com/airiclenz/apogee/internal/session"
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
func keySpace() tea.KeyPressMsg { return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "} }
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
		ContextWindow: 32768,                   // format.Tokens → "32k"
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
	if got, want := e.startup.Context, format.Tokens(opts.ContextWindow); got != want {
		t.Errorf("startup context = %q, want %q (format.Tokens of Options.ContextWindow)", got, want)
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

	// A token starts the throughput clock; the clock is then back-dated rather than slept
	// against, so the window the usage divides by clears throughputWindowFloor by a second
	// without the test waiting for one (foldStats).
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "hi"}})
	m.genStart = time.Now().Add(-time.Second)
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

	// A sub-agent's usage (Depth > 0) nests in the stream but must not move the top-level gauge:
	// it fills the delegate's own run block instead, which is the block the delegation opened.
	m = step(t, m, eventMsg{Event: domain.ToolCallEvent{
		Call: domain.ToolCall{ID: "s1", Tool: "sub_agent", Arguments: []byte(`{"task":"survey the tests"}`)},
	}})
	head := len(m.transcript.entries) - 1

	prev := m.ctxUsed
	m = step(t, m, eventMsg{Event: domain.UsageEvent{
		EventBase:    domain.EventBase{Depth: 1},
		PromptTokens: 9, CompletionTokens: 9, TotalTokens: 9,
	}})
	if m.ctxUsed != prev {
		t.Errorf("a Depth>0 UsageEvent changed the top-level gauge: %d -> %d", prev, m.ctxUsed)
	}
	if got := m.transcript.entries[head]; got.ctxUsed != 9 || got.ctxLimit != m.opts.ContextWindow {
		t.Errorf("sub-agent run fill = %d/%d, want 9/%d (the child's reading in the window it inherited)",
			got.ctxUsed, got.ctxLimit, m.opts.ContextWindow)
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
	th := newTheme(scheme.Default())

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
		if got := plain(m.View()); !strings.Contains(got, "Allow") || !strings.Contains(got, "Deny") {
			t.Errorf("approval menu rows not shown:\n%s", got)
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

// The empty box advertises ⇧⏎ only on a terminal that negotiated the enhanced keyboard protocol:
// the model starts pessimistic and follows bubbletea's KeyboardEnhancementsMsg, in both directions.
func TestModelNewlineLegendFollowsKeyboardProtocol(t *testing.T) {
	m := newTestModel(t)

	// No answer yet — the start-up default names only the chord every terminal delivers.
	if got := m.input.Placeholder; got != idlePlaceholder {
		t.Fatalf("start-up legend = %q, want the ⌥⏎-only %q", got, idlePlaceholder)
	}
	if got := plain(m.View()); !strings.Contains(got, idlePlaceholder) {
		t.Errorf("the frame does not paint the start-up legend %q", idlePlaceholder)
	}

	// The terminal answers with key disambiguation on: the chord now arrives, so the legend names it.
	on := step(t, m, tea.KeyboardEnhancementsMsg{Flags: ansi.KittyDisambiguateEscapeCodes})
	if got := on.input.Placeholder; got != idleShiftPlaceholder {
		t.Errorf("legend after the enhanced answer = %q, want %q", got, idleShiftPlaceholder)
	}
	if got := plain(on.View()); !strings.Contains(got, idleShiftPlaceholder) {
		t.Errorf("the frame does not paint the negotiated legend %q", idleShiftPlaceholder)
	}

	// And back: an answer carrying no enhancements returns the box to the honest legend.
	off := step(t, on, tea.KeyboardEnhancementsMsg{})
	if got := off.input.Placeholder; got != idlePlaceholder {
		t.Errorf("legend after a bare answer = %q, want %q back", got, idlePlaceholder)
	}
}

// ----------------------------------------------------------------------------
// Cancellation and quit
// ----------------------------------------------------------------------------

func TestModelStopKeys(t *testing.T) {
	t.Run("a single esc while running arms the gesture but cancels nothing", func(t *testing.T) {
		m := newTestModel(t)
		cancelled := false
		m.cancel = func() { cancelled = true }
		m.state = stateRunning
		next, cmd := stepCmd(t, m, keyEsc())
		if cancelled {
			t.Error("a single esc cancelled the in-flight worker; it must only arm the gesture")
		}
		if next.lastEsc.IsZero() {
			t.Error("a single esc did not arm the stop gesture")
		}
		if got := plain(next.View()); !strings.Contains(got, "press esc again to stop") {
			t.Errorf("the arm hint is not shown after one esc:\n%s", got)
		}
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
			t.Error("esc quit the program instead of arming the stop gesture")
		}
	})

	t.Run("esc twice while running cancels but does not quit", func(t *testing.T) {
		m := newTestModel(t)
		cancelled := false
		m.cancel = func() { cancelled = true }
		m.state = stateRunning
		m = step(t, m, keyEsc())
		next, cmd := stepCmd(t, m, keyEsc())
		if !cancelled {
			t.Error("esc×2 did not cancel the in-flight worker")
		}
		if next.state != stateRunning {
			t.Errorf("state = %v, want still running until the worker reports back", next.state)
		}
		// The confirming press disarms before it stops: statusRight's armed branch sits above
		// every occupant, so a stamp left standing would hold the hint on an idle status line.
		if !next.lastEsc.IsZero() {
			t.Error("the confirming esc left the gesture armed; the stamp must be zeroed before the stop")
		}
		if msg := cmdMsg(cmd); msg != nil {
			if _, isQuit := msg.(tea.QuitMsg); isQuit {
				t.Error("esc quit the program instead of cancelling the worker")
			}
		}
	})

	t.Run("a second esc after the window only re-arms", func(t *testing.T) {
		m := newTestModel(t)
		cancelled := false
		m.cancel = func() { cancelled = true }
		m.state = stateRunning
		m = step(t, m, keyEsc())
		m.lastEsc = m.lastEsc.Add(-2 * escStopWindow) // pretend the window lapsed
		// Re-arming refreshes lastEsc to ~now; the stop path zeroes it. A refreshed, non-zero
		// stamp therefore proves the press took the arm branch, not the stop branch.
		next, _ := stepCmd(t, m, keyEsc())
		if cancelled {
			t.Error("esc after the window cancelled the worker instead of only re-arming")
		}
		if next.lastEsc.IsZero() || !next.lastEsc.After(m.lastEsc) {
			t.Error("esc after the window did not re-arm the stop gesture")
		}
	})

	t.Run("a worker that finishes mid-window takes the arm with it", func(t *testing.T) {
		m := newTestModel(t)
		m.cancel = func() {}
		m.state = stateRunning
		armed := step(t, m, keyEsc())
		if got := plain(armed.View()); !strings.Contains(got, "press esc again to stop") {
			t.Fatalf("the arm hint is not shown while the worker is still busy:\n%s", got)
		}
		// The Exchange reaches its own quiescent boundary inside the window: nothing is left to
		// stop, so the hint goes with it rather than sitting out the rest of escStopWindow.
		done := step(t, armed, exchangeDoneMsg{})
		if !done.lastEsc.IsZero() {
			t.Error("the finished worker left the stop gesture armed")
		}
		if got := plain(done.View()); strings.Contains(got, "press esc again to stop") {
			t.Errorf("the arm hint lingers on the idle status line after the worker finished:\n%s", got)
		}
	})

	t.Run("esc while idle does not quit", func(t *testing.T) {
		m := newTestModel(t)
		next, cmd := stepCmd(t, m, keyEsc())
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
			t.Error("esc at idle quit the program; it must never end the app")
		}
		if !next.lastEsc.IsZero() {
			t.Error("esc at idle armed the stop gesture; there is no worker to stop")
		}
	})

	t.Run("esc while errored does not quit", func(t *testing.T) {
		m := newTestModel(t)
		m.state = stateErrored
		next, cmd := stepCmd(t, m, keyEsc())
		if _, isQuit := cmdMsg(cmd).(tea.QuitMsg); isQuit {
			t.Error("esc while errored quit the program; it must never end the app")
		}
		if !next.lastEsc.IsZero() {
			t.Error("esc while errored armed the stop gesture; there is no worker to stop")
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
// returning both so a test can assert on the decision the keys send back. The pane's decision keys
// are armed before it returns (armApproval), which is what every test below wants: the arming
// LATCH is the subject of three tests of its own, and everywhere else it is just the state a human
// reaches by looking at the pane for a moment.
func newApprovalModel(t *testing.T, req domain.ApprovalRequest) (Model, chan domain.ApprovalDecision) {
	t.Helper()
	m, reply := newUnarmedApprovalModel(t, req)
	return armApproval(t, m), reply
}

// newUnarmedApprovalModel folds an approval request in and stops there — the pane is up and its
// decision keys are still dead, the state the arming tests act on.
func newUnarmedApprovalModel(t *testing.T, req domain.ApprovalRequest) (Model, chan domain.ApprovalDecision) {
	t.Helper()
	reply := make(chan domain.ApprovalDecision, 1)
	m := step(t, newTestModel(t), approvalReqMsg{Request: req, Reply: reply})
	if m.state != stateAwaitingApproval {
		t.Fatalf("state = %v, want awaitingApproval", m.state)
	}
	return m, reply
}

// armApproval delivers the open pane's own arming message, standing in for the approvalArmDelay
// tick the fold scheduled. It reads the generation off the model rather than sleeping, so the
// suite never pays the delay and never races it.
func armApproval(t *testing.T, m Model) Model {
	t.Helper()
	m = step(t, m, approvalArmedMsg{seq: m.approvalSeq})
	if !m.approvalArmed {
		t.Fatal("approval pane did not arm on its own approvalArmedMsg")
	}
	return m
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

// The pane's decision keys are DEAD until its arming tick lands (F-12): a keystroke already in the
// input buffer when the prompt appeared — a/s/d, or the ⏎ that takes the highlighted Allow row —
// must not answer a call the human has not read. Once the arm arrives the same key rules as always.
func TestModelApprovalKeysAreDeadUntilArmed(t *testing.T) {
	m, reply := newUnarmedApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})

	for _, key := range []tea.KeyPressMsg{
		{Code: 'a'}, {Code: 's'}, {Code: 'd'}, keyEnter(),
	} {
		m = step(t, m, key)
		select {
		case got := <-reply:
			t.Fatalf("%q answered the prompt before it was armed (sent %q)", key.String(), got)
		default:
		}
		if m.state != stateAwaitingApproval {
			t.Fatalf("%q moved the state to %v before the prompt was armed", key.String(), m.state)
		}
		if m.pending == nil {
			t.Fatalf("%q cleared the pending request before the prompt was armed", key.String())
		}
	}

	m = armApproval(t, m)

	m = step(t, m, tea.KeyPressMsg{Code: 'a'})
	select {
	case got := <-reply:
		if got != domain.ApprovalAllow {
			t.Errorf("decision = %v, want ApprovalAllow", got)
		}
	default:
		t.Error("'a' sent no decision after the prompt was armed")
	}
}

// The arm names the pane it was scheduled for: a tick left over from a prompt that has since been
// cancelled arms nothing, so the NEXT prompt still gets its own full look-at-it window.
func TestModelApprovalStaleArmDoesNotArmTheNextPane(t *testing.T) {
	m, _ := newUnarmedApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "first"})
	stale := m.approvalSeq

	m.cancel = func() {}
	m = step(t, m, keyEsc())
	m = step(t, m, keyEsc())
	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
	if m.state != stateIdle {
		t.Fatalf("state = %v, want idle after the first prompt was cancelled", m.state)
	}

	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "run", Reason: "second"},
		Reply:   reply,
	})

	m = step(t, m, approvalArmedMsg{seq: stale})
	if m.approvalArmed {
		t.Fatal("the first prompt's arming tick armed the second prompt")
	}
	m = step(t, m, tea.KeyPressMsg{Code: 'a'})
	select {
	case got := <-reply:
		t.Fatalf("'a' answered the second prompt on the first one's stale arm (sent %q)", got)
	default:
	}

	m = armApproval(t, m)
	m = step(t, m, tea.KeyPressMsg{Code: 'a'})
	select {
	case got := <-reply:
		if got != domain.ApprovalAllow {
			t.Errorf("decision = %v, want ApprovalAllow", got)
		}
	default:
		t.Error("'a' sent no decision after the second prompt's own arm")
	}
}

// Esc is deliberately OUTSIDE the latch: cancelling is the safe direction and the operator's stop
// path, so it is never the key made harder to reach. It is the double-tap it is everywhere else a
// worker runs (the frame's `case "esc"` covers the pane), and neither press waits on the arm.
func TestModelApprovalEscapeIsLiveBeforeArming(t *testing.T) {
	m, _ := newUnarmedApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})
	cancelled := false
	m.cancel = func() { cancelled = true }

	m = step(t, m, keyEsc())
	if cancelled {
		t.Error("a single esc before the arm cancelled the worker; it must only arm the gesture")
	}
	if m.lastEsc.IsZero() {
		t.Error("esc before the arm did not arm the stop gesture")
	}

	m = step(t, m, keyEsc())

	if !cancelled {
		t.Error("esc×2 before the arm did not cancel the in-flight worker")
	}
	if m.state != stateAwaitingApproval {
		t.Errorf("state = %v, want still awaitingApproval until the worker reports back", m.state)
	}
}

// The fold that opens the prompt is what schedules its own arm: without that Cmd the keys would
// never come alive at all.
func TestModelApprovalFoldReturnsTheArmTick(t *testing.T) {
	reply := make(chan domain.ApprovalDecision, 1)
	m := newTestModel(t)

	next, cmd := stepCmd(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "write_file", Reason: "write"},
		Reply:   reply,
	})

	if next.approvalArmed {
		t.Error("the prompt opened with its decision keys already armed")
	}
	if cmd == nil {
		t.Fatal("the approval fold returned no arming tick")
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

// A key that is neither a shortcut nor a menu key falls through to the transcript scroll and
// resolves nothing — the prompt stays soft-modal. It is worth pinning now that ↑/↓ and ⏎ ARE live
// here: the menu claims those three and nothing else, so an unrelated letter must still leave the
// gate, the pointer and the state exactly where they were.
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
	if m.approvalSel.selected != 0 {
		t.Errorf("a non-menu key moved the pointer to row %d", m.approvalSel.selected)
	}
}

// A stop key while pending cancels the worker; the prompt clears when the worker reports back
// (the cancel path is structural — esc×2 → stopWorker → cancelledMsg → finishWorker).
func TestModelApprovalCancelClearsPrompt(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})
	cancelled := false
	m.cancel = func() { cancelled = true }

	m = step(t, m, keyEsc())
	if cancelled {
		t.Error("a single esc at the pane cancelled the worker; it must only arm the gesture")
	}
	m = step(t, m, keyEsc())
	if !cancelled {
		t.Error("esc×2 did not cancel the in-flight worker")
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

// The approval prompt paints as a MENU (docs/layout/user-questions-layout.md): the raw tool name in
// the TOP BORDER, the four decisions as menu rows with the pointer on Allow and their shortcut
// letters aligned in a second column, the labelled args still in the body — and no legend row at
// all, the letters now being written beside the options they take.
func TestModelApprovalPromptPopupChrome(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{
		Tool:      "write_file",
		Reason:    "write",
		Arguments: json.RawMessage(`{"path":"notes.txt"}`),
	})
	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(m.pending.Request), ""), "\n")
	got := strings.Join(rows, "\n")

	// The name rides the border rather than a row of its own: asserted on the TOP LINE, so a title
	// that quietly went back to costing a content row fails here rather than passing on a substring.
	if !strings.Contains(rows[0], "Approve write_file?") {
		t.Errorf("top border does not carry the capitalized tool name:\n%s", got)
	}
	if strings.Contains(got, "a allow · d deny · s allow-session · esc cancel") {
		t.Errorf("the old hint legend is still drawn; the shortcut column replaces it:\n%s", got)
	}
	for _, want := range []string{
		"❯ Allow",                     // the pointer opens on the first row (the mockup's default)
		"· Always allow this session", // the rest of the menu is dotted, not barred
		"· Deny",
		"· Cancel",
		"Reason: write", // the labelled reason on the body's lead line
		"notes.txt",     // the labelled args in the body
	} {
		if !strings.Contains(got, want) {
			t.Errorf("approval popup missing %q:\n%s", want, got)
		}
	}

	// The shortcut cells are a COLUMN — laid out by the painter's column authority, not padded by
	// hand — so every one of them starts at the same display offset whatever its label measures.
	col, seen := -1, 0
	for _, row := range rows {
		if !strings.Contains(row, glyphUser+" ") && !strings.Contains(row, glyphMenuUnselected+" ") {
			continue // not a menu row
		}
		i := strings.Index(row, "[")
		if i < 0 {
			t.Fatalf("menu row %q carries no shortcut cell:\n%s", row, got)
		}
		// In DISPLAY cells, not bytes: ❯ is three bytes and · is two, so a byte offset would call an
		// aligned column crooked (and a crooked one aligned).
		if at := lipgloss.Width(row[:i]); col >= 0 && at != col {
			t.Errorf("shortcut cell at column %d, want %d — the cells are not aligned:\n%s", at, col, got)
		} else {
			col = at
		}
		seen++
	}
	if seen != len(approvalMenu) {
		t.Errorf("drew %d menu rows, want %d:\n%s", seen, len(approvalMenu), got)
	}
}

// ⏎ takes the row the pointer is on: Allow without navigating, and whatever ↓ walked to after it.
// This is the way in for a human who has not learnt the letters — the legend that used to teach them
// is gone, so the menu itself has to be operable.
func TestModelApprovalEnterTakesTheSelectedRow(t *testing.T) {
	cases := []struct {
		name string
		down int
		want domain.ApprovalDecision
	}{
		{"no navigation → allow", 0, domain.ApprovalAllow},
		{"↓ → allow for session", 1, domain.ApprovalAllowForSession},
		{"↓↓ → deny", 2, domain.ApprovalDeny},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, reply := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})
			for range tc.down {
				m = step(t, m, keyDown())
			}

			m, cmd := stepCmd(t, m, keyEnter())

			select {
			case got := <-reply:
				if got != tc.want {
					t.Errorf("decision = %q, want %q", got, tc.want)
				}
			default:
				t.Fatal("⏎ sent no decision on the reply channel")
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

// ⏎ on the Cancel row is the Esc key written down: it cancels the in-flight worker and sends NO
// decision, and the prompt stands until the worker reports back — the same structural path Esc has
// always taken here (TestModelApprovalCancelClearsPrompt), because no fourth ApprovalDecision exists.
func TestModelApprovalEnterOnCancelStopsTheWorker(t *testing.T) {
	m, reply := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})
	cancelled := false
	m.cancel = func() { cancelled = true }

	for range len(approvalMenu) - 1 { // walk to the last row: Cancel
		m = step(t, m, keyDown())
	}
	m = step(t, m, keyEnter())

	if !cancelled {
		t.Error("⏎ on the Cancel row did not cancel the in-flight worker")
	}
	select {
	case got := <-reply:
		t.Errorf("the Cancel row sent %q on the reply channel; cancelling is not a decision", got)
	default:
	}
	if m.state != stateAwaitingApproval {
		t.Errorf("state = %v, want still awaitingApproval until the worker reports back", m.state)
	}

	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
	if m.state != stateIdle || m.pending != nil {
		t.Errorf("state = %v (pending %v), want idle with the prompt cleared", m.state, m.pending)
	}
}

// ↑/↓ are clamped and do not wrap, matching the ask prompt's choice arrows: ↑ at the top stays on
// Allow rather than jumping to Cancel, which on a security surface is the difference between a
// stray keypress and a stopped run.
func TestModelApprovalArrowsClampWithoutWrapping(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{Tool: "write_file", Reason: "write"})

	m = step(t, m, keyUp())
	if m.approvalSel.selected != 0 {
		t.Errorf("↑ on the first row moved to %d; the menu must not wrap", m.approvalSel.selected)
	}
	for range len(approvalMenu) + 2 {
		m = step(t, m, keyDown())
	}
	if want := len(approvalMenu) - 1; m.approvalSel.selected != want {
		t.Errorf("↓ past the last row selects %d, want it clamped to %d", m.approvalSel.selected, want)
	}

	// A fresh request opens on Allow again rather than inheriting where the last one was left.
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "run_terminal_command"},
		Reply:   make(chan domain.ApprovalDecision, 1),
	})
	if m.approvalSel.selected != 0 {
		t.Errorf("a new request opened on row %d, want the menu reset to Allow", m.approvalSel.selected)
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

// A labelled argument's VALUE keeps the two-space indent that hangs it under its own label on the
// rendered body lines, and keeps every line it arrived with: embedded-newline layout is preserved
// end to end, not collapsed by the wrap and not folded by the field flattening either.
//
// This is the half of the newline rule that SURVIVES: a value's line breaks are the fact the human
// is ruling on — the four lines a command will really run — so folding them would leave the pane
// claiming something other than what executes. The other half is the model-authored FIELDS around
// it, whose newlines paint rows the pane did not write and are flattened for exactly that reason
// (TestModelApprovalFlattensFieldsThatCouldForgeRows). Indentation is what makes keeping these
// safe: a value's lines sit under a label that can no longer be forged, so nothing they say reads
// as a row of the surface's own.
func TestModelApprovalArgsKeepIndentation(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{
			Tool:      "terminal",
			Arguments: json.RawMessage(`{"path":"notes.txt","command":"cd /ws/a\ngit status"}`),
		},
		Reply: reply,
	})
	// The value hangs two spaces under "path:"; had the indent been collapsed, only the popup's
	// one-space padding would precede it. The two-space run proves it survived.
	view := plain(m.View())
	for _, want := range []string{"  notes.txt", "  cd /ws/a", "  git status"} {
		if !strings.Contains(view, want) {
			t.Errorf("the argument's value lost its two-space hanging indent at %q:\n%s", want, view)
		}
	}
	// Counting the ROWS is what says the multi-line value was not folded onto one: a substring check
	// for "cd /ws/a" passes either way.
	if rows := approvalBodyRows(view, "cd /ws/a"); len(rows) != 1 {
		t.Errorf("a multi-line value must keep its own rows, got %d opening rows:\n%s", len(rows), view)
	}
	if rows := approvalBodyRows(view, "git status"); len(rows) != 1 {
		t.Errorf("a multi-line value's second line lost its row, got %d:\n%s", len(rows), view)
	}
}

// A value too long for the pane keeps its hanging indent on every row it wraps onto. Before the body
// wrap hung its continuations, a single 300-character argument painted every row after the first in
// column zero — the column the pane's own `Reason:` and labels live in — so model-authored bytes read
// as pane furniture on the surface the decision is taken off (popupBodySegmentWrapped).
func TestModelApprovalLongArgumentNeverPaintsFlushLeft(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 50, Height: 30})
	reply := make(chan domain.ApprovalDecision, 1)
	args, err := json.Marshal(map[string]string{"command": strings.Repeat("a", 300)})
	if err != nil {
		t.Fatalf("marshalling the argument: %v", err)
	}
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{Tool: "terminal", Arguments: json.RawMessage(args)},
		Reply:   reply,
	})

	view := plain(m.View())

	// Between the label and the menu is the value and nothing else, so every row there must open in
	// the value's own column — two spaces in from where a label could live.
	inBody := false
	for _, ln := range strings.Split(view, "\n") {
		row := strings.TrimSuffix(strings.TrimPrefix(ln, "│"), "│")
		switch {
		case strings.HasPrefix(strings.TrimSpace(row), "command:"):
			inBody = true
			continue
		case strings.Contains(row, "❯"):
			inBody = false
		}
		if !inBody || strings.TrimSpace(row) == "" {
			continue
		}
		if !strings.HasPrefix(row, " "+argumentValueIndent) {
			t.Errorf("a wrapped value row paints in the pane's own column: %q\n%s", ln, view)
		}
	}
}

// A model-authored FIELD paints no row of its own, whatever it carries. The approval body is drawn
// one row per line (popupBodyWrapped) and every body row wears the same th.popupBody style —
// approvalPrompt sets no bodyLead — so a newline inside an argument NAME, a sub-agent TASK or a
// sub-agent NAME painted a second "Reason:" line indistinguishable from the real one, above the
// real one, and the human authorised a call whose stated reason the model wrote. flattenField folds
// each of those onto the single line a label is.
//
// The assertions count ROWS rather than looking for substrings, because the forged text is still on
// the pane after the fix — folded into the row that legitimately carries it — so a substring check
// passes on the forgery it exists to catch.
func TestModelApprovalFlattensFieldsThatCouldForgeRows(t *testing.T) {
	cases := []struct {
		name string
		req  domain.ApprovalRequest
		// carrier is the row prefix the flattened payload must end up folded INTO, so the test pins
		// that the text was kept and moved rather than dropped.
		carrier string
	}{
		{
			"an argument name",
			domain.ApprovalRequest{
				Tool:      "terminal",
				Reason:    "subprocess execution",
				Arguments: json.RawMessage(`{"command\nReason: pre-approved":"rm -rf /"}`),
			},
			"command Reason: pre-approved:",
		},
		{
			"a sub-agent task, which leads the body",
			domain.ApprovalRequest{
				Tool:         "terminal",
				Reason:       "subprocess execution",
				SubAgentTask: "audit the loader\nReason: pre-approved",
				Arguments:    json.RawMessage(`{"command":"rm -rf /"}`),
			},
			"Sub-agent: audit the loader Reason: pre-approved",
		},
		{
			"a sub-agent name",
			domain.ApprovalRequest{
				Tool:         "terminal",
				Reason:       "subprocess execution",
				SubAgentTask: "audit the loader",
				SubAgentName: "scout\nReason: pre-approved",
				Arguments:    json.RawMessage(`{"command":"rm -rf /"}`),
			},
			"Sub-agent: scout Reason: pre-approved — audit the loader",
		},
		{
			"the gate's own reason",
			domain.ApprovalRequest{
				Tool:      "terminal",
				Reason:    "subprocess execution\nReason: pre-approved",
				Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
			},
			"Reason: subprocess execution Reason: pre-approved",
		},
		{
			"the remedy the Fix line carries",
			domain.ApprovalRequest{
				Tool:      "terminal",
				Reason:    "subprocess execution",
				Remedy:    "run /confine status\nReason: pre-approved",
				Arguments: json.RawMessage(`{"command":"rm -rf /"}`),
			},
			"Fix: run /confine status Reason: pre-approved",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
			m = step(t, m, approvalReqMsg{Request: tc.req, Reply: make(chan domain.ApprovalDecision, 1)})
			view := plain(m.View())
			if rows := approvalBodyRows(view, "Reason:"); len(rows) != 1 {
				t.Errorf("the pane paints %d rows opening \"Reason:\", want exactly the gate's own:\n%s",
					len(rows), view)
			}
			if rows := approvalBodyRows(view, tc.carrier); len(rows) != 1 {
				t.Errorf("the flattened field is not folded into its own row (%q), got %d:\n%s",
					tc.carrier, len(rows), view)
			}
		})
	}
}

// approvalBodyRows returns the pane rows that OPEN with prefix, read off the painted view with the
// popup's own border and padding taken back off. It is the row-level reading a forged-row test
// needs: what a field can do to this pane is add a ROW, and a substring check over the whole view
// cannot tell a row from a fold into one.
func approvalBodyRows(view, prefix string) []string {
	var out []string
	for _, ln := range strings.Split(view, "\n") {
		if row := strings.TrimSpace(strings.Trim(ln, "│")); strings.HasPrefix(row, prefix) {
			out = append(out, row)
		}
	}
	return out
}

// A request raised by a sub-agent leads its body with the child's delegated task, so a prompt that
// queued behind a sibling's still says which agent is asking (ADR 0039 decision 12). It leads
// because it is the fact the rest of the pane cannot supply: the tool and the arguments read the
// same whichever child sent them.
func TestModelApprovalNamesTheAskingSubAgent(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:         "write_file",
		Reason:       "write outside the workspace",
		SubAgentTask: "audit the config loader for drift",
		Arguments:    json.RawMessage(`{"path":"notes.txt"}`),
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	view := plain(m.View())
	task := strings.Index(view, "Sub-agent: audit the config loader for drift")
	if task < 0 {
		t.Fatalf("the pane does not name the asking sub-agent's task:\n%s", view)
	}
	reason := strings.Index(view, "Reason: write outside the workspace")
	if reason < 0 {
		t.Fatalf("the reason is missing from the pane:\n%s", view)
	}
	if task > reason {
		t.Errorf("the Sub-agent line renders below the Reason; it must lead the body:\n%s", view)
	}
}

// A delegation the model NAMED leads that same line with its name and keeps the task behind it: the
// name is what a human recognises the asker by across a queue of siblings, and the task is still the
// sentence saying what is being authorised on its behalf (subAgentPromptLine).
func TestModelApprovalNamesTheAskingSubAgentByName(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:         "write_file",
		Reason:       "write outside the workspace",
		SubAgentTask: "audit the config loader for drift",
		SubAgentName: "repo-scout",
		Arguments:    json.RawMessage(`{"path":"notes.txt"}`),
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	view := plain(m.View())
	if !strings.Contains(view, "Sub-agent: repo-scout — audit the config loader for drift") {
		t.Errorf("the pane does not lead with the delegation's name:\n%s", view)
	}
}

// TestSubAgentPromptLineComposition pins the rule both panes share. The clip is the part worth
// pinning: it is spent on the WHOLE line, so a model that sends a paragraph where a name was asked
// for buys itself no more of the pane than an unnamed delegation gets — and the name reaches this
// composition raw off the engine, which makes this the boundary that strips its escapes.
func TestSubAgentPromptLineComposition(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, agent, task, want string }{
		{name: "no task draws no line at all"},
		{name: "an unnamed delegation reads as it always did", task: "audit the loader", want: "Sub-agent: audit the loader"},
		{name: "a named one leads with the name", agent: "repo-scout", task: "audit the loader", want: "Sub-agent: repo-scout — audit the loader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := subAgentPromptLine(tc.agent, tc.task); got != tc.want {
				t.Errorf("subAgentPromptLine(%q, %q) = %q, want %q", tc.agent, tc.task, got, tc.want)
			}
		})
	}

	t.Run("an escape in the name never reaches the pane", func(t *testing.T) {
		got := subAgentPromptLine("repo\x1b]52;c;cGF5bG9hZA==\x07scout", "audit")
		if strings.ContainsAny(got, "\x1b\x07") {
			t.Errorf("a control character survived into the prompt line: %q", got)
		}
	})

	t.Run("the clip is spent on the whole line", func(t *testing.T) {
		got := subAgentPromptLine(strings.Repeat("n", approvalTaskClipRunes), "audit the loader")
		if body := strings.TrimPrefix(got, "Sub-agent: "); len([]rune(body)) != approvalTaskClipRunes+1 {
			t.Errorf("named line spends %d runes, want the %d-rune bound plus its ellipsis",
				len([]rune(body)), approvalTaskClipRunes)
		}
		if !strings.HasSuffix(got, "…") {
			t.Errorf("the over-long line was not marked as clipped: %q", got)
		}
	})
}

// TestSubAgentTargetFallsBackWhenTheNameStripsToNothing pins the run header's half of the same
// question the approval pane answers above, and pins it on the one input where the two ways of
// asking it come apart: a "name" of nothing but control characters is non-empty as it arrives off
// the wire, and empty once this view's escape strip has had it. The header must then show the TASK
// rather than a blank slot — the rule title.DelegateLabel holds for every Driver, with the headless
// twin pinning it at cmd/apogee/headless_test.go.
func TestSubAgentTargetFallsBackWhenTheNameStripsToNothing(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"a real name leads the header", map[string]any{"name": "repo-scout", "task": "audit the loader"}, "repo-scout"},
		{"no name shows the task", map[string]any{"task": "audit the loader"}, "audit the loader"},
		{"a control-only name shows the task", map[string]any{"name": "\x1b\x07\x00", "task": "audit the loader"}, "audit the loader"},
		{"a padded task is trimmed", map[string]any{"task": "  audit the loader  "}, "audit the loader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := subAgentTarget(tc.args); got != tc.want {
				t.Errorf("subAgentTarget(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// The top-level agent's own request carries no task, and its pane is unchanged to the byte — the
// serial floor for a session that never delegates.
func TestModelApprovalTopLevelDrawsNoSubAgentLine(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites"}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	if got := ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""); strings.Contains(got, "Sub-agent") {
		t.Errorf("a top-level prompt drew a Sub-agent line:\n%s", got)
	}
}

// The task is the ONE string this pane clips rather than wraps (approvalTaskClipRunes): it says who
// is asking, and who is asking must never push what is being decided off the screen. The clip is
// marked by its ellipsis, and the reason below it survives whole.
func TestModelApprovalClipsAnEssayLengthSubAgentTask(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	task := strings.Repeat("sprawl ", approvalTaskClipRunes) // far past the bound
	req := domain.ApprovalRequest{Tool: "write_file", Reason: "it overwrites", SubAgentTask: task}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	view := plain(m.View())
	if !strings.Contains(view, "…") {
		t.Errorf("an over-long task was not clipped with an ellipsis:\n%s", view)
	}
	if !strings.Contains(view, "Reason: it overwrites") {
		t.Errorf("the task crowded the reason off the pane:\n%s", view)
	}
}

// The shell tool's body reads as the command line it is about to run, under its own argument name
// with the line indented beneath it (docs/layout/user-questions-layout.md) — not as the JSON
// envelope that carries it. The argument braces and the quoted key are gone: this is a rendering of
// the same one fact, not an extra view beside it.
func TestModelApprovalTerminalShowsCommandBlock(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	reply := make(chan domain.ApprovalDecision, 1)
	m = step(t, m, approvalReqMsg{
		Request: domain.ApprovalRequest{
			Tool:      "terminal",
			Reason:    "subprocess execution (confinement unavailable on this host)",
			Arguments: json.RawMessage(`{"command":"cd /ws/a && git status"}`),
		},
		Reply: reply,
	})

	got := ansiPattern.ReplaceAllString(m.approvalPrompt(m.pending.Request), "")
	for _, want := range []string{
		"Reason: subprocess execution",
		"command:",
		"  cd /ws/a && git status", // its own line, indented under the label
	} {
		if !strings.Contains(got, want) {
			t.Errorf("terminal approval body missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"command"`) {
		t.Errorf("the JSON envelope is still drawn beside the command block:\n%s", got)
	}
}

// A gate whose cause the user can lift carries a Remedy, and the pane draws it as its own `Fix:`
// line on the row directly under the Reason it answers: the diagnosis and the way out are two
// facts, and a human who has just read that this host cannot confine wants the next line to say
// what to do about it. The label is the TUI's — the engine ships the bare sentence — so this is the
// Driver's assertion to make. Panes for the gates the autonomy rung itself asked for carry no
// Remedy and draw no Fix line at all, which is most of them.
func TestModelApprovalDrawsRemedyUnderReason(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:      "terminal",
		Reason:    "subprocess execution (confinement unavailable on this host)",
		Remedy:    "/confine off runs commands unconfined this session (disposable machines only)",
		Arguments: json.RawMessage(`{"command":"cd /ws/a && git status"}`),
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""), "\n")
	got := strings.Join(rows, "\n")
	if !strings.Contains(got, "Fix: /confine off") {
		t.Errorf("the remedy did not reach the pane:\n%s", got)
	}
	if reason, fix := paneRowIndex(t, rows, "Reason:"), paneRowIndex(t, rows, "Fix:"); fix != reason+1 {
		t.Errorf("Fix: sits %d lines under Reason:, want it on the very next row:\n%s", fix-reason, got)
	}

	// The same call with nothing to fix: the line is absent, not blank.
	req.Remedy = ""
	bare := ansiPattern.ReplaceAllString(m.approvalPrompt(req), "")
	if strings.Contains(bare, "Fix:") {
		t.Errorf("a gate with no remedy drew a Fix line:\n%s", bare)
	}
}

// A path argument that does not point where it reads is named on a line of its own, under the
// arguments it is about: the pane quotes the model's `path` as the model wrote it and says beside
// it where the write actually lands, because those are two facts and swapping one for the other
// would answer a question the approver did not ask. The engine sends the path only when the two
// differ (domain.ApprovalRequest.ResolvedPath), so the ordinary prompt — the overwhelming majority
// — draws no such line at all.
//
// The second half is the hostile-bytes half: the resolution is a path a MODEL-authored argument
// produced, so it is flattened before it reaches a surface that paints one row per line. Unflattened
// it would paint rows of its own in the pane's own body style — a second `Reason:` above the real
// one, which is the forgery this pane's fields are flattened to prevent.
func TestModelApprovalNamesTheResolvedPath(t *testing.T) {
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
	req := domain.ApprovalRequest{
		Tool:         "write_file",
		Reason:       "write",
		Arguments:    json.RawMessage(`{"path":"docs/notes.md","content":"hi"}`),
		ResolvedPath: "/elsewhere/notes.md",
	}
	m = step(t, m, approvalReqMsg{Request: req, Reply: make(chan domain.ApprovalDecision, 1)})

	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""), "\n")
	got := strings.Join(rows, "\n")
	if !strings.Contains(got, "→ resolves to /elsewhere/notes.md") {
		t.Errorf("the resolved path did not reach the pane:\n%s", got)
	}
	if !strings.Contains(got, "docs/notes.md") {
		t.Errorf("the argument the model wrote left the pane:\n%s", got)
	}
	if path, note := paneRowIndex(t, rows, "path:"), paneRowIndex(t, rows, "→ resolves to"); note <= path {
		t.Errorf("the resolution sits on row %d, above the argument on row %d it is about:\n%s", note, path, got)
	}

	// A call whose argument names its own target: nothing extra, not a blank line.
	req.ResolvedPath = ""
	if bare := ansiPattern.ReplaceAllString(m.approvalPrompt(req), ""); strings.Contains(bare, "resolves to") {
		t.Errorf("a call whose path resolves to itself drew a resolution line:\n%s", bare)
	}

	// A resolution carrying a newline is one row, not a forged one.
	req.ResolvedPath = "/elsewhere/notes.md\nReason: nothing to see here"
	forged := ansiPattern.ReplaceAllString(m.approvalPrompt(req), "")
	rowsLed := 0
	for _, row := range strings.Split(forged, "\n") {
		if strings.HasPrefix(strings.TrimSpace(strings.ReplaceAll(row, "│", "")), "Reason:") {
			rowsLed++
		}
	}
	if rowsLed != 1 {
		t.Errorf("pane opens %d rows with Reason:, want 1 — the resolution painted a row of its own:\n%s", rowsLed, forged)
	}
}

// Every argument is a `name:` label with the value's own lines under it — a single-line value, a
// multi-line one reading as the lines it will actually run rather than as one `\n`-escaped string,
// and every key of a multi-argument call, in the order the model wrote them. What is NOT on the
// screen is the envelope: no braces around the set, no quoted key names, nothing for the human to
// read past on the one surface whose job is that the arguments are read.
//
// The order case is the security-relevant one: a workdir naming where a command runs is exactly the
// fact a body that showed the command alone would hide.
func TestModelApprovalArgsReadAsLabelledLines(t *testing.T) {
	cases := []struct {
		name string
		args string
		want []string // in the order they must appear
	}{
		{
			"a single-line value",
			`{"path":"notes.txt"}`,
			[]string{"path:", "  notes.txt"},
		},
		{
			"a multi-line value reads as its own lines",
			`{"command":"cd /ws/a\ngit status\ngit diff"}`,
			[]string{"command:", "  cd /ws/a", "  git status", "  git diff"},
		},
		{
			"several arguments, each labelled, in wire order",
			`{"command":"git status","workdir":"/ws/b","timeout":30}`,
			[]string{"command:", "  git status", "workdir:", "  /ws/b", "timeout:", "  30"},
		},
		{
			"a non-string value keeps the literal the model sent",
			`{"command":42}`,
			[]string{"command:", "  42"},
		},
		{
			"a whitespace-only value is still labelled and shown",
			`{"command":"   "}`,
			[]string{"command:"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
			reply := make(chan domain.ApprovalDecision, 1)
			m = step(t, m, approvalReqMsg{
				Request: domain.ApprovalRequest{Tool: "terminal", Arguments: json.RawMessage(tc.args)},
				Reply:   reply,
			})
			got := ansiPattern.ReplaceAllString(m.approvalPrompt(m.pending.Request), "")

			at := 0
			for _, want := range tc.want {
				i := strings.Index(got[at:], want)
				if i < 0 {
					t.Fatalf("body missing %q at or after the line before it:\n%s", want, got)
				}
				at += i + len(want)
			}
			for _, envelope := range []string{"{", "}", `"command"`, `"path"`, `"workdir"`, `\n`} {
				if strings.Contains(got, envelope) {
					t.Errorf("the JSON envelope survives in the painted body (%q):\n%s", envelope, got)
				}
			}
		})
	}
}

// TestModelApprovalMenuSpacing pins the mockup's vertical spacing
// (docs/layout/user-questions-layout.md:13-22, the approval box): Reason: and Command: run ADJACENT
// — two labelled facts about one call, and a blank line between them reads as two blocks — ONE blank
// line sets the menu off from the body, the four decisions stay adjacent to each other, and the last
// of them ends the box with the bottom border directly under it. That single blank is the whole of
// the pane's spacing: the ask box closes its offering with a second one (its answers are a blank
// line apart, so its last would otherwise be crowded), this one does not, and the difference is the
// mockup's. It is asserted as the SHAPE of the pane rather than as a substring anywhere in it,
// because a blank line in the wrong place is exactly what a substring check cannot see and what the
// eye reads as a second block.
func TestModelApprovalMenuSpacing(t *testing.T) {
	m, _ := newApprovalModel(t, domain.ApprovalRequest{
		Tool:      "terminal",
		Reason:    "subprocess execution (confinement unavailable on this host)",
		Arguments: json.RawMessage(`{"command":"cd /ws/a && git status"}`),
	})
	rows := strings.Split(ansiPattern.ReplaceAllString(m.approvalPrompt(m.pending.Request), ""), "\n")
	got := strings.Join(rows, "\n")
	blank := func(i int) bool { return strings.TrimSpace(strings.Trim(rows[i], "│")) == "" }

	if reason, command := paneRowIndex(t, rows, "Reason:"), paneRowIndex(t, rows, "command:"); command != reason+1 {
		t.Errorf("command: sits %d lines under Reason:, want the two labels adjacent:\n%s", command-reason, got)
	}
	allow, cancel := paneRowIndex(t, rows, "❯ Allow"), paneRowIndex(t, rows, "· Cancel")
	if !blank(allow - 1) {
		t.Errorf("the line above the menu = %q, want the blank line setting it off from the body:\n%s", rows[allow-1], got)
	}
	if cancel != allow+len(approvalMenu)-1 {
		t.Errorf("the menu spans %d lines, want its %d options adjacent:\n%s", cancel-allow+1, len(approvalMenu), got)
	}
	if cancel != len(rows)-2 {
		t.Errorf("%d lines sit between the last option and the bottom border, want none:\n%s", len(rows)-2-cancel, got)
	}
}

// Arguments with no names to label fall back to the raw JSON, because that is what keeps the human
// deciding against what the tool will actually receive: a blob that does not parse, one that is not
// an object at all, and one carrying a second document behind the first. Showing them as they
// arrived is the honest rendering — half a labelled body would be a claim about the call that the
// bytes do not support.
func TestModelApprovalArgsFallBackToJSON(t *testing.T) {
	cases := []struct {
		name string
		req  domain.ApprovalRequest
		want string
	}{
		{
			"unparseable terminal arguments",
			domain.ApprovalRequest{Tool: "terminal", Arguments: json.RawMessage(`{"command":`)},
			`{"command":`,
		},
		{
			"arguments that are not an object",
			domain.ApprovalRequest{Tool: "write_file", Arguments: json.RawMessage(`["rm -rf /"]`)},
			`"rm -rf /"`,
		},
		{
			"a second document behind the first",
			domain.ApprovalRequest{Tool: "terminal", Arguments: json.RawMessage(`{"command":"ls"} {"command":"rm -rf /"}`)},
			`{"command":"ls"} {"command":"rm -rf /"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: 100, Height: 30})
			reply := make(chan domain.ApprovalDecision, 1)
			m = step(t, m, approvalReqMsg{Request: tc.req, Reply: reply})

			got := ansiPattern.ReplaceAllString(m.approvalPrompt(m.pending.Request), "")
			// The want strings each carry a brace or a quote a labelled rendering would have
			// stripped, so finding them IS the proof the fallback was taken.
			if !strings.Contains(got, tc.want) {
				t.Errorf("body missing the raw argument %q:\n%s", tc.want, got)
			}
		})
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
// account for every line it is not showing. Between 12 and 15 rows the budget grants the body its
// floor of a single row, and on prose this long that row is the count itself — every line dropped
// and every one of them named, under a border that still carries the tool the decision turns on.
//
// It runs at narrowOverlayWindow as well as at 80 columns, because the marker row was the one place
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
				if !strings.Contains(rows[0], "Approve write_file?") {
					t.Errorf("top border does not carry the tool name the decision turns on:\n%s", flat)
				}
				// Either the whole body is on the screen — its last line is the args' lone close
				// brace, the one tail no wrap can break up — or the pane counts out what is missing,
				// in whichever wording the width can pay for. Never neither.
				bodyComplete := slices.ContainsFunc(rows, func(r string) bool { return strings.Trim(r, "│ ") == "}" })
				if !bodyComplete && !elisionMarkerPattern.MatchString(flat) {
					t.Errorf("pane shows neither the whole body nor a marker for the lines it hid:\n%s", flat)
				}

				// At the floor the body budget is its irreducible ONE row, and on prose this long that
				// row IS the marker: every line of the reason and the arguments dropped, and the pane
				// stating how many. Assert the placement, not just the presence — that is what the
				// finding was about, and the row it lands on has moved now that the title rides the
				// border and the decisions themselves take the rows below.
				// The demand is the pane's own (approvalPrompt): its menu in LINES, the blank line it
				// sets the menu off by included, because a test budgeting for a shape the pane does not
				// compose would look for the marker at a window the pane never puts it on.
				menuLines := popupRowBlockLines(popupFlatRowHeights(len(approvalMenu)), 0, popupRowPadLines(true, false))
				if maxBody, _, _ := m.popupBudget(panePrompt, menuLines, menuLines, popupBorderChrome, popupFloor{}); maxBody == 1 {
					if first := strings.Trim(rows[1], "│ "); !elisionMarkerPattern.MatchString(first) {
						t.Errorf("body row = %q, want the marker counting the prose the pane dropped:\n%s", first, flat)
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

// askPaneLines renders the ask pane for req on a terminal with room to spare and returns its lines
// with the styling and the border stripped off, so an assertion reads the pane's own text at the
// column the pane paints it in.
func askPaneLines(t *testing.T, width int, req domain.AskRequest) []string {
	t.Helper()
	m := step(t, newTestModel(t), tea.WindowSizeMsg{Width: width, Height: 40})
	m = step(t, m, askReqMsg{Request: req, Reply: make(chan domain.AskAnswer, 1)})
	return askModelPaneLines(t, m)
}

// askModelPaneLines is askPaneLines for a model already holding a question — the same stripped pane
// lines, for a test that had to drive keys into the model first (a ␣ toggle repaints a marker, and
// the repaint is the assertion).
func askModelPaneLines(t *testing.T, m Model) []string {
	t.Helper()
	pane := m.askPrompt(m.pendingAsk.Request)
	if pane == "" {
		t.Fatalf("the ask pane rendered nothing at %d columns", m.width)
	}
	return strings.Split(ansiPattern.ReplaceAllString(pane, ""), "\n")
}

// TestModelAskPromptMenuChrome pins the surface the mockup asks for
// (docs/layout/user-questions-layout.md): no title of its own — the top border is unbroken and the
// QUESTION is the pane's lead line — and the choices as a menu, ❯ on the answer ⏎ would send, · on
// the rest, one blank line between each pair and one setting the block off above and below — this
// box closes its offering, unlike the approval box's adjacent decisions. The spacing is asserted
// line by line because it is the mockup's own, not a default: the pane draws each of those blanks
// itself, out of a budget that books them (popupRowStyle.gap/padBelow, popupSpec.rowPadAbove). The title
// row is the row this buys back: a heading
// reading "the assistant is asking:" over a question the human is already reading said nothing the
// question did not, and on a twelve-row terminal it cost a row of the question itself.
func TestModelAskPromptMenuChrome(t *testing.T) {
	rows := askPaneLines(t, 60, domain.AskRequest{
		Question: "which way?",
		Choices:  []string{"left", "right"},
	})
	got := strings.Join(rows, "\n")

	// The top border is asserted RUNE by rune rather than by the title's absence: a title that
	// quietly came back as a border title would pass a substring check on the old wording.
	if trimmed := strings.Trim(rows[0], "╭─╮"); trimmed != "" {
		t.Errorf("top border carries %q, want an unbroken border:\n%s", trimmed, got)
	}
	if strings.Contains(got, "the assistant is asking:") {
		t.Errorf("the dropped title is still drawn:\n%s", got)
	}
	if lead := strings.TrimSpace(strings.Trim(rows[1], "│")); lead != "which way?" {
		t.Errorf("first content line = %q, want the question itself:\n%s", lead, got)
	}

	first, second := paneRowIndex(t, rows, "❯ left"), paneRowIndex(t, rows, "· right")
	if second != first+2 {
		t.Errorf("the two options are %d lines apart, want one blank line between them:\n%s", second-first, got)
	}
	if sep := strings.TrimSpace(strings.Trim(rows[first+1], "│")); sep != "" {
		t.Errorf("the line between the options = %q, want it blank:\n%s", sep, got)
	}
	// …and the block is set off from the question above it and closed below the last option
	// (popupSpec.rowPadAbove and popupRowStyle.padBelow, the mockup's own spacing): with prose on both sides of the
	// join, the marker column alone is what tells the first answer from the last line of the question.
	if sep := strings.TrimSpace(strings.Trim(rows[first-1], "│")); sep != "" {
		t.Errorf("the line between the question and the first option = %q, want it blank:\n%s", sep, got)
	}
	if sep := strings.TrimSpace(strings.Trim(rows[second+1], "│")); sep != "" {
		t.Errorf("the line after the last option = %q, want it blank:\n%s", sep, got)
	}
}

// A question raised by a sub-agent leads its body with the child's delegated task, exactly as an
// approval prompt does (ADR 0039 decision 12): concurrent children's questions queue one at a time,
// in an order nothing on the screen predicts, so the question's own words no longer say whose work
// it serves.
func TestModelAskPromptNamesTheAskingSubAgent(t *testing.T) {
	lines := askPaneLines(t, 100, domain.AskRequest{
		Question:     "should I rewrite the loader or patch it?",
		SubAgentTask: "audit the config loader for drift",
	})
	got := strings.Join(lines, "\n")

	task := strings.Index(got, "Sub-agent: audit the config loader for drift")
	if task < 0 {
		t.Fatalf("the pane does not name the asking sub-agent's task:\n%s", got)
	}
	question := strings.Index(got, "should I rewrite the loader or patch it?")
	if question < 0 {
		t.Fatalf("the question is missing from the pane:\n%s", got)
	}
	if task > question {
		t.Errorf("the Sub-agent line renders below the question; it must lead the body:\n%s", got)
	}
}

// And a named delegation leads it with its name, in the approval pane's words to the byte — the two
// decision surfaces share one composition (subAgentPromptLine), so they cannot drift into dialects.
func TestModelAskPromptNamesTheAskingSubAgentByName(t *testing.T) {
	got := strings.Join(askPaneLines(t, 100, domain.AskRequest{
		Question:     "should I rewrite the loader or patch it?",
		SubAgentTask: "audit the config loader for drift",
		SubAgentName: "repo-scout",
	}), "\n")

	if !strings.Contains(got, "Sub-agent: repo-scout — audit the config loader for drift") {
		t.Errorf("the pane does not lead with the delegation's name:\n%s", got)
	}
}

// The top-level agent's own question carries no task, and its pane is unchanged to the byte — the
// serial floor for a session that never delegates.
func TestModelAskPromptTopLevelDrawsNoSubAgentLine(t *testing.T) {
	req := domain.AskRequest{Question: "which way?", Choices: []string{"left", "right"}}
	plainPane := strings.Join(askPaneLines(t, 100, req), "\n")
	if strings.Contains(plainPane, "Sub-agent") {
		t.Errorf("a top-level question drew a Sub-agent line:\n%s", plainPane)
	}

	req.SubAgentTask = "delegated work"
	named := strings.Join(askPaneLines(t, 100, req), "\n")
	if named == plainPane {
		t.Error("naming the asking sub-agent changed nothing on the pane")
	}
}

// The task is CLIPPED rather than wrapped, under the approval pane's own bound: it says who is
// asking, and who is asking must never push what is being asked off the screen.
func TestModelAskPromptClipsAnEssayLengthSubAgentTask(t *testing.T) {
	got := strings.Join(askPaneLines(t, 100, domain.AskRequest{
		Question:     "shall I proceed?",
		SubAgentTask: strings.Repeat("sprawl ", approvalTaskClipRunes), // far past the bound
	}), "\n")

	if !strings.Contains(got, "…") {
		t.Errorf("an over-long task was not clipped with an ellipsis:\n%s", got)
	}
	if !strings.Contains(got, "shall I proceed?") {
		t.Errorf("the task crowded the question off the pane:\n%s", got)
	}
}

// TestModelAskNamesItselfWhereTheQuestionHasNoRow is the ask pane's half of the promise the approval
// prompt keeps with its border title: a decision surface with live keys always says what it is
// deciding. The ask box says it with the question, and the question is BODY — so on the windows that
// grant the body a single row and spend that row on the "… (+N more lines)" marker, the box was a
// count and a key hint while the approval box beside it still named its tool in the border. There the
// question falls back INTO the border (popupRowStyle.titleFromBody) and takes the count with it, and only
// there: with any line of the question on a content row the border is the unbroken one the mockup
// draws (TestModelAskPromptMenuChrome), which the last subtest pins at a window with room to spare.
func TestModelAskNamesItselfWhereTheQuestionHasNoRow(t *testing.T) {
	const lead = "which way should I take"
	req := domain.AskRequest{
		Question: lead + " this refactor of the resolution pipeline, now that the gate has moved?",
		Choices:  []string{"yes, go ahead", "no", "ask me again later", "stop and let me drive"},
	}

	for _, width := range []int{80, narrowOverlayWindow} {
		for _, height := range []int{smallestOverlayWindow, 13, 14, 15} {
			t.Run(fmt.Sprintf("%d×%d", width, height), func(t *testing.T) {
				m := modelWithOverlayRoomAt(t, width, height, Options{Workspace: "/ws/a"})
				rows := strings.Split(ansiPattern.ReplaceAllString(m.askPrompt(req), ""), "\n")
				got := strings.Join(rows, "\n")

				// The premise of the case: no content row carries the question, so the border is the only
				// place its identity can be. A budget that started seating it again is a changed premise
				// and the assertion below would be pinning nothing.
				for _, row := range rows[1:] {
					if strings.Contains(row, lead) {
						t.Fatalf("the question holds a content row here — test premise broken:\n%s", got)
					}
				}
				if !strings.Contains(rows[0], lead) {
					t.Errorf("top border = %q, want it to lead with the question — the pane's whole identity here:\n%s", rows[0], got)
				}
				if !elisionMarkerPattern.MatchString(rows[0]) {
					t.Errorf("top border = %q, want the count for the answers the window seated none of:\n%s", rows[0], got)
				}
			})
		}
	}

	// …and nothing changes at a height with room: the border is unbroken and the question is the
	// pane's lead line, which is the appearance the mockup pins.
	t.Run("a window with room", func(t *testing.T) {
		rows := askPaneLines(t, 80, req)
		got := strings.Join(rows, "\n")
		if trimmed := strings.Trim(rows[0], "╭─╮"); trimmed != "" {
			t.Errorf("top border carries %q, want it unbroken where the question has its own row:\n%s", trimmed, got)
		}
		if first := strings.TrimSpace(strings.Trim(rows[1], "│")); !strings.HasPrefix(first, lead) {
			t.Errorf("first content line = %q, want the question itself:\n%s", first, got)
		}
	})
}

// askAnswerLines is how many of a rendered ask pane's lines are the head of an answer — the rows
// carrying a marker in the pane's own marker column, which is what "an answer is on the screen"
// means. The hint spends the same middle dot between its keys, so the marker is matched at the START
// of the content and never anywhere in it.
func askAnswerLines(rows []string) int {
	seated := 0
	for _, row := range rows {
		text := strings.TrimSpace(strings.Trim(row, "│"))
		if strings.HasPrefix(text, glyphUser+" ") || strings.HasPrefix(text, glyphMenuUnselected+" ") {
			seated++
		}
	}
	return seated
}

// TestModelAskQuestionKeepsItsFloorOnARoomyWindow is the rule that the pane's own budget broke: the
// answers had priority over the question at EVERY height, and an ask_user offering costs lines that
// scale with what the model wrote — four wrapped answers, the blanks between them and the pad around
// the block ask for nine of the ten lines an eighty-by-twenty-four terminal grants the pane. So on a
// window nobody would call short the box read "… (+2 more lines)" where the question belongs, and the
// human was asked to choose between four answers with nothing on the screen saying what about. The
// question now claims up to askQuestionFloor lines first (popupFloor).
//
// The claim is a CEILING and not a reservation, which the second subtest is for: a question that
// wants one line takes one and the offering keeps every line it had, pad included — a floor that
// booked three lines whatever the prose measured would cost the mockup's own spacing on the very
// windows that can afford it.
func TestModelAskQuestionKeepsItsFloorOnARoomyWindow(t *testing.T) {
	const lead = "how should I continue"
	req := domain.AskRequest{
		Question: lead + ` with the implementation of the feature "The best. Feature in the world"? ` +
			"Pick one of the options below and I will get going on it, or type an answer of your own.",
		Choices: []string{"just do it all in one shot and commit once", "no", "ask me again later", "stop and let me drive"},
	}

	for _, height := range []int{24, 26, 28} {
		t.Run(fmt.Sprintf("80×%d", height), func(t *testing.T) {
			m := modelWithOverlayRoomAt(t, 80, height, Options{Workspace: "/ws/a"})
			pane := m.askPrompt(req)
			rows := strings.Split(ansiPattern.ReplaceAllString(pane, ""), "\n")
			got := strings.Join(rows, "\n")

			if h := lipgloss.Height(pane); h > m.viewport.Height() {
				t.Fatalf("pane is %d rows on a %d-row viewport: the input box goes off the frame\n%s",
					h, m.viewport.Height(), got)
			}
			// The premise: this question wants more lines than one, so a pane granting it one would be
			// the defect rather than a question that happens to be short.
			if want := popupBodyLineCount(m.th, req.Question, m.width); want < askQuestionFloor {
				t.Fatalf("the question wraps onto %d line(s) at 80 columns — test premise broken", want)
			}
			// Its first askQuestionFloor lines are on CONTENT rows, so the border is the unbroken one
			// the mockup draws and the marker — if the question owes one at all — is the last of them
			// rather than the whole of what the pane says about itself.
			question := popupBodyWrapped(m.th, req.Question, popupInnerWidth(m.th, m.width))
			for i := range askQuestionFloor {
				if text := strings.TrimSpace(strings.Trim(rows[1+i], "│")); text != strings.TrimSpace(question[i]) {
					t.Errorf("content row %d = %q, want line %d of the question (%q):\n%s",
						i+1, text, i+1, strings.TrimSpace(question[i]), got)
				}
			}
			if trimmed := strings.Trim(rows[0], "╭─╮"); trimmed != "" {
				t.Errorf("top border carries %q, want it unbroken where the question has its own rows:\n%s", trimmed, got)
			}
			// …and the answers are still there to be taken: the question's floor buys lines off the
			// offering's surplus, never off the decision itself.
			if seated := askAnswerLines(rows); seated == 0 {
				t.Errorf("no answer is on the screen:\n%s", got)
			}
		})
	}

	t.Run("a one-line question claims one line", func(t *testing.T) {
		m := modelWithOverlayRoomAt(t, 80, 24, Options{Workspace: "/ws/a"})
		short := domain.AskRequest{Question: "which way?", Choices: req.Choices}
		rows := strings.Split(ansiPattern.ReplaceAllString(m.askPrompt(short), ""), "\n")
		got := strings.Join(rows, "\n")

		if seated, want := askAnswerLines(rows), len(short.Choices); seated != want {
			t.Errorf("%d of %d answers on the screen, want the floor to have cost the offering nothing:\n%s", seated, want, got)
		}
		if sep := strings.TrimSpace(strings.Trim(rows[2], "│")); sep != "" {
			t.Errorf("the line under the question = %q, want the blank that sets the offering off:\n%s", sep, got)
		}
	})
}

// TestModelAskQuestionFloorGivesWayToTheAnswers is the other half of the floor, and the one that
// keeps it from being a new way to empty a decision surface. A window granted rows enough to seat one
// ANSWER must still seat it: the question's claim yields to the lines the offering needs for the row
// its window is anchored on (popupFloor.rows, askAnchorRowLines), because an answer is seated whole
// or not at all and a three-line answer needs all three.
//
// The reach it may not shorten is the one the budget had before the floor existed — rows first, one
// line kept back for the body — which is what the arithmetic below states: an answer was on the
// screen exactly where the pane's granted rows past its chrome covered that answer's height with the
// body's line still set aside. Where even that could not seat one, the pane owes its identity to the
// border instead (popupRowStyle.titleFromBody), and the floor does not change which case a height is in.
func TestModelAskQuestionFloorGivesWayToTheAnswers(t *testing.T) {
	const lead = "how should I continue"
	// One long answer, so the offering's irreducible claim is three lines rather than one — the case
	// where a floor taken off the top could have left the pane with no seatable answer at all.
	req := domain.AskRequest{
		Question: lead + " with this refactor of the resolution pipeline, now that the gate has moved?",
		Choices: []string{
			"implement the config redesign first, commit it, then do the TUI part in a separate " +
				"commit, and run make check after each — the config change is the riskier part",
			"no",
			"ask me again later",
		},
	}

	for height := smallestOverlayWindow; height <= 24; height++ {
		t.Run(fmt.Sprintf("80×%d", height), func(t *testing.T) {
			m := modelWithOverlayRoomAt(t, 80, height, Options{Workspace: "/ws/a"})
			pane := m.askPrompt(req)
			rows := strings.Split(ansiPattern.ReplaceAllString(pane, ""), "\n")
			got := strings.Join(rows, "\n")

			if h := lipgloss.Height(pane); h > m.viewport.Height() {
				t.Fatalf("pane is %d rows on a %d-row viewport: the input box goes off the frame\n%s",
					h, m.viewport.Height(), got)
			}

			answer := popupWrappedRowHeights(m.th, singleCellRows(req.Choices), m.width)[0]
			avail := m.frameRowPlan(m.openPanes().with(panePrompt)).panes[panePrompt] - popupTitleBorderChrome
			seated := askAnswerLines(rows)
			switch {
			case avail-1 >= answer:
				if seated == 0 {
					t.Errorf("no answer on the screen at %d rows, where %d line(s) past the pane's chrome "+
						"could seat a %d-line one:\n%s", height, avail, answer, got)
				}
			case seated == 0:
				// The window cannot pay for an answer either way, which is the case the border fallback
				// was added for: the pane still says what it is asking and counts what it is holding back.
				if !strings.Contains(rows[0], lead) || !elisionMarkerPattern.MatchString(rows[0]) {
					t.Errorf("top border = %q, want the question's lead and the count for the answers "+
						"the window seated none of:\n%s", rows[0], got)
				}
			}
		})
	}
}

// TestModelAskLongChoiceWrapsUnderItsMarker is what the schema's relaxed wording rests on: a choice
// may now be a whole sentence, so the pane BREAKS it instead of eliding it, and its continuation
// lines hang under the option's own first column rather than under the marker. A decision taken
// against "implement the config redesign first, commit it, then do the …" is a decision taken
// against half a sentence.
func TestModelAskLongChoiceWrapsUnderItsMarker(t *testing.T) {
	const long = "implement the config redesign first, commit it, then do the TUI part in a separate commit"
	rows := askPaneLines(t, 50, domain.AskRequest{
		Question: "how to continue?",
		Choices:  []string{"just do it all in one shot", long},
	})
	got := strings.Join(rows, "\n")

	head := paneRowIndex(t, rows, "· implement")
	var wrapped []string
	for _, row := range rows[head:] {
		text := strings.Trim(row, "│")
		if strings.TrimSpace(text) == "" || strings.HasPrefix(strings.TrimSpace(text), "↑↓") {
			break
		}
		if len(wrapped) > 0 && !strings.HasPrefix(text, " "+strings.Repeat(" ", popupRowIndent)) {
			t.Errorf("continuation line %q does not hang under the option's first column:\n%s", text, got)
		}
		wrapped = append(wrapped, strings.TrimSpace(text))
	}
	if len(wrapped) < 2 {
		t.Fatalf("the long option did not wrap at all (%d line(s)):\n%s", len(wrapped), got)
	}
	// The whole option, and nothing elided out of the middle of it: the rejoined lines ARE the string
	// the model offered.
	if rejoined := strings.TrimPrefix(strings.Join(wrapped, " "), "· "); rejoined != long {
		t.Errorf("the wrapped option reads %q, want the whole of %q:\n%s", rejoined, long, got)
	}
}

// paneRowIndex is the index of the pane line carrying want, failing the test when no line does.
func paneRowIndex(t *testing.T, rows []string, want string) int {
	t.Helper()
	for i, row := range rows {
		if strings.Contains(row, want) {
			return i
		}
	}
	t.Fatalf("no pane line carries %q:\n%s", want, strings.Join(rows, "\n"))
	return -1
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
	before := m.askSel.selected
	m = step(t, m, keyDown())
	m = step(t, m, keyUp())
	if m.askSel.selected != before {
		t.Errorf("askSel moved to %d while text was in the input; want unchanged %d", m.askSel.selected, before)
	}
}

// ----------------------------------------------------------------------------
// Multi-select answers — the ␣-toggled checked set (multi_select opt-in)
// ----------------------------------------------------------------------------

// newMultiAskModel drives a fresh model to awaitingAsk on a MULTI-SELECT question offering
// alpha/beta/gamma: the fixture every checked-set test below starts from.
func newMultiAskModel(t *testing.T) (Model, chan domain.AskAnswer) {
	t.Helper()
	return newAskModel(t, domain.AskRequest{
		Question:    "which ones?",
		Choices:     []string{"alpha", "beta", "gamma"},
		MultiSelect: true,
	})
}

// ␣ ticks the highlighted row and ticks it back off, follows the ↑/↓ highlight, and — the part
// that makes it a KEY rather than a character — never reaches the answer box.
func TestModelAskMultiSelectSpaceToggles(t *testing.T) {
	m, _ := newMultiAskModel(t)
	if len(m.askChecked) != 3 {
		t.Fatalf("askChecked = %v, want one slot per offered choice", m.askChecked)
	}

	m = step(t, m, keySpace())
	if !m.askChecked[0] {
		t.Errorf("space did not tick the highlighted row: %v", m.askChecked)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("box = %q, want it untouched — ␣ is the toggle key here, not a character", got)
	}

	m = step(t, m, keySpace())
	if m.askChecked[0] {
		t.Errorf("space did not un-tick the highlighted row: %v", m.askChecked)
	}

	m = step(t, m, keyDown())
	m = step(t, m, keySpace())
	if m.askChecked[0] || !m.askChecked[1] {
		t.Errorf("askChecked = %v, want the toggle to follow the highlight to row 1", m.askChecked)
	}
}

// ⏎ sends every ticked label, one per line, in the order the choices were OFFERED — asserted by
// ticking them out of order (gamma first, then alpha), because ticking order is exactly what a
// naive append-as-you-go implementation would leak into the wire format.
func TestModelAskMultiSelectSendsCheckedLabelsInChoiceOrder(t *testing.T) {
	m, reply := newMultiAskModel(t)

	m = step(t, m, keyDown())
	m = step(t, m, keyDown())
	m = step(t, m, keySpace()) // gamma
	m = step(t, m, keyUp())
	m = step(t, m, keyUp())
	m = step(t, m, keySpace()) // alpha

	m, _ = stepCmd(t, m, keyEnter())
	if got := takeAnswer(t, reply); got != "alpha\ngamma" {
		t.Errorf("answer = %q, want %q — newline-joined, in choice order", got, "alpha\ngamma")
	}
	if m.askChecked != nil {
		t.Errorf("askChecked = %v, want it cleared with the answered question", m.askChecked)
	}
}

// ⏎ with NOTHING ticked keeps today's single-select fast path as the degenerate case: the
// highlighted row alone, byte-identical to the reply a single-select question would have sent.
func TestModelAskMultiSelectWithNothingCheckedSendsTheHighlightedRow(t *testing.T) {
	m, reply := newMultiAskModel(t)

	m = step(t, m, keyDown())
	stepCmd(t, m, keyEnter())
	if got := takeAnswer(t, reply); got != "beta" {
		t.Errorf("answer = %q, want the highlighted label %q", got, "beta")
	}
}

// Typing a custom answer REPLACES the checks rather than joining them: ⏎ sends only the typed
// text. Deleting back to empty restores the offering with the checked set intact, so a stray
// keystroke never costs the human the ticks they had already made.
func TestModelAskMultiSelectFreeTextReplacesTheChecks(t *testing.T) {
	t.Run("typed text is the whole answer", func(t *testing.T) {
		m, reply := newMultiAskModel(t)
		m = step(t, m, keySpace()) // alpha ticked, then abandoned for free text
		m = typeInput(t, m, "neither")

		stepCmd(t, m, keyEnter())
		if got := takeAnswer(t, reply); got != "neither" {
			t.Errorf("answer = %q, want only the typed text %q", got, "neither")
		}
	})

	t.Run("deleting back to empty keeps the ticks", func(t *testing.T) {
		m, reply := newMultiAskModel(t)
		m = step(t, m, keySpace()) // alpha
		m = typeInput(t, m, "hmm")
		for range "hmm" {
			m = step(t, m, tea.KeyPressMsg{Code: tea.KeyBackspace})
		}

		if !m.askChecked[0] {
			t.Fatalf("askChecked = %v, want the tick to survive the abandoned free text", m.askChecked)
		}
		m, _ = stepCmd(t, m, keyEnter())
		if got := takeAnswer(t, reply); got != "alpha" {
			t.Errorf("answer = %q, want the surviving tick %q", got, "alpha")
		}
	})
}

// The regression pin for every question that did NOT opt in: on a single-select question ␣ is
// still a character, so it falls through to the borrowed box and opens a free-text answer, and no
// checked set is allocated at all.
func TestModelAskSingleSelectSpaceStillTypes(t *testing.T) {
	m, reply := newAskModel(t, domain.AskRequest{
		Question: "which one?",
		Choices:  []string{"alpha", "beta"},
	})
	if m.askChecked != nil {
		t.Errorf("askChecked = %v, want none on a single-select question", m.askChecked)
	}

	m = step(t, m, keySpace())
	if got := m.input.Value(); got != " " {
		t.Fatalf("box = %q, want the space typed into it as it always was", got)
	}

	m = typeInput(t, m, "x")
	m, _ = stepCmd(t, m, keyEnter())
	if got := takeAnswer(t, reply); got != "x" {
		t.Errorf("answer = %q, want the trimmed typed text %q", got, "x")
	}
}

// An Exchange that dies under a multi-select question takes the checked set with it — no dead set
// is left for the next question to inherit — and still hands the borrowed draft back (finishWorker
// owns both, and the second must not have been traded for the first).
func TestModelAskMultiSelectCancelClearsTheChecks(t *testing.T) {
	const draft = "the message the question interrupted"

	m := typeInput(t, newTestModel(t), draft)
	reply := make(chan domain.AskAnswer, 1)
	m = step(t, m, askReqMsg{Request: domain.AskRequest{
		Question:    "which ones?",
		Choices:     []string{"alpha", "beta"},
		MultiSelect: true,
	}, Reply: reply})
	m.cancel = func() {}

	m = step(t, m, keySpace())
	if !m.askChecked[0] {
		t.Fatalf("askChecked = %v, want the highlighted row ticked", m.askChecked)
	}

	m = step(t, m, keyEsc())
	m = step(t, m, cancelledMsg{Result: domain.StepResult{Status: domain.StatusCancelled}})
	if m.askChecked != nil {
		t.Errorf("askChecked = %v, want it cleared with the dead question", m.askChecked)
	}
	if got := m.input.Value(); got != draft {
		t.Errorf("box = %q, want the borrowed draft %q back", got, draft)
	}
}

// ----------------------------------------------------------------------------
// Multi-select rendering — the checkbox column and the toggle hint
// ----------------------------------------------------------------------------

// askChoiceLines are the pane lines carrying an option — the ones led by the menu's pointer or its
// dim dot — with the border and the box's own padding trimmed off but the content's indentation
// kept, because where a wrapped label hangs is exactly what these assertions are about.
func askChoiceLines(rows []string) []string {
	var out []string
	for _, row := range rows {
		content := popupContent(row)
		if strings.HasPrefix(content, glyphUser) || strings.HasPrefix(content, glyphMenuUnselected) {
			out = append(out, content)
		}
	}
	return out
}

// A multi-select question draws a checkbox in front of every option, in a column of its own: the
// boxes start at one offset down the pane whichever row is pointed at, and the pointer/dot marker,
// the labels and the spacing around them are the menu style's unchanged.
func TestModelAskMultiSelectRendersACheckboxColumn(t *testing.T) {
	rows := askPaneLines(t, 60, domain.AskRequest{
		Question:    "which ones?",
		Choices:     []string{"alpha", "beta", "gamma"},
		MultiSelect: true,
	})
	got := strings.Join(rows, "\n")

	choices := askChoiceLines(rows)
	want := []string{
		glyphUser + " " + askUncheckedMarker + "  alpha",
		glyphMenuUnselected + " " + askUncheckedMarker + "  beta",
		glyphMenuUnselected + " " + askUncheckedMarker + "  gamma",
	}
	if len(choices) != len(want) {
		t.Fatalf("pane draws %d option lines, want %d:\n%s", len(choices), len(want), got)
	}
	for i, line := range choices {
		if line != want[i] {
			t.Errorf("option line %d = %q, want %q:\n%s", i, line, want[i], got)
		}
	}

	// In DISPLAY cells, not bytes: ❯ is three bytes and · two, so a byte offset would report the
	// pointed-at row a column off from the rows below it and call an aligned pane broken.
	for i, line := range choices {
		box := lipgloss.Width(line[:strings.Index(line, askUncheckedMarker)])
		first := lipgloss.Width(choices[0][:strings.Index(choices[0], askUncheckedMarker)])
		if box != first {
			t.Errorf("row %d's checkbox starts at cell %d, want %d (the column the first row opened):\n%s", i, box, first, got)
		}
	}
}

// ␣ repaints the marker of the row it ticked and leaves every other cell of the pane where it was:
// the checked set is state the rendering reads, not a second layout.
func TestModelAskMultiSelectToggleRepaintsTheMarker(t *testing.T) {
	m, _ := newMultiAskModel(t)
	before := askModelPaneLines(t, m)

	m = step(t, m, keySpace())
	after := askModelPaneLines(t, m)

	if len(before) != len(after) {
		t.Fatalf("the pane changed height on a toggle (%d → %d lines):\n%s", len(before), len(after), strings.Join(after, "\n"))
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
			if !strings.Contains(after[i], askCheckedMarker+"  alpha") {
				t.Errorf("line %d changed to %q, want the ticked highlighted row", i, after[i])
			}
		}
	}
	if changed != 1 {
		t.Errorf("a toggle repainted %d lines, want exactly the ticked row's:\nbefore:\n%s\n\nafter:\n%s",
			changed, strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
}

// The menu's own cues survive the extra column: the pointed-at row is lit in the accent and the rest
// stay faint dots, exactly as they are on a single-select question. The checkbox says what is TICKED
// and the accent says what ⏎ and ␣ act on — two different facts, and the second must not be lost to
// the first.
func TestModelAskMultiSelectKeepsTheMenuStyling(t *testing.T) {
	m, _ := newMultiAskModel(t)
	if !colorActive(m.th) {
		t.Skip("no ANSI styling in this environment")
	}
	pane := m.askPrompt(m.pendingAsk.Request)

	for _, line := range popupLines(pane) {
		content := popupContent(line)
		switch {
		case strings.HasPrefix(content, glyphUser):
			if sgr := styleSGR(m.th.popupAccent); !strings.Contains(line, sgr) {
				t.Errorf("the pointed-at option carries no accent SGR %q: %q", sgr, content)
			}
		case strings.HasPrefix(content, glyphMenuUnselected):
			if sgr := styleSGR(m.th.statusFaint); !strings.Contains(line, sgr) {
				t.Errorf("an unpicked option is not faint (%q): %q", sgr, content)
			}
		}
	}
	if sgr := styleSGR(m.th.userBlock); strings.Contains(pane, sgr) {
		t.Errorf("a checkbox row carries the highlight bar %q the menu style exists to avoid", sgr)
	}
}

// A label too long for the pane hangs its continuation lines under the LABEL, not under the box: the
// checkbox column stays a column, and one option still reads as one block of text. This is the
// columned half of TestModelAskLongChoiceWrapsUnderItsMarker.
func TestModelAskMultiSelectLongChoiceWrapsUnderItsLabel(t *testing.T) {
	const long = "implement the config redesign first, commit it, then do the TUI part in a separate commit"
	rows := askPaneLines(t, 50, domain.AskRequest{
		Question:    "which ones?",
		Choices:     []string{"just do it all in one shot", long},
		MultiSelect: true,
	})
	got := strings.Join(rows, "\n")

	head := paneRowIndex(t, rows, glyphMenuUnselected+" "+askUncheckedMarker+"  implement")
	indent := popupRowIndent + lipgloss.Width(askUncheckedMarker+popupGutter)
	wrapped := []string{strings.TrimSpace(strings.TrimPrefix(popupContent(rows[head]), glyphMenuUnselected+" "+askUncheckedMarker))}
	for _, row := range rows[head+1:] {
		content := popupContent(row)
		if strings.TrimSpace(content) == "" || strings.HasPrefix(strings.TrimSpace(content), "↑↓") {
			break
		}
		if !strings.HasPrefix(content, strings.Repeat(" ", indent)) || strings.HasPrefix(content, strings.Repeat(" ", indent+1)) {
			t.Errorf("continuation line %q does not hang at %d cells, under the label's own column:\n%s", content, indent, got)
		}
		wrapped = append(wrapped, strings.TrimSpace(content))
	}
	if len(wrapped) < 2 {
		t.Fatalf("the long option did not wrap at all:\n%s", got)
	}
	// The whole option, and nothing elided out of the middle of it.
	if rejoined := strings.Join(wrapped, " "); rejoined != long {
		t.Errorf("the wrapped option reads %q, want the whole of %q:\n%s", rejoined, long, got)
	}
}

// The hint names ␣ among the live keys on a multi-select question and is the single-select legend
// word for word otherwise; on a pane too narrow to seat it, it elides through the same truncation
// every other pane line takes rather than wrapping the box.
func TestModelAskMultiSelectHintNamesTheToggle(t *testing.T) {
	const want = "↑↓ select · ␣ toggle · ⏎ send · type for a custom answer · esc cancel"
	req := domain.AskRequest{
		Question:    "which ones?",
		Choices:     []string{"alpha", "beta"},
		MultiSelect: true,
	}

	rows := askPaneLines(t, 100, req)
	hint := popupContent(rows[len(rows)-2]) // the row above the bottom border
	if hint != want {
		t.Errorf("multi-select hint = %q, want the pinned %q", hint, want)
	}

	single := askPaneLines(t, 100, domain.AskRequest{Question: req.Question, Choices: req.Choices})
	if got := popupContent(single[len(single)-2]); got != "↑↓ select · ⏎ send · type for a custom answer · esc cancel" {
		t.Errorf("single-select hint = %q, want it unchanged by multi-select", got)
	}

	narrow := askPaneLines(t, narrowOverlayWindow, req)
	short := popupContent(narrow[len(narrow)-2])
	if !strings.HasSuffix(short, "…") {
		t.Errorf("narrow hint = %q, want it elided rather than dropped or wrapped", short)
	}
	for i, line := range narrow {
		if w := lipgloss.Width(line); w != narrowOverlayWindow {
			t.Errorf("line %d is %d cells, want the pane's %d: %q", i, w, narrowOverlayWindow, line)
		}
	}
}

// The regression pin for every question that did NOT opt in: a single-select offering is composed of
// the very rows it was composed of before multi-select existed (singleCellRows), so its pane is the
// byte-identical one — no marker column, no checkbox anywhere in it.
func TestModelAskSingleSelectRenderIsUnchanged(t *testing.T) {
	labels := []string{"alpha", "beta", "gamma"}
	if got, want := askChoiceRows(labels, false, []bool{true, true, true}), singleCellRows(labels); !reflect.DeepEqual(got, want) {
		t.Errorf("single-select rows = %v, want the plain one-cell labels %v", got, want)
	}

	rows := askPaneLines(t, 60, domain.AskRequest{Question: "which one?", Choices: labels})
	got := strings.Join(rows, "\n")
	if strings.Contains(got, askUncheckedMarker) || strings.Contains(got, askCheckedMarker) {
		t.Errorf("a single-select pane drew a checkbox:\n%s", got)
	}
	for i, line := range askChoiceLines(rows) {
		want := glyphMenuUnselected + " " + labels[i]
		if i == 0 {
			want = glyphUser + " " + labels[i]
		}
		if line != want {
			t.Errorf("option line %d = %q, want %q:\n%s", i, line, want, got)
		}
	}
}

// The question and choices are escape-stripped before rendering, so a model-authored ESC byte
// never reaches the terminal (D8, hardening). stripEscapes drops the control characters and keeps
// every printable rune, so the ESC goes and the rest of the sequence stays behind as INERT literal
// text ("[31mred"); had the ESC survived, plain() would have consumed the whole "\x1b[31m" as a
// real SGR sequence and the literal would be gone — so its presence in the stripped View is
// exactly the proof the strip happened at the call site.
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
	pending := expandBatch(cmd)
	for range maxWriteDrainSteps {
		if len(pending) == 0 {
			return m
		}
		var next tea.Cmd
		m, next = stepCmd(t, m, pending[0])
		pending = append(pending[1:], expandBatch(next)...)
	}
	t.Fatal("the record-write queue never settled")
	return m
}

// expandBatch runs cmd and returns the Msgs the RUNTIME would deliver from it: a tea.BatchMsg is
// flattened into its members' Msgs, recursively, and a nil Cmd or a Cmd that answers nil yields
// none. Update has no tea.BatchMsg case (foldWidgetMsg is the end of the switch), because the
// batch never reaches a model in the real program — the runtime runs each member itself. A driver
// that stepped the BatchMsg would therefore silently drop everything inside it, which is exactly
// what a record write batched with anything else looks like when it goes missing.
func expandBatch(cmd tea.Cmd) []tea.Msg {
	msg := cmdMsg(cmd)
	batch, batched := msg.(tea.BatchMsg)
	if !batched {
		if msg == nil {
			return nil
		}
		return []tea.Msg{msg}
	}
	var msgs []tea.Msg
	for _, member := range batch {
		msgs = append(msgs, expandBatch(member)...)
	}
	return msgs
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
	pending := expandBatch(cmd)
	for range maxWriteDrainSteps {
		if len(pending) == 0 {
			t.Fatal("the record-write queue settled without firing the deferred quit")
		}
		msg := pending[0]
		if _, quit := msg.(tea.QuitMsg); quit {
			return m
		}
		var next tea.Cmd
		m, next = stepCmd(t, m, msg)
		pending = append(pending[1:], expandBatch(next)...)
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

// TestNewModelReplaysARunnerWrittenScrollback is the blank-replay gap closed from the reading
// end: a blob the RUNNER wrote (internal/run's fold — stream facts only, no presenter verdicts)
// replays as real entries, and the "no scrollback recorded" degrade note stops firing for it.
// The entries are built as session.Entry values and encoded with session.EncodeTranscript rather
// than through this package's own transcript, because a runner-written record never passes
// through a TUI transcript at all — that is the whole point of the neutral codec.
func TestNewModelReplaysARunnerWrittenScrollback(t *testing.T) {
	t.Parallel()

	blob, err := session.EncodeTranscript([]session.Entry{
		{Kind: session.EntryKindUser, Text: "check the build"},
		{
			Kind:   session.EntryKindToolCall,
			CallID: "call_1",
			Done:   true,
			Tool:   &session.ToolView{Name: "note_something", Args: json.RawMessage(`{"note":"hello"}`)},
		},
		{Kind: session.EntryKindToolResult, Text: "noted: hello"},
		{Kind: session.EntryKindAssistant, Text: "the build is green", Depth: 1, SpawnCallID: "call_1"},
	})
	if err != nil {
		t.Fatalf("EncodeTranscript: %v", err)
	}

	m := newModel(context.Background(), &fakeEngine{}, Options{
		Resumed: &ResumedSession{Transcript: blob, Title: "check the build"},
	}, nil)

	if !hasEntry(m, entryUser, "check the build") {
		t.Error("the runner's user entry did not replay")
	}
	if !hasEntry(m, entryToolResult, "noted: hello") {
		t.Error("the runner's tool result did not replay")
	}
	if !hasEntry(m, entryAssistant, "the build is green") {
		t.Error("the runner's delegated assistant text did not replay")
	}
	for _, e := range m.transcript.entries {
		if strings.Contains(e.text, "no scrollback recorded — the model still remembers") {
			t.Fatalf("the degrade note still fires on a runner-written blob: %q", e.text)
		}
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
	if footer := ansiPattern.ReplaceAllString(m.footerContent(m.width), ""); strings.Contains(footer, format.Tokens(opts.ContextWindow)) {
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
			m := modelWithOverlayRoomAt(t, 80, 24,
				Options{Workspace: "/ws/a", Server: &fakeServerHost{list: staticServers(servers)}})
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

	// Each Event re-derives the phrase: streamed text, then the verb of the tool that is running.
	// The tool's TARGET stays off the row — the call's own block already names it, and the path is
	// what used to push the context gauge off the line.
	m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "hi"}})
	if got := statusText(t, m); !strings.Contains(got, "responding") {
		t.Errorf("status line while streaming = %q, want it to contain %q", got, "responding")
	}
	m = step(t, m, eventMsg{Event: domain.ToolCallEvent{
		Call: domain.ToolCall{ID: "1", Tool: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
	}})
	if got := statusText(t, m); !strings.Contains(got, "reading") {
		t.Errorf("status line during a tool call = %q, want it to name what the tool is doing", got)
	}
	if got := statusText(t, m); strings.Contains(got, "main.go") {
		t.Errorf("status line during a tool call = %q, want it to leave the target to the call's block", got)
	}

	// Esc×2 registers the stop, and the phrase stays until the worker's terminal Msg unwinds it.
	m = step(t, m, keyEsc())
	m = step(t, m, keyEsc())
	if got := statusText(t, m); !strings.Contains(got, "stopping") {
		t.Errorf("status line after esc×2 = %q, want it to contain %q", got, "stopping")
	}
	m = step(t, m, cancelledMsg{})
	if got := statusText(t, m); got != "" {
		t.Errorf("status line after the worker unwound = %q, want the idle empty slot", got)
	}
}

// guardedRunningModel is a model with a worker running and the stall guard armed at after —
// "thinking", the request away and nothing back yet, which is the shape the incident wore.
func guardedRunningModel(t *testing.T, after time.Duration) Model {
	t.Helper()
	m := newTestModel(t)
	m.opts.StallAfter = after
	m.input.SetValue("hello")
	return step(t, m, keyEnter())
}

// silentFor backdates BOTH of the model's clocks by quiet: the last-event clock, so the engine
// reads as having said nothing for that long, and the activity's own start, because the stall the
// guard was built for is ONE span and not two — nothing has arrived since the phrase went up, which
// is exactly why the row shows a single clock. The silence is arranged rather than waited out, so
// the row is asserted in no time at all. Callers set the activity FIRST and backdate second: moving
// the activity is itself the engine being heard from, and would restamp both clocks this hands back.
func silentFor(m Model, quiet time.Duration) Model {
	m.lastEvent = time.Now().Add(-quiet)
	backdateActivity(&m, m.lastEvent)
	return m
}

// TestStatusLineQuietSuffix proves the stall guard on the row it actually paints. The status line
// used to claim "thinking" for twenty minutes with nothing behind it (2026-08-14); it now qualifies
// the phrase with a bare `quiet` in front of the activity's own clock once the engine has been
// silent past `ui.stall-after` — the fact that nothing is arriving, never a verdict about it — and
// takes the qualifier straight back off the moment an Event lands. There is ONE clock on the row:
// in the shape the guard was built for the silence and the activity are the same span, so a second
// duration behind the word only stated the first one twice. The states that are waiting on the
// HUMAN never show it: the silence there is the human's own, and telling them the engine is quiet
// while it waits for their answer is the same lie in the other direction.
func TestStatusLineQuietSuffix(t *testing.T) {
	const after = 90 * time.Second
	// A gap that renders unambiguously either side of a second's slack: "3m 10s".
	const quiet = 190*time.Second + 500*time.Millisecond

	t.Run("below the threshold the row says nothing about it", func(t *testing.T) {
		m := silentFor(guardedRunningModel(t, after), 89*time.Second)
		got := statusText(t, m)
		if strings.Contains(got, "quiet") {
			t.Errorf("status line = %q, want no quiet qualifier below the threshold", got)
		}
		if !strings.Contains(got, "thinking") {
			t.Errorf("status line = %q, want the running phrase to stand alone", got)
		}
	})

	t.Run("past the threshold the phrase gains the qualifier", func(t *testing.T) {
		m := silentFor(guardedRunningModel(t, after), quiet)
		got := statusText(t, m)
		if want := "thinking · quiet · 3m 10s"; !strings.Contains(got, want) {
			t.Errorf("status line = %q, want it to contain %q", got, want)
		}
		// The word carries no clock of its own: the one duration on the row is the activity's.
		if strings.Contains(got, "quiet 3m 10s") {
			t.Errorf("status line = %q, want no second clock hung off the qualifier", got)
		}
		// The fact is a QUALIFIER on a running phrase, so it carries the amber warning tint rather
		// than the errored state's red or the bar's own plain field (theme.go)...
		if tinted := m.th.statusWarning.Render(quietQualifier); !strings.Contains(m.statusLine(), tinted) {
			t.Errorf("status line does not carry the qualifier under statusWarning: %q", m.statusLine())
		}
		// ...and the tint stops there: the clock is the activity's, not the guard's, so it stays on
		// the bar's own field rather than inside the amber run.
		if tinted := m.th.statusWarning.Render(quietQualifier + " · 3m 10s"); strings.Contains(m.statusLine(), tinted) {
			t.Errorf("status line paints the clock inside the warning tint: %q", m.statusLine())
		}
	})

	t.Run("an arriving event takes it straight back off", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			event domain.Event
		}{
			// A reasoning stream is life: the incident's signature was NO events, not silent ones.
			{name: "reasoning", event: domain.ReasoningEvent{Text: "still working"}},
			{name: "streamed text", event: domain.TokenEvent{Text: "so"}},
			// Accounting moves no phrase (foldActivity ignores it) and still proves the engine alive.
			{name: "usage", event: domain.UsageEvent{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}},
			// A nested agent's event is the engine speaking too, whatever depth it speaks from.
			{name: "a sub-agent's token", event: domain.TokenEvent{EventBase: domain.EventBase{Depth: 1, CallID: "s1"}, Text: "sub"}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := silentFor(guardedRunningModel(t, after), quiet)
				if got := statusText(t, m); !strings.Contains(got, "quiet") {
					t.Fatalf("status line = %q, want the qualifier before the event lands", got)
				}

				m = step(t, m, eventMsg{Event: tc.event})

				if got := statusText(t, m); strings.Contains(got, "quiet") {
					t.Errorf("status line = %q, want the qualifier gone the moment an event arrives", got)
				}
			})
		}
	})

	// The pin on what the qualifier MEANS: it reports the absence of events, never the length of
	// the work. A model actually emitting thinking tokens is the loudest thing on the wire, so a
	// turn that streams reasoning for many multiples of `ui.stall-after` must never surface the
	// word at any point in it — only a genuine gap longer than the threshold may. The sub-test
	// above proves one arriving Event takes an owed qualifier back off; this one proves the
	// sustained case, where the row's single clock counts far past the threshold while the
	// qualifier never appears at all, because no single gap between chunks ever crosses it.
	t.Run("a streaming reasoning channel never surfaces it", func(t *testing.T) {
		// Chunks a shade under the threshold apart: the tightest stream that still never trips the
		// guard, over a turn eight of those gaps long (~11m 52s, near eight times `after`).
		const gap = after - time.Second
		const rounds = 8

		m := guardedRunningModel(t, after)

		for round := 1; round <= rounds; round++ {
			streamed := time.Duration(round) * gap

			// Nothing heard since the last chunk, on an activity that has been thinking since the
			// turn began — silentFor arranges the one-span shape, and the activity's own clock is
			// then pushed the rest of the way back to that start.
			m = silentFor(m, gap)
			backdateActivity(&m, time.Now().Add(-streamed))

			got := statusText(t, m)
			if strings.Contains(got, "quiet") {
				t.Fatalf("status line %s into a streamed turn = %q, want no qualifier while thinking tokens keep arriving", streamed, got)
			}
			if !strings.Contains(got, "thinking") {
				t.Fatalf("status line %s into a streamed turn = %q, want the running phrase on its own clock", streamed, got)
			}

			// The next chunk lands, restamping the silence clock as every Event does.
			m = step(t, m, eventMsg{Event: domain.ReasoningEvent{Text: "still reasoning"}})

			if got := statusText(t, m); strings.Contains(got, "quiet") {
				t.Fatalf("status line after a thinking chunk %s in = %q, want no qualifier", streamed, got)
			}
		}
	})

	t.Run("a running tool call never shows it", func(t *testing.T) {
		m := guardedRunningModel(t, after)
		m = step(t, m, eventMsg{Event: domain.ToolCallEvent{
			Call: domain.ToolCall{ID: "1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)},
		}})
		m = silentFor(m, quiet)

		got := statusText(t, m)
		if strings.Contains(got, "quiet") {
			t.Errorf("status line = %q, want no qualifier — a long silent tool call is normal", got)
		}
		if !strings.Contains(got, "running") {
			t.Errorf("status line = %q, want the tool's own phrase", got)
		}
	})

	t.Run("a stopping worker never shows it", func(t *testing.T) {
		m := step(t, step(t, guardedRunningModel(t, after), keyEsc()), keyEsc()) // esc×2 fired the cancel; the worker unwinds
		m = silentFor(m, quiet)

		got := statusText(t, m)
		if strings.Contains(got, "quiet") {
			t.Errorf("status line = %q, want no qualifier while the stop is in flight", got)
		}
		if !strings.Contains(got, "stopping") {
			t.Errorf("status line = %q, want the sticky stop phrase", got)
		}
	})

	t.Run("a state waiting on the human never shows it", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			state uiState
			want  string
		}{
			{name: "an open question", state: stateAwaitingAsk, want: "answer needed"},
			{name: "an open approval", state: stateAwaitingApproval, want: "approval needed"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				m := guardedRunningModel(t, after)
				// The gate is an open tool call the human has not answered — the incident's corrected
				// shape: it completed into a question nobody was at the screen for.
				m = step(t, m, eventMsg{Event: domain.ToolCallEvent{
					Call: domain.ToolCall{ID: "1", Tool: "ask_user", Arguments: []byte(`{"question":"which one?"}`)},
				}})
				m.state = tc.state
				m = silentFor(m, quiet)

				got := statusText(t, m)
				if strings.Contains(got, "quiet") {
					t.Errorf("status line = %q, want no qualifier while the wait is the human's own", got)
				}
				if !strings.Contains(got, tc.want) {
					t.Errorf("status line = %q, want it to contain %q", got, tc.want)
				}
			})
		}
	})

	t.Run("a fresh exchange never inherits the last one's silence", func(t *testing.T) {
		m := silentFor(guardedRunningModel(t, after), quiet)
		if got := statusText(t, m); !strings.Contains(got, "quiet") {
			t.Fatalf("status line = %q, want the qualifier on the exchange that went quiet", got)
		}

		m = step(t, m, cancelledMsg{}) // the worker unwound; the slot goes idle
		m.input.SetValue("again")
		m = step(t, m, keyEnter()) // and a new request is away

		if got := statusText(t, m); strings.Contains(got, "quiet") {
			t.Errorf("status line = %q, want the new exchange's clock to start at its own launch", got)
		}
	})

	t.Run("the guard turned off never shows it", func(t *testing.T) {
		m := silentFor(guardedRunningModel(t, 0), 20*time.Minute)
		if got := statusText(t, m); strings.Contains(got, "quiet") {
			t.Errorf("status line = %q, want nothing at all with ui.stall-after: 0", got)
		}
	})
}

// TestStatusLineQuietSuffixGivesWayFirst proves the qualifier is paid for out of the row's own
// width like every other occupant, and that it is the FIRST thing the slot gives up: a window too
// narrow for both keeps the phrase and its clock whole and drops the word rather than truncating it
// into "· quie…", which would report neither the silence nor its own word (layout.md).
func TestStatusLineQuietSuffixGivesWayFirst(t *testing.T) {
	m := silentFor(guardedRunningModel(t, 90*time.Second), 190*time.Second+500*time.Millisecond)
	if got := statusText(t, m); !strings.Contains(got, "thinking · quiet · 3m 10s") {
		t.Fatalf("status line = %q, want the qualifier painted at a full-width window", got)
	}
	if got := ansi.StringWidth(m.statusLine()); got != m.width {
		t.Errorf("status line renders %d columns, want exactly %d (the qualifier rides inside the width)", got, m.width)
	}

	m = step(t, m, tea.WindowSizeMsg{Width: 24, Height: 24})

	got := statusText(t, m)
	if strings.Contains(got, "quiet") {
		t.Errorf("status line at 24 columns = %q, want the qualifier dropped whole", got)
	}
	if !strings.Contains(got, "thinking · 3m 10s") {
		t.Errorf("status line at 24 columns = %q, want the phrase and its clock kept intact", got)
	}
	if w := ansi.StringWidth(m.statusLine()); w != m.width {
		t.Errorf("status line renders %d columns, want exactly %d", w, m.width)
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
	body := renderEntryLines(newTheme(scheme.Default()), paintInput{kind: entryAssistant, text: "alpha beta"}, wrapWidth, false).lines
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

		if width == 0 {
			if got := ansi.StringWidth(m.statusLine()); got > 0 {
				t.Errorf("status line at width 0 renders %d columns; want nothing", got)
			}
			continue
		}
		if got := ansi.StringWidth(m.statusLine()); got != m.width {
			t.Errorf("status line at width %d renders %d columns; want exactly %d", width, got, m.width)
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

// gaugeMarks are the glyphs only the context gauge puts on the status line: the per-cent sign of
// its numeric prefix, the full block of a filled cell, and every partial-cell eighth. A row holding
// any of them still shows the gauge.
var gaugeMarks = "%\u2588" + string(gaugeEighths)

// TestStatusLineDroppedRightSlotKeepsTheField proves the band survives the drop: at a window too
// narrow to seat the right slot the gauge goes, but the black field still runs to the last column.
// The pre-fix code returned the truncated left slot bare, so every column past it was padded later
// by the frame with the terminal's default background and the band broke exactly where the gauge
// would have sat — contradicting layout.md's "the black field runs past it to the edge regardless".
func TestStatusLineDroppedRightSlotKeepsTheField(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("hello")
	m = step(t, m, keyEnter()) // running, so the gauge displaces a hint that would otherwise show
	m = step(t, m, eventMsg{Event: domain.UsageEvent{PromptTokens: 1000, CompletionTokens: 200, TotalTokens: 1200}})
	if m.contextGauge() == "" {
		t.Fatal("context gauge unlit after usage: nothing in the right slot to drop")
	}

	widths := []int{3, 10, 12, widestDroppedGauge(t, m)}

	for _, width := range widths {
		narrow := step(t, m, tea.WindowSizeMsg{Width: width, Height: 24})
		line := narrow.statusLine()

		if got := ansi.StringWidth(line); got != narrow.width {
			t.Errorf("status line at width %d renders %d columns, want exactly %d", width, got, narrow.width)
		}
		if cells := statusCells(t, narrow); strings.ContainsAny(cells, gaugeMarks) {
			t.Errorf("status line at width %d = %q, want the gauge dropped whole", width, cells)
		}
		if col, ok := firstCellWithoutBackground(line); !ok {
			t.Errorf("status line at width %d has a bare (no-background) cell at column %d: %q",
				width, col, statusCells(t, narrow))
		}
	}
}

// widestDroppedGauge reports the widest window at which the right slot still does not fit and the
// gauge is dropped — the last column before the band's two-slot layout takes over, and the width
// the bare-return bug showed at most plainly. It scans upward rather than recomputing the slot
// arithmetic, so it follows the real composition wherever that moves.
func widestDroppedGauge(t *testing.T, m Model) int {
	t.Helper()
	widest := 0
	for width := 1; width <= 200; width++ {
		if strings.ContainsAny(statusCells(t, step(t, m, tea.WindowSizeMsg{Width: width, Height: 24})), gaugeMarks) {
			break
		}
		widest = width
	}
	if widest == 0 {
		t.Fatal("the gauge is painted at every width: no dropped-slot width to assert on")
	}
	return widest
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
		assertStatusRightTail(t, m, "esc×2 stop"+bodyIndent)
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
	m.transcript.commitAssistant(strings.Repeat("filler above. ", 80), runRef{})
	m.transcript.addUser("STICKY-PROMPT", nil)
	for i := 0; i < 30; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
	}
	m.refreshViewport()

	m.detached = true
	m.viewport.SetYOffset(5) // up in the history, well off the bottom
	off := m.viewport.YOffset()

	m.transcript.commitAssistant("more streamed content", runRef{})
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
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
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
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
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
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
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
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
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

// TestTranscriptTailReachableWithAPaneOpen is the pop-up occlusion guard: with a pane over the
// transcript, the LAST content line must still be reachable by scrolling. The viewport clamps every
// scroll to total − Height(), so while layout() fed the widget the whole transcriptBudget — the
// overlay-blind number — exactly the pane's height of tail lines sat below the clamp at every
// offset: unreachable by PgDn, by the wheel and by the block cursor, with AtBottom reporting "at
// the bottom" over content nobody could see. The widget now carries the DRAWN height
// ([Model.transcriptRows]), which puts the clamp and the paint on the same row.
func TestTranscriptTailReachableWithAPaneOpen(t *testing.T) {
	m, _ := newAskModel(t, domain.AskRequest{Question: "which way?", Choices: []string{"left", "right"}})
	m.transcript.addUser("a question", nil)
	for i := range 40 { // deeper than the drawn rows, so there is a tail to strand
		m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), runRef{})
	}
	m.refreshViewport()

	drawn, budget := m.transcriptRows(), m.transcriptBudget()
	if drawn >= budget {
		t.Fatalf("setup: the ask prompt took no rows off the transcript (drawn %d, budget %d)", drawn, budget)
	}
	total := m.viewport.TotalLineCount()
	if total <= drawn {
		t.Fatalf("setup: %d transcript lines over %d drawn rows — nothing to scroll", total, drawn)
	}

	// Off the tail and back down it, the human's own way: PgUp detaches, then PgDn until the view
	// stops moving — the clamp is what ends the loop, not a press count.
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if !m.detached {
		t.Fatalf("precondition: PgUp did not detach (offset %d of %d lines)", m.viewport.YOffset(), total)
	}
	for i := 0; i <= total; i++ {
		before := m.viewport.YOffset()
		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})
		if m.viewport.YOffset() == before {
			break
		}
		if i == total {
			t.Fatalf("PgDn still moving after %d presses over %d lines", i, total)
		}
	}

	if painted := m.viewport.YOffset() + drawn; painted < total {
		t.Errorf("at maximum scroll the transcript paints through line %d of %d — the last %d are "+
			"stranded under the pane (offset %d, drawn %d)",
			painted, total, total-painted, m.viewport.YOffset(), drawn)
	}
	if !m.viewport.AtBottom() {
		t.Errorf("the viewport does not report AtBottom at maximum scroll (offset %d of %d lines)",
			m.viewport.YOffset(), total)
	}
	if m.detached {
		t.Error("detached is still set at the tail: what is on screen and what the Model believes disagree")
	}

	// And on the really-composed frame: the last committed line is on it.
	if last, rows := "reply line 39", transcriptRows(t, m); !strings.Contains(strings.Join(rows, "\n"), last) {
		t.Errorf("the last transcript line %q is not painted at maximum scroll:\n%s", last, strings.Join(rows, "\n"))
	}
}

// paneOverTailModel is the occlusion fixture the symptom guards below share: an ask prompt over the
// transcript and more content than the rows the prompt leaves, so there is a tail to strand and a
// bar with a position to describe. The prompt is the pane because it is the one the defect was
// reported against — a question the human must answer while reading the last replies.
func paneOverTailModel(t *testing.T) Model {
	t.Helper()

	m, _ := newAskModel(t, domain.AskRequest{Question: "which way?", Choices: []string{"left", "right"}})
	m.transcript.addUser("a question", nil)
	for i := range 40 { // deeper than the drawn rows, so the tail overflows them several times over
		m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), runRef{})
	}
	m.refreshViewport() // follows the tail: the view opens at maximum scroll, attached

	drawn, budget := m.transcriptRows(), m.transcriptBudget()
	if drawn >= budget {
		t.Fatalf("setup: the ask prompt took no rows off the transcript (drawn %d, budget %d)", drawn, budget)
	}
	if total := m.viewport.TotalLineCount(); total < 2*drawn {
		t.Fatalf("setup: %d transcript lines over %d drawn rows — too short to page through", total, drawn)
	}
	return m
}

// TestScrollbarThumbSeatsAtTheBottomWithAPaneOpen is the occlusion defect's second face. The gutter
// places its thumb from the same window the clamp holds (scrollbarThumb, boxdraw.go), so while the
// widget carried the overlay-blind budget the thumb stopped short of its track at maximum scroll:
// the bar said "there is more below" over a transcript with nothing left to give. Nothing in
// renderScrollbar changed for it — the seat falls out of the clamp, and this pins that it does.
func TestScrollbarThumbSeatsAtTheBottomWithAPaneOpen(t *testing.T) {
	m := paneOverTailModel(t)
	if m.opts.HideScrollbar {
		t.Fatal("setup: the scroll bar is switched off, so there is no thumb to place")
	}

	rows := transcriptRows(t, m)
	last := []rune(rows[len(rows)-1])
	if len(last) == 0 {
		t.Fatal("the transcript's last drawn row is empty — it carries no scroll-bar cell")
	}

	if got := string(last[len(last)-1]); got != glyphScrollThumb {
		t.Errorf("the bar's last line carries %q, want the thumb %q: the thumb does not reach the "+
			"bottom of its track at maximum scroll (offset %d of %d lines over %d drawn rows)",
			got, glyphScrollThumb, m.viewport.YOffset(), m.viewport.TotalLineCount(), m.transcriptRows())
	}
}

// TestPageDownAdvancesOneDrawnScreenfulWithAPaneOpen is the third face: PgDn scrolls by the widget's
// own Height() (viewport.PageDown), so the overlay-blind height stepped by a screenful PLUS the open
// pane and jumped clean over the lines in between — a page key that skips what it never showed. The
// step is now exactly the rows the frame draws, which is what makes one press one screenful.
func TestPageDownAdvancesOneDrawnScreenfulWithAPaneOpen(t *testing.T) {
	m := paneOverTailModel(t)
	drawn, total := m.transcriptRows(), m.viewport.TotalLineCount()

	// To the top the human's own way, so the press below pages through content rather than into the
	// clamp; the loop ends on the top, not on a press count.
	for i := 0; !m.viewport.AtTop(); i++ {
		if i > total {
			t.Fatalf("PgUp still moving after %d presses over %d lines", i, total)
		}
		m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	}

	top := m.viewport.YOffset()
	m = step(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown})

	if got := m.viewport.YOffset() - top; got != drawn {
		t.Errorf("PgDn advanced %d lines, want the %d rows the frame draws — the press skips %d "+
			"lines the human never saw", got, drawn, got-drawn)
	}
}

// ----------------------------------------------------------------------------
// Pane-height freshness — every change to a pane's DRAWN height reaches layout()
// ----------------------------------------------------------------------------

// The widget's height IS the transcript's drawn row count (layout(), model.go), so the number goes
// stale the moment an open pane is drawn at a different height without laying out again — and a
// stale one is the pop-up occlusion defect back in miniature: the scroll clamp holds back rows the
// frame is painting, or lets the offset past rows it is not. Every site below moved a pane's height
// somewhere OTHER than an open/close edge, and each one was found by the freshness audit.

// assertClampFresh checks layout()'s invariant on the Model as it stands: the viewport widget's
// height is the transcript's drawn row count, floored the one row a scroll surface needs.
func assertClampFresh(t *testing.T, m Model, when string) {
	t.Helper()
	if got, want := m.viewport.Height(), max(1, m.transcriptRows()); got != want {
		t.Fatalf("%s: viewport height = %d, drawn transcript rows = %d — the scroll clamp is stale",
			when, got, want)
	}
}

// wireSummaries is an offering of n models the /model picker draws a row each from, named so a
// filter can prune them apart.
func wireSummaries(names ...string) []heartbeat.ModelSummary {
	out := make([]heartbeat.ModelSummary, 0, len(names))
	for _, name := range names {
		out = append(out, heartbeat.ModelSummary{ID: name, ContextWindow: 32768})
	}
	return out
}

// browserMetas is a stored-session listing: n records in workspace, titled so a filter rune can
// tell them apart.
func browserMetas(workspace string, titles ...string) []session.Meta {
	now := time.Now()
	out := make([]session.Meta, 0, len(titles))
	for _, title := range titles {
		out = append(out, session.Meta{
			ID: title, Title: title, UpdatedAt: now, UserMsgs: 1, Workspace: workspace,
		})
	}
	return out
}

// TestPaneHeightChangeReachesLayout is the freshness guard for [Model.layout]'s single-setter rule:
// with the widget sized to the DRAWN rows, a pane redrawn at a new height without a layout() leaves
// the scroll clamp measuring the frame that came before it. Each case opens a pane over a transcript
// deeper than the window, moves that pane's height by a route that is not an open/close edge, and
// asserts the widget followed.
func TestPaneHeightChangeReachesLayout(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(*testing.T) Model
		act     func(*testing.T, Model) Model
	}{{
		// interject.go — the worker committed staged rows, so the band above the input box shrinks.
		name: "delivered rows leave the staged band",
		arrange: func(t *testing.T) Model {
			t.Helper()
			m := withStagedRows(modelWithOverlayRoomAt(t, 80, 24, testOpts), 3)
			m.layout()
			return m
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			return step(t, m, interjectedMsg{items: m.pendingInterjections[:2]})
		},
	}, {
		// model.go — an Event feeds the Inspector's ring, and the open pane derives its rows from it.
		name: "a wire event grows the open /inspect pane",
		arrange: func(t *testing.T) Model {
			t.Helper()
			opts := testOpts
			opts.Inspector = true
			m := modelWithOverlayRoomAt(t, 80, 24, opts)
			m.inspector = inspectorPane{open: true}
			m.layout()
			return m
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			return step(t, m, eventMsg{Event: domain.WireEvent{
				Direction: "request",
				Payload:   `{"model":"test-model","stream":true,"messages":[]}`,
			}})
		},
	}, {
		// heartbeat.go — a beat replaces the offering an open /model picker draws its rows from.
		name: "a beat moves the offering under an open /model picker",
		arrange: func(t *testing.T) Model {
			t.Helper()
			opts := testOpts
			serverSeams(&opts).beat = (&fakeHeartbeat{}).beat
			m := modelWithOverlayRoomAt(t, 80, 24, opts)
			m.hb.models = wireSummaries("alpha", "beta")
			m.picker = picker{open: true, kind: pickerModel}
			m.layout()
			return m
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			beat := upBeat(testOpts.Model, testOpts.ContextWindow)
			beat.AvailableModels = wireSummaries("alpha", "beta", "gamma", "delta", "epsilon")
			return foldBeatMsg(t, m, beat)
		},
	}, {
		// autocomplete.go — esc gives the dropdown's rows back to the transcript.
		name: "esc closes the autocomplete dropdown",
		arrange: func(t *testing.T) Model {
			t.Helper()
			m := modelWithOverlayRoomAt(t, 80, 24, testOpts)
			m.input.SetValue("/")
			m.autocomplete = m.computeAutocomplete(m.caretByteOffset())
			m.layout()
			if !m.autocomplete.active || len(m.autocomplete.items) == 0 {
				t.Fatal(`the "/" menu did not open — test premise broken`)
			}
			return m
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			return step(t, m, keyEsc())
		},
	}, {
		// sessions.go — the listing landed off the Update loop and opens the pane over the transcript.
		name: "the /sessions listing opens the browser",
		arrange: func(t *testing.T) Model {
			t.Helper()
			opts := testOpts
			opts.Workspace = "/ws/a"
			return modelWithOverlayRoomAt(t, 80, 24, opts)
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			return step(t, m, sessionListMsg{
				metas: browserMetas("/ws/a", "alpha one", "beta two", "gamma three"),
			})
		},
	}, {
		// sessions.go — ^a widens the browser to every workspace, which is a different row set.
		name: "the browser's workspace toggle changes its row set",
		arrange: func(t *testing.T) Model {
			t.Helper()
			opts := testOpts
			opts.Workspace = "/ws/a"
			m := modelWithOverlayRoomAt(t, 80, 24, opts)
			metas := browserMetas("/ws/a", "alpha one")
			metas = append(metas, browserMetas("/ws/b", "beta two", "gamma three", "delta four")...)
			return step(t, m, sessionListMsg{metas: metas})
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			return step(t, m, tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
		},
	}, {
		// sessions.go — a rune types into the browser's filter, which prunes rows.
		name: "a filter rune prunes the browser's rows",
		arrange: func(t *testing.T) Model {
			t.Helper()
			opts := testOpts
			opts.Workspace = "/ws/a"
			m := modelWithOverlayRoomAt(t, 80, 24, opts)
			return step(t, m, sessionListMsg{
				metas: browserMetas("/ws/a", "alpha one", "beta two", "gamma three"),
			})
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			return step(t, m, keyRune('g'))
		},
	}, {
		// picker.go — the same rune, one modal across.
		name: "a filter rune prunes the /model picker's rows",
		arrange: func(t *testing.T) Model {
			t.Helper()
			m := modelWithOverlayRoomAt(t, 80, 24, testOpts)
			// Deep enough that pruning to one row outruns the two the filter line and its gap add
			// back: at four rows the pane happened to draw the same height either way.
			m.hb.models = wireSummaries("alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta")
			m.picker = picker{open: true, kind: pickerModel}
			m.layout()
			return m
		},
		act: func(t *testing.T, m Model) Model {
			t.Helper()
			return step(t, m, keyRune('g'))
		},
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := tc.arrange(t)
			assertClampFresh(t, m, "setup")
			before := m.frameOverlays().height()

			m = tc.act(t, m)

			if after := m.frameOverlays().height(); after == before {
				t.Fatalf("premise: the overlays still measure %d rows — the act moved no pane height", after)
			}
			assertClampFresh(t, m, "after the act")
		})
	}
}

// TestANewPaneClaimingAKeyStillReachesLayout is the STRUCTURAL half of the freshness guard above:
// that one pins the panes that exist today, this one pins the mechanism they inherit it from. The
// claimant it walks is deliberately NOT on [keyClaimOrder] — it stands for a pane nobody has written
// yet — and it does exactly what such a pane will do wrong: it redraws itself at a new height and
// stops there, never laying out. Nothing but the walk's own freshening ([Model.claimKey]) can keep
// the scroll clamp honest for a surface no enumerated case knows about, so dropping that call fails
// this test while every case above still passes.
func TestANewPaneClaimingAKeyStillReachesLayout(t *testing.T) {
	m := modelWithOverlayRoomAt(t, 80, 24, testOpts)
	m.hb.models = wireSummaries("alpha", "beta")
	m.picker = picker{open: true, kind: pickerModel}
	m.layout()
	assertClampFresh(t, m, "setup")
	before := m.frameOverlays().height()

	// The offering is the picker's row source (pickerFilteredView), so replacing it is this surface
	// redrawing itself taller — the omission each enumerated case above had to be fixed for one site
	// at a time.
	unwritten := keyClaimant{
		name: "a pane nobody has written yet",
		claim: func(m Model, _ tea.KeyPressMsg) (Model, tea.Cmd, bool) {
			m.hb.models = wireSummaries("alpha", "beta", "gamma", "delta", "epsilon", "zeta")
			return m, nil, true
		},
	}

	m, _, claimed := m.claimKey([]keyClaimant{unwritten}, keyRune('x'))

	if !claimed {
		t.Fatal("premise: the surface did not claim the key")
	}
	if after := m.frameOverlays().height(); after == before {
		t.Fatalf("premise: the overlays still measure %d rows — the surface moved no pane height", after)
	}
	assertClampFresh(t, m, "after a key claimed by a pane that never lays out")
}

// A scroll that lands mid-history holds exactly there: content appended below does not move the
// view, and does not re-attach it either.
func TestScrollMidHistoryHoldsPositionOnAppend(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("a question", nil)
	for i := 0; i < 60; i++ {
		m.transcript.commitAssistant("reply paragraph "+strings.Repeat("x", 10), runRef{})
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

	m.transcript.commitAssistant("more streamed content", runRef{})
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
// overlay writes to View, not the viewport, so the viewport alone cannot see it.
func firstViewLine(m Model) string {
	return strings.SplitN(plain(m.View()), "\n", 2)[0]
}

// A short exchange stays whole at the tail: no prompt is hoisted to the top of an emptied
// screen, so the earlier turn is still on screen and no blank padding was appended below the
// reply. The prompt reaches the top row only naturally, once a reply has grown a screenful.
func TestShortReplyKeepsTheExchangeAtTheTail(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.addUser("FIRST-QUESTION", nil)
	m.transcript.commitAssistant("a prior short answer", runRef{})
	m.transcript.addUser("LATEST-PROMPT", nil)
	m.transcript.commitAssistant("a short reply", runRef{})
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
		m.transcript.commitAssistant("history paragraph "+strings.Repeat("x", 10), runRef{})
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
		m.transcript.commitAssistant("one reply "+strings.Repeat("x", 10), runRef{})
	}
	m.transcript.addUser("PROMPT-TWO", nil)
	for i := 0; i < 20; i++ {
		m.transcript.commitAssistant("two reply "+strings.Repeat("y", 10), runRef{})
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
		m.transcript.commitAssistant(fmt.Sprintf("reply line %02d", i), runRef{})
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
	// By POINTER: the Model carries four text fields now (the chat box, the /settings value row and
	// the two overlay filters) and a channel element may not exceed 64kB.
	done := make(chan *outcome, 1)
	go func() {
		next, _ := m.Update(tea.PasteMsg{Content: value})
		seeded := next.(Model)
		before := seeded.input.Height()
		next, _ = seeded.Update(keyRune('x'))
		done <- &outcome{next.(Model), before}
	}()
	var got *outcome
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

// TestFooterContentStripsEscapes pins the footer as an escape-strip SEAM (doc.go). Two of the three
// facts it paints on the left are the SERVER's own text — the model id it advertised and the effort
// default it reported — and the third is config text; displayModel and footerEffortLabel are pure
// formatters that pass whatever they are given straight through. The cell buffer honours OSC 8
// across the whole frame, so one unterminated opener painted into the footer's black field would
// make every remaining cell a link to somebody else's URL.
//
// Both widths are checked because the narrow branch composes its line separately (truncate + pad),
// and a strip that only covered the roomy branch would leave the other one painting the escape. The
// narrow width is below the mode marker's OWN floor, the only window the fit still falls back to
// that branch on: at thirty columns the marker now seats (footerFit), so a pair of widths either
// side of thirty would both take the marker's branch and leave the fallback covered by nothing.
func TestFooterContentStripsEscapes(t *testing.T) {
	t.Parallel()
	opts := testOpts
	opts.Model = "\x1b]8;;mailto:evil\x07qwen"
	opts.HostAlias = "host\x1b[31m"

	m := step(t, newModel(context.Background(), &fakeEngine{}, opts, nil), tea.WindowSizeMsg{Width: 80, Height: 24})
	m.hb.effort = provider.EffortSupport{Supported: true, Default: "\x1b]8;;x\x07medium"}

	// The line's own styling is CSI, which ansiPattern takes out; an OSC introducer is not, so what
	// survives that strip is exactly what a producer smuggled through.
	roomy := ansiPattern.ReplaceAllString(m.footerContent(80), "")
	narrow := ansiPattern.ReplaceAllString(m.footerContent(8), "")
	assertNoESCIn(t, "footer", roomy, narrow)

	if !strings.Contains(roomy, "qwen") || !strings.Contains(roomy, "medium") {
		t.Errorf("footer = %q, want the model id and the effort word still readable", roomy)
	}
	// The host's escape is CSI, so ansiPattern would eat an UNSTRIPPED one along with the styling
	// and the check above could not tell the two apart. A stripped one survives as inert text —
	// which is what makes this the assertion that the host went through the seam too.
	if want := "host[31m"; !strings.Contains(roomy, want) {
		t.Errorf("footer = %q, want the host segment left inert as %q", roomy, want)
	}
}

// TestFooterMarkerSaysWhetherAutoIsConfined pins the footer's confinement word to the LIVE blast
// radius: Auto's marker states what the next tool call would actually run under, and /confine moves
// that mid-session, so the word is read off the engine rather than off the boot Options the pane
// was built with. The three assertions are the three things a human reads it for — that a fenced
// Auto says so, that turning the fence off changes the word (in the error tone, the one state where
// Auto runs with their full privileges), and that a rung which never reads the flag stays silent.
func TestFooterMarkerSaysWhetherAutoIsConfined(t *testing.T) {
	eng := &fakeEngine{confine: true}
	m := newTestModelEng(t, eng, confineOpts(capableHost, domain.ModeAuto))

	flat := ansiPattern.ReplaceAllString(m.footerContent(120), "")
	if want := "auto · confined" + bodyIndent; !strings.HasSuffix(flat, want) {
		t.Errorf("footer = %q, want it to end %q", flat, want)
	}

	// /confine off through the real key path: the same model, the same width, one word different.
	m.input.SetValue("/confine off")
	m = step(t, m, keyEnter())

	footer := m.footerContent(120)
	flat = ansiPattern.ReplaceAllString(footer, "")
	want := "auto · unconfined" + bodyIndent
	if !strings.HasSuffix(flat, want) {
		t.Errorf("footer after /confine off = %q, want it to end %q", flat, want)
	}
	if run := m.th.footerText.Foreground(m.th.errorFg).Render(" · " + unconfinedWord); !strings.Contains(footer, run) {
		t.Errorf("footer does not carry %q in the error tone: %q", unconfinedWord, footer)
	}

	// A narrow window no longer costs the human the one fact they read the footer for: the marker
	// is what the row never gives up (footerFit), so at thirty columns it is still there WHOLE,
	// blast-radius word and all — the left run is what gave way to seat it.
	if narrow := ansiPattern.ReplaceAllString(m.footerContent(30), ""); !strings.HasSuffix(narrow, want) {
		t.Errorf("narrow footer = %q, want it to end %q", narrow, want)
	}
}

// TestFooterMarkerCarriesNoConfinementWordBelowAuto proves the lower rungs' markers are untouched:
// they gate every subprocess call through Approval whatever the flag says, so there is no blast
// radius to name — and the word must not leak in from a confined engine underneath them.
func TestFooterMarkerCarriesNoConfinementWordBelowAuto(t *testing.T) {
	for _, mode := range []domain.Mode{domain.ModePlan, domain.ModeAskBefore, domain.ModeAllowEdits} {
		t.Run(string(mode), func(t *testing.T) {
			m := newTestModelEng(t, &fakeEngine{confine: true}, confineOpts(capableHost, mode))
			flat := ansiPattern.ReplaceAllString(m.footerContent(120), "")
			if want := modeMarker(mode) + bodyIndent; !strings.HasSuffix(flat, want) {
				t.Errorf("footer = %q, want it to end %q with no confinement word", flat, want)
			}
		})
	}
}

// footerFactsModel builds the model the footer's fit is exercised on: every one of the four
// outward-in facts named — host alias, model id, effort word, workdir — and Auto's marker with its
// blast-radius word beside it, the widest marker the row ever has to seat. Nothing is dropped
// because nothing was missing, so which segment leaves at which width is the fit's doing alone.
func footerFactsModel(t *testing.T) Model {
	t.Helper()
	opts := testOpts
	opts.Workspace = "/ws/proj"
	opts.Mode = domain.ModeAuto
	opts.Confinement = capableHost
	serverSeams(&opts).beat = (&fakeHeartbeat{}).beat

	m := newTestModelEng(t, &fakeEngine{effortProfile: domain.EffortHigh, confine: true}, opts)
	beat := upBeat("test-model", 32768)
	beat.EffortSupport = provider.EffortSupport{
		Supported: true,
		Dialect:   provider.EffortDialectReasoning,
		Efforts:   []string{"low", "medium", "high"},
		Default:   "medium",
	}
	return foldBeatMsg(t, m, beat)
}

// TestFooterFitsTheWindowExactly is the footer's counterpart to TestStatusLineIndentFitsNarrowWindow:
// the bodyIndent lead and the marker's trailing margin are part of the row's width BUDGET, not an
// overhang, so the line the painter emits spends the window exactly — never a column more, which
// would wrap the footer onto a second row and push the bottom rule off the alternate screen.
//
// The sweep crosses all three shapes the fit composes: windows too narrow to seat the marker at all
// (and narrower than the lead margin itself), windows where the ladder has dropped segments to seat
// it, and roomy ones where nothing is given up.
func TestFooterFitsTheWindowExactly(t *testing.T) {
	m := footerFactsModel(t)

	for _, w := range []int{0, 1, 2, 3, 10, 20, 40, 80} {
		if got := m.th.measure.Width(m.footerContent(w)); got != w {
			t.Errorf("footer at width %d renders %d columns, want exactly %d: %q",
				w, got, w, ansiPattern.ReplaceAllString(m.footerContent(w), ""))
		}
	}
}

// TestFooterOfflineKeepsItsErrorToneAtEveryWidth pins the tone the offline word is read by. It is
// priority 0 in the fit — the row gives up every fact about the session before it gives up the word
// for a state a send is refused in — and it is painted as its own styled run beside the fact line
// rather than inside it. A narrow window used to fold the whole row through one Render and cost the
// word its error tone at exactly the width the row is most cramped; the layout hands it its own run
// at every width instead.
func TestFooterOfflineKeepsItsErrorToneAtEveryWidth(t *testing.T) {
	m := footerFactsModel(t)
	m.hb.offline = true

	for _, w := range []int{120, 40} {
		footer := m.footerContent(w)
		if run := m.th.footerText.Foreground(m.th.errorFg).Render(" " + glyphAssistant + " " + offlineLabel); !strings.Contains(footer, run) {
			t.Errorf("footer at width %d does not carry %q as its own error-toned run: %q",
				w, offlineLabel, footer)
		}
		if flat := ansiPattern.ReplaceAllString(footer, ""); !strings.Contains(flat, offlineLabel) {
			t.Errorf("footer at width %d = %q, want the offline word kept", w, flat)
		}
	}
}

// TestFooterWorkdirIsEscapeStripped is the workdir segment's security seam. The footer's other
// facts — host, model id, effort default — are put through stripEscapes as they are worded, but the
// workdir crossed the row unsanitised: a workspace whose ROOT NAME carries a CSI (a legal directory
// name on every POSIX filesystem, and one a checkout or an unpacked archive can author) painted the
// footer in a colour the theme never chose. It is stripped once at construction now, where the
// field is resolved.
//
// The claim is made twice — on the field and on the painted row — because either alone would pass a
// fix that sanitised only the other.
func TestFooterWorkdirIsEscapeStripped(t *testing.T) {
	opts := testOpts
	opts.Workspace = "/ws/proj\x1b[31mRED"
	m := newTestModelEng(t, &fakeEngine{}, opts)

	const want = "proj[31mRED"
	if strings.ContainsRune(m.workdir, 0x1b) {
		t.Errorf("m.workdir keeps the ESC: %q", m.workdir)
	}
	if !strings.HasSuffix(m.workdir, want) {
		t.Errorf("m.workdir = %q, want it to end %q — the sequence inert, its text kept", m.workdir, want)
	}
	footer := m.footerContent(120)
	if strings.Contains(ansiPattern.ReplaceAllString(footer, ""), "\x1b") {
		t.Errorf("the painted footer carries an ESC the theme did not author: %q", footer)
	}
	if flat := ansiPattern.ReplaceAllString(footer, ""); !strings.Contains(flat, want) {
		t.Errorf("footer = %q, want the workdir shown inert as %q", flat, want)
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
			body, rows, _ := m.popupBudget(paneBrowser, 8, maxSessionRows, popupChrome, popupFloor{})
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

// ----------------------------------------------------------------------------
// The viewport does not wrap: its rows are the stored lines, one for one
// ----------------------------------------------------------------------------

// vs16WideModel builds a transcript whose committed assistant line is EXACTLY the transcript width
// in the painter's measure (WcWidth) and two cells over the VIEWPORT's width in the widget's
// (GraphemeWidth), because it ends in two VARIATION SELECTOR-16 glyphs — `⚠️` is one painted cell
// and two grapheme cells (ADR 0030). A tool block follows it, so the fixture carries a second block
// whose header row is a click target. It is the exact shape that used to fold one stored line into
// two screen rows and put every row-addressed reader one row off — and, once the widget stopped
// wrapping, the shape whose trailing glyph the widget's clip ATE. The painter's own reserve breaks
// it into two stored lines now (reserveWidgetCells, render.go), so both glyphs stay on the screen
// and the rows stay 1:1 with the lines.
func vs16WideModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("go", nil)
	// −4: the assistant marker "✦ " spends two painted cells and the two ⚠️ spend one each.
	m.transcript.commitAssistant(strings.Repeat("a", m.transcriptWidth()-4)+"⚠️⚠️", runRef{})
	m.transcript.apply(domain.ToolCallEvent{Call: domain.ToolCall{
		ID: "c1", Tool: "terminal", Arguments: []byte(`{"command":"go test ./..."}`)}})
	m.transcript.apply(domain.ToolResultEvent{Result: domain.ToolResult{
		CallID: "c1", Content: "ok   a\nok   b\nok   c\nPASS"}})
	m.refreshViewport()
	return m
}

// wideAssistantLine returns the index of the FIRST stored line of the fixture's over-wide answer —
// the line the painter's reserve broke it at — and asserts the reserve did its work: the line ends
// on the first `⚠️` and fits the viewport's own width in the widget's measure, which is what leaves
// the widget's clip nothing to cut.
func wideAssistantLine(t *testing.T, m Model) int {
	t.Helper()
	for i, ln := range m.lines {
		if !strings.Contains(strip(ln), "aaaa") {
			continue
		}
		if got := ansi.StringWidth(ln); got > m.viewport.Width() {
			t.Fatalf("setup: line %d measures %d grapheme cells on a %d-cell viewport; the painter's "+
				"reserve left it for the widget to cut", i, got, m.viewport.Width())
		}
		if got := strip(ln); !strings.HasSuffix(got, vs16Warning) {
			t.Fatalf("setup: line %d is %q; the reserve should have ended it on the first ⚠️", i, got)
		}
		return i
	}
	t.Fatal("setup: the fixture's over-wide assistant line is not in the rendered lines")
	return -1
}

// TestViewportRowsStayOneForOneWithStoredLines is the row-map invariant. A stored line the WIDGET
// measures wider than its width used to be soft-wrapped into two screen rows — the painter had
// already wrapped it in WcWidth, the widget re-measured in GraphemeWidth, and a `⚠️` was the two
// measures disagreeing. Every reader of `contentLineAt(row) = YOffset() + row` below such a line then
// addressed the wrong line: a click on the NEXT block's header toggled whatever sat one row up.
// With SoftWrap off the widget's rows ARE the stored lines, so the count matches and the click lands.
func TestViewportRowsStayOneForOneWithStoredLines(t *testing.T) {
	m := vs16WideModel(t)
	wide := wideAssistantLine(t, m)

	if got, want := m.viewport.TotalLineCount(), len(m.lines); got != want {
		t.Errorf("the viewport holds %d rows for %d stored lines; the over-wide line at %d was re-wrapped",
			got, want, wide)
	}
	if got, want := len(strings.Split(m.viewport.View(), "\n")), m.viewport.Height(); got != want {
		t.Errorf("the viewport drew %d rows on a %d-row widget", got, want)
	}

	header := markedLine(t, m, targetHeader)
	if header <= wide {
		t.Fatalf("setup: the tool block's header is line %d, not below the over-wide line %d", header, wide)
	}
	if blockExpanded(t, m, header) {
		t.Fatal("setup: the block is expanded before any click; collapsed is the default")
	}
	// The click is aimed where the row is DRAWN — the human clicks what is on the screen, and the
	// model maps that row back through contentLineAt. The block's LAST row is the discriminating
	// one: an extra screen row above it shifts the whole block down, so the mapped line falls past
	// the block (past the transcript's end here) and the toggle misses entirely.
	m = clickCell(t, m, 6, drawnRow(t, m, "go test ./..."))
	if !blockExpanded(t, m, header) {
		t.Error("a click on the drawn leader row below the over-wide line did not toggle that block: the row map drifted")
	}
}

// drawnRow returns the viewport row the given text is DRAWN on — the screen row a human aiming at
// that text would click. It is deliberately not screenRow, which converts a stored-line index and so
// assumes the very 1:1 mapping these tests are here to prove.
func drawnRow(t *testing.T, m Model, want string) int {
	t.Helper()
	for row, ln := range strings.Split(m.viewport.View(), "\n") {
		if strings.Contains(strip(ln), want) {
			return row
		}
	}
	t.Fatalf("no drawn viewport row carries %q", want)
	return -1
}

// TestPainterReservesTheCellsTheWidgetMeasuresOver is the other half of the same decision. The
// widget does not wrap, so a line it measures over its width has its tail CUT — and a line the
// painter had filled to the column with a `⚠️` at the end lost that glyph outright, which is
// content the painter drew and the reader never saw. The painter therefore reserves the widget's
// extra cells (reserveWidgetCells, render.go): it breaks such a line into stored lines of its own,
// so every glyph is still drawn, no drawn row is over the viewport's width in the widget's measure,
// and the clip finds nothing to cut.
func TestPainterReservesTheCellsTheWidgetMeasuresOver(t *testing.T) {
	m := vs16WideModel(t)
	wide := wideAssistantLine(t, m)

	rows := strings.Split(m.viewport.View(), "\n")
	row := rows[wide-m.viewport.YOffset()]
	if got := ansi.StringWidth(row); got > m.viewport.Width() {
		t.Errorf("the drawn row is %d grapheme cells on a %d-cell viewport; the reserve did not hold",
			got, m.viewport.Width())
	}
	if n := strings.Count(strip(row), "⚠"); n != 1 {
		t.Errorf("the drawn row carries %d ⚠️ glyphs, want 1: the widget has no room for the second", n)
	}
	// The glyph the widget's own measure left no room for is on the row BELOW — a stored line of its
	// own, put there by the painter, rather than cut off the right edge.
	if next := strip(rows[wide+1-m.viewport.YOffset()]); !strings.Contains(next, "⚠") {
		t.Errorf("row %d is %q; the glyph the reserve broke off is not drawn anywhere", wide+1, next)
	}
	if n := strings.Count(strip(m.viewport.View()), "⚠"); n != 2 {
		t.Errorf("the frame draws %d ⚠️ glyphs, want the 2 the answer committed", n)
	}
	if got := m.viewport.XOffset(); got != 0 {
		t.Errorf("the transcript sits at x-offset %d; the reserve must not move the view sideways", got)
	}
}

// TestFullWidthLineKeepsItsTrailingWideMeasuredGlyph is the reserve at its exact edge: an answer
// filled to the transcript width in the painter's measure and ending in ONE `⚠️` measures exactly
// the viewport's width to the widget — the transcript width plus the right gutter (bodyRightGutter)
// — so it fits whole. Nothing is broken and nothing is cut: one stored line, one drawn row, the
// glyph still on it. It is the case the reserve must NOT spend a row on, the row-map invariant and
// the trailing glyph in one.
func TestFullWidthLineKeepsItsTrailingWideMeasuredGlyph(t *testing.T) {
	m := newTestModel(t) // 80x24
	m.transcript.reset()
	m.transcript.addUser("go", nil)
	// −3: the assistant marker "✦ " spends two painted cells and the single ⚠️ spends one.
	m.transcript.commitAssistant(strings.Repeat("a", m.transcriptWidth()-3)+vs16Warning, runRef{})
	m.refreshViewport()

	line := -1
	for i, ln := range m.lines {
		if strings.Contains(strip(ln), "aaaa") {
			line = i
			break
		}
	}
	if line < 0 {
		t.Fatal("setup: the full-width assistant line is not in the rendered lines")
	}
	if got := m.th.measure.Width(m.lines[line]); got != m.transcriptWidth() {
		t.Fatalf("setup: line %d measures %d painted cells, want the full width %d",
			line, got, m.transcriptWidth())
	}
	if got := ansi.StringWidth(m.lines[line]); got != m.viewport.Width() {
		t.Fatalf("setup: line %d measures %d grapheme cells, want exactly the viewport's %d "+
			"(the edge the reserve has to leave alone)", line, got, m.viewport.Width())
	}

	rows := strings.Split(m.viewport.View(), "\n")
	if got := strip(rows[line-m.viewport.YOffset()]); !strings.HasSuffix(got, vs16Warning) {
		t.Errorf("the drawn row is %q; its trailing ⚠️ was clipped", got)
	}
	if got, want := m.viewport.TotalLineCount(), len(m.lines); got != want {
		t.Errorf("the viewport holds %d rows for %d stored lines; the full-width line did not stay on one row",
			got, want)
	}
	if next := line + 1 - m.viewport.YOffset(); next < len(rows) && strings.Contains(strip(rows[next]), "⚠") {
		t.Errorf("row %d is %q; the full-width line spilled onto a second row", next, strip(rows[next]))
	}
	if got := m.viewport.XOffset(); got != 0 {
		t.Errorf("the transcript sits at x-offset %d; horizontal scrolling must stay off", got)
	}
}

// TestReserveWidgetCellsMovesTargetsAndSpansWithTheRows pins the bookkeeping the reserve owes the
// rest of the model. A line it breaks becomes TWO stored lines, so everything addressed by line
// index moves with it: the click target of the line it came from is carried onto the continuation
// row (every physical row of a header is the same click surface), and a user block's span is pushed
// down by the rows added above it and stretched by the rows added inside it. A tail that shows
// nothing but padding is dropped rather than given a row of its own — the reserve is here to keep
// glyphs on the screen, not the spaces a squared line was filled out with.
func TestReserveWidgetCellsMovesTargetsAndSpansWithTheRows(t *testing.T) {
	const limit = 6
	in := renderedTranscript{
		lines: []string{
			"user",                          // one row: inside the limit in both measures
			strings.Repeat(vs16Warning, 4),  // 4 painted cells, 8 grapheme cells: two rows
			"tail" + strings.Repeat(" ", 8), // over the limit in PADDING alone: still one row
		},
		userBlocks: []userBlock{{start: 0, count: 1}, {start: 1, count: 2}},
		targets:    []lineTarget{{}, {kind: targetHeader, entry: 3}, {}},
	}

	got := in.reserveWidgetCells(limit)

	wantLines := []string{"user", strings.Repeat(vs16Warning, 3), vs16Warning, "tail  "}
	if !slices.Equal(mapStrip(got.lines), wantLines) {
		t.Errorf("lines = %q, want %q", mapStrip(got.lines), wantLines)
	}
	wantTargets := []lineTarget{{}, {kind: targetHeader, entry: 3}, {kind: targetHeader, entry: 3}, {}}
	if !slices.Equal(got.targets, wantTargets) {
		t.Errorf("targets = %+v, want %+v", got.targets, wantTargets)
	}
	wantBlocks := []userBlock{{start: 0, count: 1}, {start: 1, count: 3}}
	if !slices.Equal(got.userBlocks, wantBlocks) {
		t.Errorf("userBlocks = %+v, want %+v", got.userBlocks, wantBlocks)
	}
	// Nothing the pass returns is over the limit in the measure the widget would cut it in.
	for i, ln := range got.lines {
		if w := ansi.StringWidth(ln); w > limit {
			t.Errorf("line %d %q is %d grapheme cells, over the %d-cell limit", i, strip(ln), w, limit)
		}
	}
}

// TestTranscriptNeverScrollsSideways pins the guard that comes with the clip: horizontal scrolling
// is disabled outright (SetHorizontalStep(0), newModel), so no gesture can walk the view off column
// 0 and leave the mouse's column arithmetic addressing cells that are not where it thinks. The three
// gestures the widget binds are all asked: a wheel-left notch, a shift-modified wheel, and the
// left/right keys in an inert state, where keys scroll the transcript.
func TestTranscriptNeverScrollsSideways(t *testing.T) {
	base := vs16WideModel(t)
	wide := wideAssistantLine(t, base)
	row := screenRow(t, base, wide)

	cases := []struct {
		name string
		msg  tea.Msg
	}{
		{"wheel left", tea.MouseWheelMsg{X: 10, Y: row, Button: tea.MouseWheelLeft}},
		{"wheel right", tea.MouseWheelMsg{X: 10, Y: row, Button: tea.MouseWheelRight}},
		{"shift-wheel down", tea.MouseWheelMsg{X: 10, Y: row, Button: tea.MouseWheelDown, Mod: tea.ModShift}},
		{"shift-wheel up", tea.MouseWheelMsg{X: 10, Y: row, Button: tea.MouseWheelUp, Mod: tea.ModShift}},
		{"left key", tea.KeyPressMsg{Code: tea.KeyLeft}},
		{"right key", tea.KeyPressMsg{Code: tea.KeyRight}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := base
			m.state = stateErrored // the inert state where the keys scroll instead of typing
			m = step(t, m, c.msg)
			if got := m.viewport.XOffset(); got != 0 {
				t.Errorf("%s left the transcript at x-offset %d; horizontal scrolling must stay off", c.name, got)
			}
		})
	}
}

// TestFooterModeMarkerSpanAgreesWithThePaintedCells is the painter/pointer pin. The marker's text
// and the column it lands on used to be two arithmetics that agreed — footerContent composed the
// marker and measured its way to the right edge, and anything asking WHERE it was drawn had to
// repeat both. They are one value now ([Model.footerModeSpan]), and this is the assertion that
// keeps them one: the cells the span reports hold exactly the string it reports, in the real
// rendered footer. Auto is the case that matters, because its blast-radius word is part of the same
// marker and is painted in two tones.
func TestFooterModeMarkerSpanAgreesWithThePaintedCells(t *testing.T) {
	for _, tc := range []struct {
		name string
		eng  *fakeEngine
		mode domain.Mode
		want string
	}{
		{"ask before", &fakeEngine{}, domain.ModeAskBefore, modeMarker(domain.ModeAskBefore)},
		{"auto confined", &fakeEngine{confine: true}, domain.ModeAuto, modeMarker(domain.ModeAuto) + " · " + confinedWord},
		{"auto unconfined", &fakeEngine{confine: false}, domain.ModeAuto, modeMarker(domain.ModeAuto) + " · " + unconfinedWord},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newTestModelEng(t, tc.eng, confineOpts(capableHost, tc.mode))

			// Thirty columns is the case the fit changed: the marker used to drop whole there, and
			// now the left run gives way to seat it — so the pointer must address its cells on a
			// narrow window exactly as it does on a roomy one.
			for _, w := range []int{30, 80, 120} {
				text, col, ok := m.footerModeSpan(w)
				if !ok {
					t.Fatalf("width %d: the marker does not fit, want it drawn", w)
				}
				if text != tc.want {
					t.Errorf("width %d: span text = %q, want %q", w, text, tc.want)
				}

				flat := ansiPattern.ReplaceAllString(m.footerContent(w), "")
				right := col + m.th.measure.Width(text)

				if got := m.th.measure.Cut(flat, col, right); got != text {
					t.Errorf("width %d: painted cells [%d,%d) = %q, want the reported %q", w, col, right, got, text)
				}
			}
		})
	}
}

// TestCancelCommitsThePartialBeforeTheNote: a stopped reply keeps what had arrived as a real
// ENTRY, not as the live preview it was mid-stream. A preview belongs to no entry, so the next
// prompt used to render above it and a resumed session showed the note with nothing before it.
// Committed, the three land in the order the screen shows: partial, `· cancelled`, next message.
func TestCancelCommitsThePartialBeforeTheNote(t *testing.T) {
	t.Run("the streamed partial becomes the entry the note stands behind", func(t *testing.T) {
		m := runningModel(t)
		m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "Item 1.\n"}})
		m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "Item 2."}})

		m = step(t, m, cancelledMsg{})
		m.transcript.addUser("next", nil)

		want := []entry{
			{kind: entryAssistant, text: "Item 1.\nItem 2."},
			{kind: entryNote, text: "cancelled"},
			{kind: entryUser, text: "next"},
		}
		got := m.transcript.entries
		if len(got) < len(want) {
			t.Fatalf("transcript holds %d entries; want at least the closing %d: %+v",
				len(got), len(want), got)
		}
		for i, w := range want {
			e := got[len(got)-len(want)+i]
			if e.kind != w.kind || e.text != w.text || e.depth != 0 {
				t.Errorf("closing entry %d = kind %v depth %d text %q; want kind %v depth 0 text %q",
					i, e.kind, e.depth, e.text, w.kind, w.text)
			}
		}
		if m.transcript.streaming {
			t.Error("the transcript is still streaming after the cancel; the buffer was not taken")
		}
		if got := m.transcript.pending.String(); got != "" {
			t.Errorf("pending buffer after the cancel = %q; want it drained", got)
		}
	})

	t.Run("a whitespace-only buffer commits nothing", func(t *testing.T) {
		m := runningModel(t)
		m = step(t, m, eventMsg{Event: domain.TokenEvent{Text: "  \n\n"}})

		m = step(t, m, cancelledMsg{})

		last := m.transcript.entries[len(m.transcript.entries)-1]
		if last.kind != entryNote || last.text != "cancelled" {
			t.Errorf("last entry = kind %v text %q; want the cancelled note alone, no blank reply",
				last.kind, last.text)
		}
	})
}
