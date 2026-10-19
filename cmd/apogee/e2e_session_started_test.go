package main

// The session-started notice end to end (ADR 0076 amendment of 2026-10-19): a driven TUI session
// whose `reactions:` block runs a command on `session-started`, asserted from OUTSIDE apogee — the
// file that command appended to. The engine half is pinned in internal/agent (sessionstart_test.go);
// what this proves is the composition: the launch's construction announces once the late engine has
// installed its Agent, `/clear` announces its own start, and the boundary — which runs on the TUI's
// Update goroutine — lands rather than waiting on the Runner (the TestE2EBackgroundClearDoesNotHang
// shape).

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// TestE2ESessionStartedClearDoesNotHang launches a session, has one exchange, types `/clear`, and
// sees the cleared view; the reaction sink then holds exactly the two boundaries, in order.
func TestE2ESessionStartedClearDoesNotHang(t *testing.T) {
	t.Parallel()

	sink := filepath.Join(t.TempDir(), "fired.jsonl")
	stub := stubllm.New(t, loadScript(t, "hooks"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, hookBlockOf(
		"  - id: "+hooksSinkName+"\n"+
			"    on: ["+string(domain.MomentSessionStarted)+"]\n"+
			"    run: [sh, -c, 'cat >> \"$0\"', '"+sink+"']\n"))

	submit(drv, hooksPrompt)
	drv.WaitText(hooksAnswer)
	drv.WaitFor(func() bool { return len(readHookPayloads(t, sink)) == 1 },
		tuitest.Awaiting("the launch's session-started payload in the reaction sink"))

	submit(drv, "/clear")
	// The cleared view: the closed conversation's reply leaves the transcript, which only the clear's
	// own fold can do — the view is reset once the engine has accepted the clear.
	drv.WaitGone(hooksAnswer)
	drv.WaitText(idlePromptHead)
	drv.WaitFor(func() bool { return len(readHookPayloads(t, sink)) == 2 },
		tuitest.Awaiting("the clear's session-started payload in the reaction sink"))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	// Read once the run has drained its Runner, so a late duplicate would be in the file by now.
	var starts []domain.SessionStart
	for _, payload := range readHookPayloads(t, sink) {
		starts = append(starts, payload.Start)
	}
	if want := []domain.SessionStart{domain.SessionStartNew, domain.SessionStartClear}; !slices.Equal(starts, want) {
		t.Errorf("the session-started starts were %v; want each boundary exactly once, %v", starts, want)
	}
}
