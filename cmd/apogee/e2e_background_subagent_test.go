package main

// A background sub_agent (ADR 0094) end to end: the model calls `sub_agent` with
// `background: true`, the call is answered at once and the reply ends while the child is held; the
// human's message reaches the model meanwhile; the child is let go, its finish line lands, and the
// idle agent wakes on a note carrying the child's report. /workflows then lists the run under its
// delegation's name with its `sub_agent` origin, offers no save-as-recipe, and opens its one item
// from its stage's row.
//
// The pieces are pinned one layer down — the launch and the note in internal/agent, the paint and
// the pane in internal/tui. This file asserts the rope: a real composition, with `workflow` lifted
// so the TUI offers the switch, talking to a scripted server.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The fixture's own words, restated here so an assertion reads as the claim it is making
// (testdata/stubllm/background-subagent.yaml).
const (
	bgSubAgentPrompt   = "Scout the repository in the background."
	bgSubAgentAnswered = "The scout is running in the background."
	bgSubAgentMessage  = "What else is there to do meanwhile?"
	bgSubAgentReply    = "Nothing else while the scout runs."
	bgSubAgentReport   = "The scout found three files."
	bgSubAgentWoken    = "The scout reported three files."
	// bgSubAgentGate holds the child's answer until the human's message has been answered.
	bgSubAgentGate = "scouted"
	// bgSubAgentStarted opens the call's immediate answer (internal/agent's
	// subAgentBackgroundStarted), the text the call's row carries in its slot.
	bgSubAgentStarted = "sub_agent started in the background"
	// bgSubAgentNoteLead is the finish note's lead line (internal/agent's sub_agent note).
	bgSubAgentNoteLead = "sub_agent scout finished"
	// bgSubAgentFinishLine is the transcript's finish line (internal/tui's subAgentFinishFormat
	// over the delegation's name and the item tally the engine's receipt counts).
	bgSubAgentFinishLine = "background sub_agent scout finished — items 1 · ok 1 · partial 0 · blocked 0"
	// bgSubAgentWorkflowsRow is the /workflows row's tail: the item counted, and the origin.
	bgSubAgentItems  = "· 1/1 items"
	bgSubAgentOrigin = "· sub_agent"
	// bgSubAgentItemStatus is the item level's status row: the engine's receipt over the report's
	// first line.
	bgSubAgentItemStatus = "status: ok — " + bgSubAgentReport
)

// bgSubAgentTools lifts `workflow`, the roster delta that offers sub_agent's background switch.
const bgSubAgentTools = "tools:\n  enabled: [workflow]\n"

// TestE2EBackgroundSubAgent is the journey.
func TestE2EBackgroundSubAgent(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "background-subagent"))
	home := preemptHome(t, stub, 2)
	appendHomeConfig(t, home, bgSubAgentTools)
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, home, "")

	submit(drv, bgSubAgentPrompt)
	drv.WaitText(bgSubAgentAnswered)

	// The call's row is a background-workflow call: the delegation's name over the immediate
	// answer, and no delegation verdict.
	row := rowContaining(t, drv.Frame(), bgSubAgentStarted)
	if !strings.Contains(row, "scout") || strings.Contains(row, "done") {
		t.Errorf("the background call's row = %q; want the delegation's name over the immediate answer", row)
	}

	// The child is held; the human's message reaches the model meanwhile.
	submit(drv, bgSubAgentMessage)
	drv.WaitText(bgSubAgentReply)
	if !requestEndsOn(stub, bgSubAgentMessage) {
		t.Fatalf("no request ends on the human's message while the child ran:\n%s", preemptRequestLog(stub))
	}

	// The child is let go: its finish line lands and the idle agent wakes on its report.
	stub.Release(bgSubAgentGate)
	drv.WaitText(bgSubAgentWoken)
	drv.WaitQuiet(settled)
	if text := flatten(drv.Frame().String()); !strings.Contains(text, bgSubAgentFinishLine) {
		t.Errorf("the frame does not carry the finish line %q:\n%s", bgSubAgentFinishLine, drv.Frame())
	}
	wake, ok := wakeRequest(stub)
	if !ok {
		t.Fatalf("no request ends on the finish note:\n%s", preemptRequestLog(stub))
	}
	if last := wake.Messages[len(wake.Messages)-1].Content; !strings.Contains(last, bgSubAgentReport) {
		t.Errorf("the wake's note does not carry the child's report %q: %q", bgSubAgentReport, last)
	}

	// /workflows lists it under its name with its origin, and its detail offers no save-as-recipe.
	submit(drv, "/workflows")
	drv.WaitText(bgSubAgentOrigin)
	row = rowContaining(t, drv.Frame(), bgSubAgentOrigin)
	if !strings.Contains(row, "scout") || !strings.Contains(row, bgSubAgentItems) {
		t.Errorf("the /workflows row = %q; want the delegation's name, %q and %q", row, bgSubAgentItems, bgSubAgentOrigin)
	}
	drv.Press(tuitest.Enter)
	drv.WaitText("esc back")
	if text := drv.Frame().String(); strings.Contains(text, "save as recipe") {
		t.Errorf("a background sub_agent's detail offers save-as-recipe:\n%s", drv.Frame())
	}
	// ⏎ on its one stage's row opens its one item.
	drv.Press(tuitest.Enter)
	drv.WaitText(bgSubAgentItemStatus)

	// esc walks the pane back up — item, detail, list — and closes it.
	for range 3 {
		drv.Press(tuitest.Esc)
	}
	drv.WaitFor(func() bool {
		text := drv.Frame().String()
		return !strings.Contains(text, "esc close") && !strings.Contains(text, "esc back")
	},
		tuitest.Awaiting("the /workflows pane to close"))

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// requestEndsOn reports whether some request's last message is the human's text.
func requestEndsOn(stub *stubllm.Server, text string) bool {
	_, ok := preemptParentRequest(stub, text)
	return ok
}

// wakeRequest is the request whose last message is the background sub_agent's finish note.
func wakeRequest(stub *stubllm.Server) (stubllm.Request, bool) {
	for _, req := range stub.Requests() {
		if n := len(req.Messages); n > 0 && strings.Contains(req.Messages[n-1].Content, bgSubAgentNoteLead) {
			return req, true
		}
	}
	return stubllm.Request{}, false
}
