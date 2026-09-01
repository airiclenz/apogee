package main

// The generated-delegation-name journey end to end (ADR 0068): the model delegates without naming
// the delegation, apogee asks the child's own Upstream for a name while the child works, and the
// name it gets back is what a human — and a script reading stderr — sees that run called.
//
// Every seam of this is already pinned one layer down: the prompt in internal/title, the goroutine
// and the event in internal/agent, the host's completion in naming.go, the head rename in
// internal/tui, the usage record in internal/run. None of those proves the ROPE, and the rope is
// where this feature can fail without a single unit test noticing — a namer that is never injected,
// a gate wired to the wrong cell, an event that reaches the fold after the block has closed. So
// this file asserts the one thing no unit can: that a real composition, talking to a scripted
// server, ends up showing the name the server invented.
//
// The three cases are the three Drivers the feature has to be true on: the TUI, an unattended
// headless run, and a session that has switched naming OFF. The last is not a nicety — `auto-title:`
// is the only door a user has, and a gate that silently failed open would spend a completion per
// delegation on every session that asked for none.

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/config"
	"github.com/airiclenz/apogee/internal/run"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The fixture's own words, restated here so an assertion reads as the claim it is making.
// testdata/stubllm/naming.yaml holds the other half of each pair, and the name deliberately shares
// no word with the task: text that says the run is called "scout config keys" cannot be the task's
// first line wearing a haircut.
const (
	namingPrompt = "Delegate the configuration audit to a sub-agent."
	namingTask   = "Audit every configuration key in this workspace"
	// The head of the task, for the assertions made against a FRAME: a delegation's branch row
	// clips its label to the width it has, so the whole task line is never on screen and only its
	// opening is a fact a frame can be asked about.
	namingTaskHead  = "Audit every configuration key"
	namingGenerated = "scout config keys"
	namingWrapUp    = "The audit is in."

	// The opening of internal/title/prompts/delegation-instruction.txt, which is how a naming
	// request is told from every other request in the log — and from the SESSION title's call,
	// whose instruction opens "You name coding sessions". It is restated rather than imported
	// because it is a prompt asset, and a re-wording that dropped the delegation half of it would
	// leave this file's fixture matching nothing.
	namingInstruction = "You name delegated sub-tasks"
)

// TestE2ENamingPaintsAGeneratedName is the TUI half: a delegation the model left unnamed is
// painted by the name the out-of-band call produced, in place of the task's first line it wore
// while the call was in flight.
func TestE2ENamingPaintsAGeneratedName(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "naming"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)

	submit(drv, namingPrompt)
	drv.WaitText("Sub-Agent")

	// The name arriving is the assertion. It cannot be waited for by waiting for the delegation to
	// END — the naming call runs concurrently with the child (ADR 0068 decision 3), so a fast child
	// can finish first and the name lands on a block that is already closed, which is exactly the
	// case that must still paint.
	drv.WaitText(namingGenerated)
	drv.WaitText(namingWrapUp)
	drv.WaitQuiet(settled)

	// And the task's first line is GONE from the block, rather than sitting beside the new name: the
	// head carries one label, and the rename moves it.
	flat := flatten(drv.Frame().String())
	if strings.Contains(flat, flatten(namingTaskHead)) {
		t.Errorf("the delegation still shows its task first line beside the generated name:\n%s", drv.Frame())
	}

	// Exactly one naming call was made for one delegation. Two would mean the engine asks per Turn
	// rather than per spawn, which is the cost this feature is bounded by.
	if calls := namingRequests(stub); len(calls) != 1 {
		t.Errorf("the run made %d naming requests, want exactly 1", len(calls))
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2ENamingReachesAHeadlessSubAgentLine is the unattended half: the same script, the same
// generated name, on the Driver that has no block to paint and reports a delegation as one line of
// stderr.
//
// It runs the REAL runner (run.Once) rather than headless_test.go's stubRunner, because what is
// being asserted is that the composition INJECTS a namer at all on this path — the firing Config's
// own (wire_firing.go). A stubbed runner would only prove that the line formats a Name the test
// itself set, which headless_test.go already pins.
func TestE2ENamingReachesAHeadlessSubAgentLine(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "naming"))
	errOut := headlessAgainst(t, stub, "")

	lines := subAgentStderrLines(errOut)
	if len(lines) != 1 {
		t.Fatalf("sub-agent lines = %q; want exactly one, for the one delegation the script makes", lines)
	}
	// The reading is the composition's own — a real run against a real window — so the line is
	// asserted by its TAIL, which is the half this item is about.
	if !strings.HasSuffix(lines[0], " · "+namingGenerated) {
		t.Errorf("the sub-agent line is %q; it must close with the generated name %q", lines[0], namingGenerated)
	}
	if strings.Contains(errOut, namingTask) {
		t.Errorf("the delegation printed its task beside the generated name: %q", errOut)
	}
	if calls := namingRequests(stub); len(calls) != 1 {
		t.Errorf("the headless run made %d naming requests, want exactly 1", len(calls))
	}
}

