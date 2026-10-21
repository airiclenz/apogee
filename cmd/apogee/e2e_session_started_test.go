package main

// The session-started notice end to end (ADR 0076 amendment of 2026-10-19): a driven TUI session
// whose `reactions:` block runs a command on `session-started`, asserted from OUTSIDE apogee — the
// file that command appended to. The engine half is pinned in internal/agent (sessionstart_test.go);
// what this proves is the composition: the launch's construction announces once the late engine has
// installed its Agent, `/clear` and `/fork` announce their own starts, and each boundary — which runs
// on the TUI's Update goroutine — lands rather than waiting on the Runner (the
// TestE2EBackgroundClearDoesNotHang shape). An `advise:` entry on the same notice is driven too: its
// fence leads the first Turn request, and `/advice` shows the firing.

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// TestE2ESessionStartedAdviseLeadsTheFirstTurn is the session-start slot's TUI journey: an argv
// `advise:` entry on `session-started` in the user's own config.yaml runs at the launch's Session
// boundary, and what it printed reaches the model fenced AHEAD of the prompt on the session's
// first Turn request — the leading fence domain.RenderAdvice composes for the entry, its origin,
// session-started and the Turn the join stamps — and `/advice` then shows that firing with its text.
//
// auto-title is off so the first request the stub answers IS the Turn's: the naming call would
// otherwise race it (titleCmd fires beside the first Exchange) and carry no fence of its own.
func TestE2ESessionStartedAdviseLeadsTheFirstTurn(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "hooks"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, "auto-title: false\n"+
		adviseConfig(domain.MomentSessionStarted, "echo "+adviseCoachSays))

	submit(drv, hooksPrompt)
	drv.WaitText(hooksAnswer)
	waitIdle(drv)

	want := domain.RenderAdvice(domain.AdviceSpan{
		Reaction: adviseCoachName,
		Origin:   domain.OriginUser,
		Moment:   domain.MomentSessionStarted,
		Turn:     0,
		Leading:  true,
	}, adviseCoachText)
	if got := openingHandedOn(t, stub, 0); got != want+hooksPrompt {
		t.Errorf("the first Turn request opened with the user message\n%q\nwant the leading fence "+
			"ahead of the prompt\n%q", got, want+hooksPrompt)
	}

	// The transcript never carries advice, so the coach's words on screen are the pane's alone.
	submit(drv, "/advice")
	drv.WaitText(adviseCoachName + " (" + string(domain.OriginUser) + " origin) @ " +
		string(domain.MomentSessionStarted))
	drv.WaitText(strings.TrimSpace(adviseCoachText))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

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

// TestE2ESessionStartedForkDoesNotHang launches a session, has one exchange, forks it at that
// exchange's prompt, and sees the switch to the child; the reaction sink then holds exactly the
// launch's boundary and the fork's, the fork's marked `fork` rather than `resume`.
func TestE2ESessionStartedForkDoesNotHang(t *testing.T) {
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
	waitIdle(drv)
	drv.WaitFor(func() bool { return len(readHookPayloads(t, sink)) == 1 },
		tuitest.Awaiting("the launch's session-started payload in the reaction sink"))
	drv.WaitFor(func() bool { return len(sess.sessionRecords()) == 1 },
		tuitest.Awaiting("the parent's idle save to reach the session store"))

	// /fork, and the picker's first row — the one prompt there is, keeping its whole Exchange.
	submit(drv, "/fork")
	drv.WaitText(forkPickerTitle)
	drv.Press(tuitest.Enter)
	// The switch: only the fork's own fold paints the note naming the parent it was cut from, and it
	// paints it once the engine has accepted the restore.
	drv.WaitText("forked from ")
	waitIdle(drv)
	drv.WaitFor(func() bool { return len(readHookPayloads(t, sink)) == 2 },
		tuitest.Awaiting("the fork's session-started payload in the reaction sink"))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
	// Read once the run has drained its Runner, so a late duplicate would be in the file by now.
	var starts []domain.SessionStart
	for _, payload := range readHookPayloads(t, sink) {
		starts = append(starts, payload.Start)
	}
	if want := []domain.SessionStart{domain.SessionStartNew, domain.SessionStartFork}; !slices.Equal(starts, want) {
		t.Errorf("the session-started starts were %v; want each boundary exactly once, %v", starts, want)
	}
}
