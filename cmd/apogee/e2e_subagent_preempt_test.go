package main

// A queued message pre-empts the sub-agents that have not started (ADR 0025), end to end: the
// model delegates twice, the human types a message while the first child is still working, and
// the second delegation is SKIPPED — an explicit error-shaped tool result in its slot, the message
// committed at the boundary the first child's report brings — instead of the message waiting out
// every queued child.
//
// Every seam is pinned one layer down: the predicate and the skip in internal/agent, the mailbox
// and the Bridge's answer in internal/tui. None of those proves the ROPE — that the composition
// installs the Bridge's predicate as Config.InterjectionPending at all, and that the box the
// human's ⏎ fills is the box the engine reads. This file asserts the rope: a real composition
// talking to a scripted server produces the parent request the design promises, in order, and
// the row on screen that says why.
//
// It runs at `parallel-agents: 1` and therefore drives the serial dispatch only; the pool path is
// pinned by internal/agent's own tests and gets no second journey here (writer decision
// 2026-09-14).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The fixture's own words, restated here so an assertion reads as the claim it is making
// (testdata/stubllm/subagent-preempt.yaml).
const (
	preemptPrompt  = "Delegate two summaries of the workspace."
	preemptMessage = "Also check the tests before you finish."
	preemptWrapUp  = "Message received; the beta half was not started."
	preemptReport  = "The alpha half is two files and a README."
	// The two children's tasks: the first is what the held child's request carries, the second is
	// what NO request may carry — a delegation the human's message pre-empted is never asked.
	preemptFirstTask  = "alpha half of the workspace"
	preemptSecondTask = "beta half of the workspace"
	// The gate the fixture holds the first child's answer on, released once the queued row is on
	// screen — which is what puts the ⏎ strictly before the engine decides on the second delegation.
	preemptChildGate = "staged"
	// The one word the skipped row's outcome slot reads (internal/tui's `error` verdict), and the
	// word it must not: the row is over, not waiting.
	preemptErrorWord     = "error"
	preemptScheduledWord = "scheduled"
	// preemptQueuedReadout is the status line's count of staged messages while one waits.
	preemptQueuedReadout = "1 queued"

	// The engine's whole account of the skipped delegation — internal/agent/dispatch.go's
	// skippedDelegationContent, restated because cmd/apogee cannot import it, which is the point: it
	// is what the model is told, so a rewording over there has to fail here.
	preemptSkipContent = "sub-agent not started: the user sent a message while this group was " +
		"running; delegate again if the task is still needed"
)

// preemptHome writes a one-server home whose entry pins `parallel-agents: 1`, the cap that makes
// the two delegations dispatch serially. It is written whole rather than appended, for
// parallelHome's reason: the pin sits INSIDE the `servers:` entry and no helper can add it after
// the fact.
func preemptHome(t *testing.T, stub *stubllm.Server) string {
	t.Helper()

	body := "servers:\n" +
		"  - name: probe-target\n" +
		"    endpoint: " + stub.URL + "\n" +
		"    model: " + stub.Model + "\n" +
		"    parallel-agents: 1\n" +
		"server: probe-target\n"
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the serial home's config: %v", err)
	}
	return home
}

