package main

// A message sent NOW pre-empts the sub-agents that have not started (ADR 0025, amended 2026-10-05),
// end to end: the model delegates twice, the human sends a message with ctrl+g while the first child
// is still working, and the second delegation is SKIPPED — an explicit error-shaped tool result in
// its slot, the message committed at the boundary the first child's report brings — instead of the
// message waiting out every queued child. A message sent with ⏎ is the other journey: it waits for
// the whole wave, the second child included, and lands after both reports
// (TestE2EQueuedMessageWaitsForTheWholeWave).
//
// Every seam is pinned one layer down: the predicate and the skip in internal/agent, the mailbox
// and the Bridge's answer in internal/tui. None of those proves the ROPE — that the composition
// installs the Bridge's predicate as Config.InterjectionPending at all, and that the box the
// human's ctrl+g fills is the box the engine reads — and that a ⏎ in the same box leaves it unread. This file asserts the rope: a real composition
// talking to a scripted server produces the parent request the design promises, in order, and
// the row on screen that says why.
//
// The journey runs at `parallel-agents: 1` and therefore drives the serial dispatch; the pool's
// skip rule is pinned by internal/agent's own tests (writer decision 2026-09-14). The pool gets two
// journeys of its own here for what ONLY a pool shows the human, because only a pool draws a
// queued member's row before it starts: a skipped row standing on screen before its group's results
// burst, which must open its reason in place rather than an empty run view
// (TestE2EQueuedMessagePreemptsAPooledSubAgentAndItsRowOpensInPlace), and the queued readout naming
// the wave a ⏎ message waits for (TestE2EQueuedMessageWaitsForAPooledWaveAndSaysSo).

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

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
	// The verdict the skipped row's outcome slot reads (internal/tui's neutral pre-emption verdict), and the
	// word it must not: the row is over, not waiting.
	preemptErrorWord     = "not started · your message"
	preemptScheduledWord = "scheduled"
	// preemptQueuedReadout is the status line's count of staged messages while one waits.
	preemptQueuedReadout = "1 queued"

	// The finish-the-wave journey's own words: a message sent with ⏎, the wrap-up it triggers once
	// both children have reported, and the second child's report — the fixture answers that child
	// only for this journey, since the pre-empt journey never asks it.
	waveMessage = "Also review the docs once both halves are in."
	waveWrapUp  = "Message received after both halves."
	waveReport  = "The beta half is the test suite."
	// waveReadout is the queued readout while a ⏎ message waits out a wave's queued member: the
	// count, when it lands, and the key that would send it now instead (internal/tui's
	// queuedWaveHint). A pooled wave shows its queued member as a `scheduled` row; a serial wave draws
	// no row for a member it has not started, and the readout counts that member off the size the
	// engine announced for the group — so both journeys show it.
	waveReadout = "1 queued · after the wave · ctrl+g sends now"

	// The engine's whole account of the skipped delegation — internal/agent/dispatch.go's
	// skippedDelegationContent, restated because cmd/apogee cannot import it, which is the point: it
	// is what the model is told, so a rewording over there has to fail here.
	preemptSkipContent = "sub-agent not started: the user sent a message while this group was " +
		"running; delegate again if the task is still needed"
)

// preemptHome writes a one-server home whose entry pins `parallel-agents:` to agents — 1 is the cap
// that makes the delegations dispatch serially, 2 the pool the pre-burst journey needs. It is
// written whole rather than appended, for parallelHome's reason: the pin sits INSIDE the
// `servers:` entry and no helper can add it after the fact.
func preemptHome(t *testing.T, stub *stubllm.Server, agents int) string {
	t.Helper()

	body := "servers:\n" +
		"  - name: probe-target\n" +
		"    endpoint: " + stub.URL + "\n" +
		"    model: " + stub.Model + "\n" +
		"    parallel-agents: " + strconv.Itoa(agents) + "\n" +
		"server: probe-target\n"
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.yaml"), []byte(body), 0o600); err != nil {
		t.Fatalf("write the serial home's config: %v", err)
	}
	return home
}

