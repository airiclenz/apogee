package main

// The task list end to end (ADR 0072): the model writes its checklist down with task_list, and the
// list is re-rendered into the standing system content of every request that follows — including the
// requests of a session that was quit and resumed with `--continue`.
//
// Every seam below this is pinned one layer down — the list and its render in internal/tasklist, the
// tool in internal/tools, the block's composition in internal/agent, the card in internal/tui. None
// of them proves the ROPE, and the rope is the whole point of the feature: a block composed onto a
// message nobody sends, or a list held somewhere a snapshot never reads, would pass every one of
// those and still leave a long run with no idea what is left. So this run asks the question the way
// the model meets it — what did the upstream actually RECEIVE — and asks it twice, once inside the
// session that wrote the list and once inside the session that restored it.

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tuitest"
)

// The prompts tasklist.yaml answers and the wording each run is waited on. The BLOCK's own header is
// never spelled here: cmd/apogee is package main and cannot import internal/agent, so the fence
// comes off the root facade (apogee.TaskListFence) the way seatFallbackNoteText and
// apogee.DelegateReportBlock do — a copy retyped into a test goes on passing after the engine's text
// has changed.
const (
	taskListPrompt = "Plan the parser work and write it down."
	taskListWrapUp = "The list is written down."
	// taskListResumePrompt is sent AFTER the `--continue` relaunch. A resumed session issues no
	// request until it is asked something, so without a second prompt there is no wire to look at.
	taskListResumePrompt = "What is still open?"
	taskListResumeReply  = "Two tasks are still open."
	// taskListDoneRow is the exact row internal/tasklist renders for the one finished task in the
	// fixture's call — marker and text, as the model reads it. Asserting the ROW rather than the
	// task's words is what makes this a claim about the render reaching the wire rather than about
	// the fixture's own spelling surviving a round trip.
	taskListDoneRow = "[✔] wire the parser seam"
	// taskListCardLabel is the presenter's own label for a task_list call (internal/tui's
	// toolRegistry), so a frame carrying it is a call that was PRESENTED rather than merely made.
	taskListCardLabel = "Task List"
	// taskListStandingPrompt pins this run's own system prompt, for reportStandingPrompt's reason:
	// the block rides along on standing content that already exists, so a run whose prompt is the
	// embedded default would be asserting against wording the default is free to change.
	taskListStandingPrompt = "system-prompt-text: |\n  You are apogee, a terminal coding agent.\n"
)

// TestE2ETaskListReachesTheWireAndSurvivesAResume drives one session that writes a three-task list,
// checks the block on the request that follows the call, quits, resumes the same session with
// `--continue`, and checks the block again on the request the resumed session's first prompt makes.
func TestE2ETaskListReachesTheWireAndSurvivesAResume(t *testing.T) {
	stub := stubllm.New(t, loadScript(t, "tasklist"))
	drv := tuitest.NewDriver(t, e2eSize)
	sess := launchTUIConfigured(t, drv, stub, taskListStandingPrompt)

	// The call is made, presented, and answered. The wrap-up turn is keyed on the task_list tool
	// result, so a frame carrying it is a list that actually reached the engine — which is what
	// keeps the block assertions below from passing over a run that never called the tool.
	submit(drv, taskListPrompt)
	drv.WaitText(taskListCardLabel)
	drv.WaitText(taskListWrapUp)
	drv.WaitQuiet(settled)

	written := taskListRequestAfterAResult(t, stub.Requests())
	assertCarriesTheTaskList(t, written, "the request following the task_list result")

	// The resume. The session record is written on the way out, so the run has to END before it can
	// be reopened — and the reopened run is asked something, because a session that is merely
	// restored sends nothing at all.
	if err := sess.Quit(); err != nil {
		t.Fatalf("the first run returned %v; want a clean quit", err)
	}
	before := len(stub.Requests())

	next := sess.RelaunchWith("--continue")
	next.WaitText("Send a message")
	submit(next, taskListResumePrompt)
	next.WaitText(taskListResumeReply)
	next.WaitQuiet(settled)

	resumed := taskListRequestCarrying(t, stub.Requests()[before:], taskListResumePrompt)
	assertCarriesTheTaskList(t, resumed, "the resumed session's request")

	stub.AssertConsumed(t)
	if err := sess.Quit(); err != nil {
		t.Fatalf("the resumed run returned %v; want a clean quit", err)
	}
}

// assertCarriesTheTaskList fails t unless req's system content holds the block's fence and the
// rendered done row. The two are asserted together on purpose: a fence with no row is a header the
// engine composed over an empty list, and a row with no fence is text that reached the model without
// the sentence telling it whose list it is.
func assertCarriesTheTaskList(t *testing.T, req stubllm.Request, what string) {
	t.Helper()

	system := seatSystemText(req)
	if !strings.Contains(system, apogee.TaskListFence) {
		t.Errorf("%s (request %d) carries no task list block — no %q in its system content:\n%s",
			what, req.N, apogee.TaskListFence, system)
	}
	if !strings.Contains(system, taskListDoneRow) {
		t.Errorf("%s (request %d) carries no %q row; the list did not reach the wire as rendered:\n%s",
			what, req.N, taskListDoneRow, system)
	}
}

// taskListRequestAfterAResult is the first request whose last message is a tool RESULT — the round
// apogee makes once the call has run, and so the first one whose standing content could carry the
// list the call just wrote.
func taskListRequestAfterAResult(t *testing.T, reqs []stubllm.Request) stubllm.Request {
	t.Helper()

	for _, req := range reqs {
		if len(req.Messages) == 0 {
			continue
		}
		if req.Messages[len(req.Messages)-1].ToolCallID != "" {
			return req
		}
	}
	t.Fatalf("no request carried a tool result; the run made %d requests and none of them followed "+
		"a call", len(reqs))
	return stubllm.Request{}
}

// taskListRequestCarrying is the first request whose last message is the given prompt — how the
// resumed session's own round is told from the title call and from anything a Mechanism asked.
func taskListRequestCarrying(t *testing.T, reqs []stubllm.Request, prompt string) stubllm.Request {
	t.Helper()

	for _, req := range reqs {
		if len(req.Messages) == 0 {
			continue
		}
		if strings.Contains(req.Messages[len(req.Messages)-1].Content, prompt) {
			return req
		}
	}
	t.Fatalf("no request of the resumed session ended on %q; it made %d requests", prompt, len(reqs))
	return stubllm.Request{}
}
