package main

// The one-run stop end to end (ADR 0086 D4, D5): a human opens the run view of a delegate that is
// hanging, reads the key that stops it off the view's own header, presses ^x, and the parent's
// Turn goes on to its final answer with the folded partial result in hand.
//
// Every earlier test of the stop drives one seam — Agent.StopChild in internal/agent, the key and
// the verdict in internal/tui. This one drives the whole rope: the key has to reach the engine
// with THAT run's id, the engine has to cut the child's hanging request, fold what it did and hand
// the parent a result under the stopped head, and the parent has to keep going rather than end
// its Turn with the child. Asserted semantically; the look of the view is t17's golden.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The wordings this run asserts on, restated because cmd/apogee cannot import them: the verdict a
// stopped run's row wears (internal/tui's toolregistry.go, delegationStoppedVerdict) and the head
// the parent's result opens on (internal/agent's subagent.go, stoppedResultHead). A rename over
// there has to fail here.
const (
	stopPrompt    = "Delegate a survey and let me stop it."
	stopDelegate  = "survey"
	stopTask      = "Survey every file in the workspace"
	stopCrumb     = "← main › " + stopDelegate
	stopFold      = "Engine fold: the delegate read a.txt before it was stopped."
	stopFinal     = "The delegate was stopped; it had read a.txt, which says hello."
	stoppedByYou  = "stopped by you"
	stoppedHead   = "[stopped by the user — engine summary follows]"
	stopKeyInHint = "^x stop"
)

// TestE2ESubAgentStop drives the stop in one session: the view of a hanging delegate offers ^x,
// ^x stops that run alone, its row settles as stopped by you, and the parent reads the stopped
// result and answers.
func TestE2ESubAgentStop(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "delegate-stop"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUI(t, drv, stub)
	waitIdle(drv)
	drv.WaitQuiet(settled)

	submit(drv, stopPrompt)

	// The child is HANGING before anything is pressed: its second request, the one the script never
	// answers, has reached the server. From here the run cannot finish on its own, so whatever ends
	// it is the stop.
	drv.WaitFor(func() bool { return childHangs(stub, stopTask) },
		tuitest.Awaiting("the child's second request, which the script answers only with a hang"))

	// The view of the working run offers the stop, and the test reads the key from the frame rather
	// than assuming it.
	openRunNamed(drv, stopCrumb)
	opened := frameWhen(t, drv, "the run view of the hanging child, offering the stop",
		func(f tuitest.Frame) bool { return holds(f, stopCrumb) && holds(f, stopKeyInHint) })
	if crumb := strings.TrimRight(rowContaining(t, opened, stopCrumb), " "); !strings.HasSuffix(crumb, stopKeyInHint) {
		t.Fatalf("the breadcrumb of a working run does not end in %q: %q", stopKeyInHint, crumb)
	}

	// ^x — no confirmation. The hint goes once the run is over, since there is nothing left to stop.
	drv.Press(tuitest.CtrlX)
	stopped := frameWhen(t, drv, "the viewed run to end, dropping the stop from its breadcrumb",
		func(f tuitest.Frame) bool { return holds(f, stopCrumb) && !holds(f, stopKeyInHint) })
	if crumb := strings.TrimRight(rowContaining(t, stopped, stopCrumb), " "); !strings.HasSuffix(crumb, runViewHint) {
		t.Errorf("the breadcrumb of the stopped run should offer the way back alone: %q", crumb)
	}

	// The parent's Turn goes on: it reads the stopped result and answers.
	drv.Press(tuitest.Esc)
	drv.WaitGone(stopCrumb)
	drv.WaitText(stopFinal)
	waitIdle(drv)
	drv.WaitQuiet(settled)

	top := drv.Frame()
	// The member row under the umbrella settles on the verdict of a run the human stopped.
	if row := rowContaining(t, top, "┕ "+stopDelegate); !strings.Contains(row, stoppedByYou) {
		t.Errorf("the stopped delegation's member row does not read %q: %q", stoppedByYou, row)
	}

	// What the parent read: a result under the stopped head, carrying the engine fold written at the
	// stop — a non-error partial result, not a cancel of the parent's Turn.
	req, result, ok := stoppedResult(stub)
	if !ok {
		t.Fatalf("no request the parent sent carries a tool result opening on %q", stoppedHead)
	}
	if !strings.Contains(result, stopFold) {
		t.Errorf("the stopped result does not carry the engine fold %q:\n%s", stopFold, result)
	}
	if carriesTask(req, stopTask) {
		t.Errorf("the request carrying the stopped result belongs to the child's conversation, not the parent's")
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// openRunNamed is [openWorkingRun] for a run whose breadcrumb is crumb: ⌥↑ then ⏎, waiting on the
// view's own header rather than on a settled screen, since a working run never goes quiet.
func openRunNamed(drv *tuitest.Driver, crumb string) {
	painted := drv.Screen().BytesWritten()
	drv.Press(tuitest.AltUp)
	drv.WaitFor(func() bool { return drv.Screen().BytesWritten() > painted },
		tuitest.Awaiting("the block cursor to highlight a block"))
	drv.Press(tuitest.Enter)
	drv.WaitText(crumb)
}

// childHangs reports whether the child handed task has sent the request that follows its read —
// the one the fixture answers only with a hang.
func childHangs(stub *stubllm.Server, task string) bool {
	for _, req := range stub.Requests() {
		if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "tool" && carriesTask(req, task) {
			return true
		}
	}
	return false
}

// stoppedResult is the first request carrying a tool result that opens on the stopped head, and
// that result's content with any engine note fenced onto its tail cut off.
func stoppedResult(stub *stubllm.Server) (stubllm.Request, string, bool) {
	for _, req := range stub.Requests() {
		for _, msg := range req.Messages {
			if msg.Role == "tool" && strings.HasPrefix(msg.Content, stoppedHead) {
				return req, resultBody(msg.Content), true
			}
		}
	}
	return stubllm.Request{}, "", false
}