// TestE2EQueuedMessagePreemptsTheScheduledSubAgents is the journey: two delegations, a message
// queued while the first runs, and the parent's next request carrying — in order — the first
// child's result, the exact skip content as the second tool result, and then the interjected
// message; on screen, the second row collapsed to the error verdict.
func TestE2EQueuedMessagePreemptsTheScheduledSubAgents(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "subagent-preempt"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, preemptHome(t, stub), "")

	submit(drv, preemptPrompt)
	// The first delegation is on screen and its child is held on the gate: the window the human
	// types into.
	drv.WaitText("alpha")

	// ⏎ while running stages the message; the readout is the proof it is in the queue — and, through
	// the Bridge, in front of the engine — before the child is let go.
	submit(drv, preemptMessage)
	drv.WaitText(preemptQueuedReadout)

	stub.Release(preemptChildGate)
	drv.WaitText(preemptWrapUp)
	drv.WaitQuiet(settled)

	// The wire: the parent request that carried the wrap-up's trigger. Its tail is the first
	// child's result, the skip, then the human's message — the order the design promises.
	parent, ok := preemptParentRequest(stub)
	if !ok {
		t.Fatalf("no parent request ends on the queued message; requests:\n%s", preemptRequestLog(stub))
	}
	assertPreemptTail(t, parent)

	// The second child was never asked: no request in the log belongs to its conversation.
	for _, req := range stub.Requests() {
		if carriesTask(req, preemptSecondTask) {
			t.Errorf("request %d carries the second delegation's task; the skipped child was run", req.N)
		}
	}

	// The screen: the skipped row says the verdict, not `scheduled`, and the group is collapsed.
	frame := drv.Frame()
	row := rowContaining(t, frame, "beta")
	if !strings.Contains(row, preemptErrorWord) {
		t.Errorf("the skipped delegation's row does not read %q: %q", preemptErrorWord, row)
	}
	if strings.Contains(flatten(frame.String()), preemptScheduledWord) {
		t.Errorf("a delegation still reads %q after the run settled:\n%s", preemptScheduledWord, frame)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// preemptParentRequest is the parent's request whose LAST message is the queued message — the one
// the wrap-up answered. There is exactly one such request in a run that went as designed.
func preemptParentRequest(stub *stubllm.Server) (stubllm.Request, bool) {
	for _, req := range stub.Requests() {
		msgs := req.Messages
		if len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		if last.Role == "user" && strings.HasPrefix(last.Content, preemptMessage) {
			return req, true
		}
	}
	return stubllm.Request{}, false
}

// assertPreemptTail checks the parent request's last four messages: the assistant turn that issued
// the two sub_agent calls, the first call's result, the second call's skip, and the human's message.
// Order is the claim — results commit in call order and the message at the boundary after them.
func assertPreemptTail(t *testing.T, req stubllm.Request) {
	t.Helper()

	msgs := req.Messages
	if len(msgs) < 4 {
		t.Fatalf("the parent request carries %d messages; want at least the delegating turn, two results and the message", len(msgs))
	}
	issued, first, second, message := msgs[len(msgs)-4], msgs[len(msgs)-3], msgs[len(msgs)-2], msgs[len(msgs)-1]

	if issued.Role != "assistant" || len(issued.ToolCalls) != 2 {
		t.Fatalf("the message before the results is %q with %d tool calls; want the assistant turn with 2", issued.Role, len(issued.ToolCalls))
	}
	if first.Role != "tool" || first.ToolCallID != issued.ToolCalls[0].ID || !strings.Contains(first.Content, preemptReport) {
		t.Errorf("first tool result = %+v; want the first call's report %q", first, preemptReport)
	}
	if second.Role != "tool" || second.ToolCallID != issued.ToolCalls[1].ID {
		t.Errorf("second tool result = %+v; want the second call's slot", second)
	}
	if second.Content != preemptSkipContent {
		t.Errorf("second tool result content = %q; want exactly the skip content %q", second.Content, preemptSkipContent)
	}
	if message.Role != "user" || !strings.HasPrefix(message.Content, preemptMessage) {
		t.Errorf("last message = %+v; want the human's queued message", message)
	}
}

// preemptRequestLog renders the request log for a failure message: one line per request, its
// last message's role and opening.
func preemptRequestLog(stub *stubllm.Server) string {
	var b strings.Builder
	for _, req := range stub.Requests() {
		last := ""
		if n := len(req.Messages); n > 0 {
			m := req.Messages[n-1]
			last = m.Role + ": " + m.Content
		}
		if len(last) > 80 {
			last = last[:80] + "…"
		}
		b.WriteString(strings.Join([]string{"#", strings.TrimSpace(last)}, " "))
		b.WriteString("\n")
	}
	return b.String()
}