// TestE2EQueuedMessagePreemptsTheScheduledSubAgents is the journey: two delegations, a message
// sent now (ctrl+g) while the first runs, and the parent's next request carrying — in order — the
// first child's result, the exact skip content as the second tool result, and then the interjected
// message; on screen, the second row collapsed to the neutral skip verdict.
func TestE2EQueuedMessagePreemptsTheScheduledSubAgents(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "subagent-preempt"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, preemptHome(t, stub, 1), "")

	submit(drv, preemptPrompt)
	// The first delegation is on screen and its child is held on the gate: the window the human
	// types into.
	drv.WaitText("alpha")

	// ctrl+g while running stages the message now; the readout is the proof it is in the queue — and,
	// through the Bridge, in front of the engine — before the child is let go.
	submitNow(drv, preemptMessage)
	drv.WaitText(preemptQueuedReadout)

	stub.Release(preemptChildGate)
	drv.WaitText(preemptWrapUp)
	drv.WaitQuiet(settled)

	// The wire: the parent request that carried the wrap-up's trigger. Its tail is the first
	// child's result, the skip, then the human's message — the order the design promises.
	parent, ok := preemptParentRequest(stub, preemptMessage)
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

// TestE2EQueuedMessageWaitsForTheWholeWave is the default send's journey (ADR 0025, amended
// 2026-10-05): the same two delegations at `parallel-agents: 1`, a message sent with ⏎ while the
// first child runs — and the readout says the message waits for the wave and names the key that
// would not, though the second delegation has no row yet. The second child is NOT skipped: it is
// asked and reports, and the parent's next request carries both reports, in call order, and then
// the message.
func TestE2EQueuedMessageWaitsForTheWholeWave(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "subagent-preempt"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, preemptHome(t, stub, 1), "")

	submit(drv, preemptPrompt)
	drv.WaitText("alpha")

	// ⏎ while running stages an ordinary message, and the readout counts it and names the wave.
	submit(drv, waveMessage)
	drv.WaitText(waveReadout)

	stub.Release(preemptChildGate)
	drv.WaitText(waveWrapUp)
	drv.WaitQuiet(settled)

	parent, ok := preemptParentRequest(stub, waveMessage)
	if !ok {
		t.Fatalf("no parent request ends on the queued message; requests:\n%s", preemptRequestLog(stub))
	}
	assertWaveTail(t, parent)

	asked := false
	for _, req := range stub.Requests() {
		if carriesTask(req, preemptSecondTask) {
			asked = true
		}
	}
	if !asked {
		t.Errorf("no request carries the second delegation's task; the ⏎ message skipped it:\n%s", preemptRequestLog(stub))
	}

	frame := drv.Frame()
	if row := rowContaining(t, frame, "beta"); strings.Contains(row, preemptErrorWord) {
		t.Errorf("the second delegation's row reads %q after a ⏎ message; want it run: %q", preemptErrorWord, row)
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// TestE2EQueuedMessageWaitsForAPooledWaveAndSaysSo is the wave rule at `parallel-agents: 2`, where
// the queued member stands on screen as `scheduled` before it starts: a message sent with ⏎ while
// the first two run makes the queued readout say the message waits for the wave and name the key
// that would not, and the third delegation still runs once a slot frees.
func TestE2EQueuedMessageWaitsForAPooledWaveAndSaysSo(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "subagent-preempt-pool"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, preemptHome(t, stub, 2), "")

	submit(drv, poolPreemptPrompt)
	drv.WaitText(poolSkippedName)

	submit(drv, preemptMessage)
	drv.WaitText(waveReadout)

	stub.Release(poolStagedGate)
	stub.Release(poolHeldGate)
	drv.WaitFor(func() bool {
		_, ok := preemptParentRequest(stub, preemptMessage)
		return ok
	}, tuitest.Awaiting("the parent's request carrying the queued message"))
	drv.WaitQuiet(settled)

	asked := false
	for _, req := range stub.Requests() {
		if carriesTask(req, poolSkippedTask) {
			asked = true
		}
	}
	if !asked {
		t.Errorf("no request carries the queued delegation's task; the ⏎ message skipped it:\n%s", preemptRequestLog(stub))
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// assertWaveTail checks the parent request's last four messages after a ⏎ message waited out the
// wave: the delegating turn, the first child's report, the second child's report — not a skip — and
// the human's message, committed at the boundary both results close.
func assertWaveTail(t *testing.T, req stubllm.Request) {
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
	if second.Role != "tool" || second.ToolCallID != issued.ToolCalls[1].ID || !strings.Contains(second.Content, waveReport) {
		t.Errorf("second tool result = %+v; want the second call's report %q, not a skip", second, waveReport)
	}
	if message.Role != "user" || !strings.HasPrefix(message.Content, waveMessage) {
		t.Errorf("last message = %+v; want the human's queued message", message)
	}
}

// submitNow is [submit] with the now send: it types a line into the prompt box, waits for it there
// for submit's reason, and presses ctrl+g instead of ⏎.
func submitNow(drv driven, text string) {
	drv.Type(text)
	drv.WaitFor(func() bool {
		rows, ok := drv.Frame().PromptBox()
		return ok && strings.Contains(strings.Join(rows, "\n"), promptTail(text))
	}, tuitest.Within(tuitest.DefaultTimeout+time.Duration(len(text))*typingAllowance),
		tuitest.Awaiting("the typed prompt to appear in the prompt box"))
	drv.Press(tuitest.CtrlG)
}

// The pool fixture's own words (testdata/stubllm/subagent-preempt-pool.yaml).
const (
	poolPreemptPrompt  = "Delegate three summaries of the workspace."
	poolSkippedName    = "gamma"
	poolSkippedTask    = "gamma half of the workspace"
	poolStagedGate     = "staged" // holds the second child until the message is queued
	poolHeldGate       = "held"   // holds the first child until the skipped row has been expanded
	poolRunViewCrumbAt = "← main ›"
)

// TestE2EQueuedMessagePreemptsAPooledSubAgentAndItsRowOpensInPlace is the pre-burst expand: three
// delegations at `parallel-agents: 2`, a message queued while the first two run, the third skipped
// at its dequeue once the second reports — and, with the first child still held so the group has
// not joined, a click on the skipped row. The row is over (its finished phase is in) but not yet
// paired with its result, and a child that never ran has no run to show: the click must open the
// skip's own words in place and push no run view.
func TestE2EQueuedMessagePreemptsAPooledSubAgentAndItsRowOpensInPlace(t *testing.T) {
	t.Parallel()

	stub := stubllm.New(t, loadScript(t, "subagent-preempt-pool"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIOn(t, drv, stub, preemptHome(t, stub, 2), "")

	submit(drv, poolPreemptPrompt)
	drv.WaitText(poolSkippedName)

	submitNow(drv, preemptMessage)
	drv.WaitText(preemptQueuedReadout)

	// The second child reports; its worker dequeues the third delegation with the message pending,
	// and the row reads the verdict while the first child is still held.
	stub.Release(poolStagedGate)
	drv.WaitFor(func() bool {
		f := drv.Frame()
		_, y, ok := f.Find(poolSkippedName)
		return ok && strings.Contains(f.Row(y), preemptErrorWord)
	}, tuitest.Awaiting("the skipped row to read its verdict before the burst"))

	frame := drv.Frame()
	x, y, ok := frame.Find(poolSkippedName)
	if !ok {
		t.Fatalf("the skipped row left the screen:\n%s", frame)
	}
	click(drv, x, y)
	drv.WaitText("sub-agent not started")

	if opened := drv.Frame(); strings.Contains(opened.String(), poolRunViewCrumbAt) {
		t.Errorf("expanding the skipped row before the burst opened a run view:\n%s", opened)
	}

	// The click left the viewport standing on the opened row rather than following the tail, so the
	// wrap-up is awaited on the wire: the parent's request that ends on the queued message.
	stub.Release(poolHeldGate)
	drv.WaitFor(func() bool {
		_, ok := preemptParentRequest(stub, preemptMessage)
		return ok
	}, tuitest.Awaiting("the parent's request carrying the queued message"))
	drv.WaitQuiet(settled)

	for _, req := range stub.Requests() {
		if carriesTask(req, poolSkippedTask) {
			t.Errorf("request %d carries the skipped delegation's task; the skipped child was run", req.N)
		}
	}

	if err := sess.Quit(); err != nil {
		t.Fatalf("the run returned %v; want a clean quit", err)
	}
}

// preemptParentRequest is the parent's request whose LAST message is the queued message — the one
// the wrap-up answered. There is exactly one such request in a run that went as designed.
func preemptParentRequest(stub *stubllm.Server, message string) (stubllm.Request, bool) {
	for _, req := range stub.Requests() {
		msgs := req.Messages
		if len(msgs) == 0 {
			continue
		}
		last := msgs[len(msgs)-1]
		if last.Role == "user" && strings.HasPrefix(last.Content, message) {
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