// TestE2ENamingIsSilentWithAutoTitleOff is the gate: `auto-title: false` and no naming request is
// made at all — not one that is made and discarded. The delegation still runs and still paints, on
// the task's first line it has always worn.
func TestE2ENamingIsSilentWithAutoTitleOff(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "naming"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, "auto-title: false\n")

	submit(drv, namingPrompt)
	drv.WaitText("Sub-Agent")
	// Waiting for the parent's wrap-up is waiting for the whole delegation to be over: the naming
	// call would have been made at spawn, so by here a request that was going to arrive has.
	drv.WaitText(namingWrapUp)
	drv.WaitQuiet(settled)

	if calls := namingRequests(stub); len(calls) != 0 {
		t.Errorf("auto-title is off and the run still made %d naming requests", len(calls))
	}
	flat := flatten(drv.Frame().String())
	if strings.Contains(flat, flatten(namingGenerated)) {
		t.Errorf("a name was painted with auto-title off:\n%s", drv.Frame())
	}
	if !strings.Contains(flat, flatten(namingTaskHead)) {
		t.Errorf("the unnamed delegation does not show its task first line:\n%s", drv.Frame())
	}
	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// headlessAgainst runs one real `apogee headless` against stub in a home of its own and returns
// what reached stderr. extraConfig is written above the `servers:` block, which is how a case
// reaches a file-only key.
//
// `context-window:` is pinned rather than discovered: a sub-agent line is printed only for a run
// whose fill sits in a window (headlessSubAgentLines), and a stub advertises none.
func headlessAgainst(t *testing.T, stub *stubllm.Server, extraConfig string) string {
	t.Helper()

	prev := runOnce
	runOnce = run.Once
	t.Cleanup(func() { runOnce = prev })
	// The environment must not move the home or the mode out from under the run.
	assertNoAmbientApogeeConfig(t)
	t.Setenv(config.EnvMode, "")

	home := t.TempDir()
	writeConfigHome(t, home, extraConfig+
		"context-window: 32768\n"+
		"servers:\n"+
		"  - name: stub\n"+
		"    endpoint: "+stub.URL+"\n"+
		"    model: "+stub.Model+"\n"+
		"server: stub\n")

	cmd := newHeadlessCommand()
	var outBuf, errBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetErr(&errBuf)
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--config", home, "--workspace", e2eWorkspace(t), namingPrompt})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("headless: %v\n%s", err, errBuf.String())
	}
	return errBuf.String()
}

// namingRequests picks the DELEGATION naming calls out of a stub's request log by the one thing
// that identifies them — the system instruction they carry. It is what makes "no naming request was
// made" a claim about the wire rather than about the screen.
func namingRequests(stub *stubllm.Server) []stubllm.Request {
	var out []stubllm.Request
	for _, r := range stub.Requests() {
		for _, m := range r.Messages {
			if m.Role == "system" && strings.Contains(m.Content, namingInstruction) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}
