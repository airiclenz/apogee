package floor

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// loopResponse builds a post-response working value over a conversation history — the shape
// ToolLoopBreak reads, which unlike the repair guard needs the history through
// resp.View().Conversation(). The view is a real domain request view, so the scans behave exactly
// as they do in the loop.
func loopResponse(history []domain.Message, calls ...domain.ToolCall) *domain.Response {
	view := domain.NewRequest("m", history, nil, domain.Budget{}, 0).View()
	finish := domain.FinishStop
	if len(calls) > 0 {
		finish = domain.FinishToolCalls
	}
	return domain.NewResponse("", "", calls, finish, view)
}

// A response repeating the previous turn's exact tool calls draws the loop-breaking directive
// (apogee-sim detectToolCallLoop + retryWithToolLoopDirective @pin), which names the repeated tool
// and credits the file already written. The A-B-A-B alternation rule has its own cases below.
func TestToolLoopBreakOnIdenticalRepeat(t *testing.T) {
	t.Parallel()

	history := []domain.Message{
		domaintest.UserMessage("build the thing"),
		domaintest.AssistantCallsMessage(writeCall("w1", "a.go")),
		domaintest.ToolResultMessage("w1", "ok"),
	}
	directive, ok := ToolLoopBreak(loopResponse(history, writeCall("w2", "a.go")))

	if !ok {
		t.Fatal("ToolLoopBreak returned ok = false on an identical repeat")
	}
	if !strings.Contains(directive, "in a loop") {
		t.Errorf("directive = %q, want the loop-breaking wording", directive)
	}
	if !strings.Contains(directive, "write_file") {
		t.Errorf("directive = %q, want it to name the repeated tool", directive)
	}
	if !strings.Contains(directive, "a.go") {
		t.Errorf("directive = %q, want it to credit the file already written", directive)
	}
	if !strings.Contains(directive, "build the thing") {
		t.Errorf("directive = %q, want it to restate the user's task", directive)
	}
}

// The repeat is order-independent: the same set of calls issued in the other order is still the
// repeat it is (computeToolCallKey sorts before comparing).
func TestToolLoopBreakIgnoresCallOrder(t *testing.T) {
	t.Parallel()

	first, second := writeCall("w1", "a.go"), writeCall("w2", "b.go")
	history := []domain.Message{
		domaintest.UserMessage("build the thing"),
		domaintest.AssistantCallsMessage(first, second),
		domaintest.ToolResultMessage("w1", "ok"),
	}
	if _, ok := ToolLoopBreak(loopResponse(history, second, first)); !ok {
		t.Error("ToolLoopBreak returned ok = false on a reordered but identical set of calls")
	}
}

// Different calls, no calls at all, and a first tool-call turn with nothing to loop against are
// each a no-op — the guard books nothing and the response stands.
func TestToolLoopBreakIsANoOpWithoutARepeat(t *testing.T) {
	t.Parallel()

	prior := []domain.Message{
		domaintest.UserMessage("build the thing"),
		domaintest.AssistantCallsMessage(writeCall("w1", "a.go")),
		domaintest.ToolResultMessage("w1", "ok"),
	}
	cases := []struct {
		name    string
		history []domain.Message
		calls   []domain.ToolCall
	}{
		{"different calls", prior, []domain.ToolCall{writeCall("w2", "b.go")}},
		{"no previous tool-call turn", []domain.Message{domaintest.UserMessage("build the thing")}, []domain.ToolCall{writeCall("w1", "a.go")}},
		{"no tool calls in the response", prior, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if directive, ok := ToolLoopBreak(loopResponse(tc.history, tc.calls...)); ok {
				t.Errorf("ToolLoopBreak = (%q, true), want no directive", directive)
			}
		})
	}
}

// With nothing written and nothing read to credit, the directive still lands — it takes the
// different-action tail instead of a file recap, so a repeat with no file activity is never silent.
func TestToolLoopBreakFallsBackToTheDifferentActionTail(t *testing.T) {
	t.Parallel()

	listCall := domaintest.Call("l1", "list_files", map[string]string{"pattern": "*.go"})
	history := []domain.Message{
		domaintest.UserMessage("what is here"),
		domaintest.AssistantCallsMessage(listCall),
		domaintest.ToolResultMessage("l1", "a.go"),
	}
	directive, ok := ToolLoopBreak(loopResponse(history, listCall))

	if !ok {
		t.Fatal("ToolLoopBreak returned ok = false on an identical repeat with no file activity")
	}
	if !strings.Contains(directive, toolLoopDifferentAction) {
		t.Errorf("directive = %q, want it to end with the different-action tail %q", directive, toolLoopDifferentAction)
	}
}

// The write branch of the directive's file recap counts an apogee EDIT tool into filesWritten, so
// the directive credits an edit_existing_file / single_find_and_replace as work already done and
// steers toward what remains rather than back to write_file. It holds only because
// isFileMutatingTool counts apogee's own edit menu, not just the sim spellings; the identical
// read-repeat of b.go is what trips the guard.
func TestToolLoopBreakCreditsAnEditToolWrite(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"edit_existing_file", "single_find_and_replace"} {
		t.Run(tool, func(t *testing.T) {
			t.Parallel()

			history := []domain.Message{
				domaintest.UserMessage("update a.go"),
				domaintest.AssistantCallsMessage(domaintest.Call("e1", tool, map[string]string{"path": "a.go"})),
				domaintest.ToolResultMessage("e1", "wrote a.go"),
				domaintest.AssistantCallsMessage(domaintest.ReadCall("r1", "b.go")),
				domaintest.ToolResultMessage("r1", "package b"),
			}
			// The response repeats the previous turn's exact read call.
			directive, ok := ToolLoopBreak(loopResponse(history, domaintest.ReadCall("r2", "b.go")))

			if !ok {
				t.Fatal("ToolLoopBreak returned ok = false on the identical repeat")
			}
			if !strings.Contains(directive, "already written: a.go") {
				t.Errorf("directive = %q, want it to credit the %s write of a.go", directive, tool)
			}
		})
	}
}

// The repeat scan is bounded to the CURRENT Exchange: a user who asks for the same thing again
// opens a new Exchange, and its first call — byte-identical to the last call of the previous one —
// is the work just asked for, not a loop. Answering it with the directive would steer the model off
// that work, which is the one thing a Floor guard may never do.
func TestToolLoopBreakDoesNotFireAcrossAnExchangeBoundary(t *testing.T) {
	t.Parallel()

	history := []domain.Message{
		domaintest.UserMessage("write a.go"),
		domaintest.AssistantCallsMessage(writeCall("w1", "a.go")),
		domaintest.ToolResultMessage("w1", "ok"),
		domaintest.AssistantTextMessage("done"),
		domaintest.UserMessage("write a.go again"), // a NEW Exchange opens here
	}
	if directive, ok := ToolLoopBreak(loopResponse(history, writeCall("w2", "a.go"))); ok {
		t.Errorf("ToolLoopBreak = (%q, true) on a re-ask, want no directive", directive)
	}
}

// An INTERJECTION is not an Exchange opening (domain.CurrentExchange skips it), so a remark dropped
// into the running Exchange leaves the repeat in scope: the guard still fires on the loop the
// interjection landed in the middle of.
func TestToolLoopBreakStillFiresAcrossAnInterjection(t *testing.T) {
	t.Parallel()

	history := []domain.Message{
		domaintest.UserMessage("write a.go"),
		domaintest.AssistantCallsMessage(writeCall("w1", "a.go")),
		domaintest.ToolResultMessage("w1", "ok"),
		{Role: domain.RoleUser, Content: "also check the tests", Interjected: true},
	}
	if _, ok := ToolLoopBreak(loopResponse(history, writeCall("w2", "a.go"))); !ok {
		t.Error("ToolLoopBreak returned ok = false; an interjection does not open an Exchange")
	}
}

// The directive's recap is the current Exchange's too: it restates the request THIS Exchange opened
// with and credits only the files this Exchange touched. A recap drawn from the whole conversation
// restates a task the user has moved on from and credits work the current request never asked for.
func TestToolLoopBreakRecapsTheCurrentExchangeOnly(t *testing.T) {
	t.Parallel()

	readB := domaintest.ReadCall("r1", "b.go")
	history := []domain.Message{
		domaintest.UserMessage("write a.go"),
		domaintest.AssistantCallsMessage(writeCall("w1", "a.go")),
		domaintest.ToolResultMessage("w1", "ok"),
		domaintest.AssistantTextMessage("done"),
		domaintest.UserMessage("now read b.go"), // the current Exchange opens
		domaintest.AssistantCallsMessage(readB),
		domaintest.ToolResultMessage("r1", "package b"),
	}
	directive, ok := ToolLoopBreak(loopResponse(history, domaintest.ReadCall("r2", "b.go")))

	if !ok {
		t.Fatal("ToolLoopBreak returned ok = false on an identical repeat inside one Exchange")
	}
	if !strings.Contains(directive, "now read b.go") {
		t.Errorf("directive = %q, want it to restate this Exchange's request", directive)
	}
	if strings.Contains(directive, "write a.go") {
		t.Errorf("directive = %q, want no trace of the PREVIOUS Exchange's request", directive)
	}
	if strings.Contains(directive, "a.go") {
		t.Errorf("directive = %q, want no credit for a file the previous Exchange wrote", directive)
	}
	if !strings.Contains(directive, "b.go") {
		t.Errorf("directive = %q, want it to credit the file this Exchange read", directive)
	}
}

// alternationHistory is the A-B-A Exchange body the A-B-A-B rule closes on: the two tool Turns
// aCall and bCall alternating, with aCall answered by aResults[0] the first time and aResults[1]
// the second, so a test decides whether A's output stalled or moved between its two runs.
func alternationHistory(aCall, bCall domain.ToolCall, aResults [2]string) []domain.Message {
	first, second := aCall, aCall
	first.ID, second.ID = "a1", "a2"
	middle := bCall
	middle.ID = "b1"
	return []domain.Message{
		domaintest.UserMessage("finish the task"),
		domaintest.AssistantCallsMessage(first),
		domaintest.ToolResultMessage("a1", aResults[0]),
		domaintest.AssistantCallsMessage(middle),
		domaintest.ToolResultMessage("b1", "nothing to do"),
		domaintest.AssistantCallsMessage(second),
		domaintest.ToolResultMessage("a2", aResults[1]),
	}
}

// An exact A-B-A-B alternation over the Exchange's last four tool Turns — byte-identical keys AND
// byte-identical results for the repeated first member — is the loop the immediate-repeat rule
// could never see (review 28b8d620: sub_agent(noop) / task_list for fourteen calls). The fourth
// call draws the directive, which names the tool it repeats.
func TestToolLoopBreakOnAnAlternatingRepeat(t *testing.T) {
	t.Parallel()

	taskList := domaintest.Call("t", "task_list", map[string]string{})
	noop := domaintest.Call("s", "sub_agent", map[string]string{"task": "noop"})
	history := alternationHistory(taskList, noop, [2]string{"1. [ ] finish", "1. [ ] finish"})
	directive, ok := ToolLoopBreak(loopResponse(history, noop))

	if !ok {
		t.Fatal("ToolLoopBreak returned ok = false on an exact A-B-A-B alternation with an identical task_list result")
	}
	if !strings.Contains(directive, "in a loop") || !strings.Contains(directive, "sub_agent") {
		t.Errorf("directive = %q, want the loop-breaking wording naming sub_agent", directive)
	}
}

// The alternation rule needs the repeated pair's results to be byte-identical: a poll whose output
// moves — console_read or read_file between other calls while a build runs (ADR 0059) — is
// progress, not a loop, however identical its keys are.
func TestToolLoopBreakLetsAMovingPollThrough(t *testing.T) {
	t.Parallel()

	build := domaintest.Call("b", "run_command", map[string]string{"command": "make"})
	for _, poll := range []domain.ToolCall{
		domaintest.Call("p", "console_read", map[string]string{"id": "dev"}),
		domaintest.ReadCall("p", "build.log"),
	} {
		t.Run(poll.Tool, func(t *testing.T) {
			t.Parallel()

			history := alternationHistory(poll, build, [2]string{"compiling 3/10", "compiling 7/10"})
			if directive, ok := ToolLoopBreak(loopResponse(history, build)); ok {
				t.Errorf("ToolLoopBreak = (%q, true) on a poll whose output moved, want no directive", directive)
			}
		})
	}
}

// The alternation rule matches byte-identical keys only: A-B-A-C (a different fourth call) and
// A-B-A'-B (the repeated call with changed arguments) are not the pattern, whatever the results.
func TestToolLoopBreakAlternationNeedsExactKeys(t *testing.T) {
	t.Parallel()

	taskList := domaintest.Call("t", "task_list", map[string]string{})
	noop := domaintest.Call("s", "sub_agent", map[string]string{"task": "noop"})
	cases := []struct {
		name    string
		history []domain.Message
		now     domain.ToolCall
	}{
		{
			name:    "A-B-A-C",
			history: alternationHistory(taskList, noop, [2]string{"same", "same"}),
			now:     domaintest.Call("c", "sub_agent", map[string]string{"task": "write the report"}),
		},
		{
			name: "A-B-A'-B with changed args",
			history: func() []domain.Message {
				h := alternationHistory(taskList, noop, [2]string{"same", "same"})
				h[5] = domaintest.AssistantCallsMessage(domaintest.Call("a2", "task_list", map[string]string{"filter": "open"}))
				return h
			}(),
			now: noop,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if directive, ok := ToolLoopBreak(loopResponse(tc.history, tc.now)); ok {
				t.Errorf("ToolLoopBreak = (%q, true), want no directive", directive)
			}
		})
	}
}

// The alternation rule reads four tool Turns: an Exchange holding only A-B has no -3 to match the
// response's A against, so it never fires — the immediate-repeat rule is the only one that can fire
// this early, and the Turns differ.
func TestToolLoopBreakAlternationNeedsFourToolTurns(t *testing.T) {
	t.Parallel()

	taskList := domaintest.Call("a1", "task_list", map[string]string{})
	history := []domain.Message{
		domaintest.UserMessage("finish the task"),
		domaintest.AssistantCallsMessage(taskList),
		domaintest.ToolResultMessage("a1", "same"),
		domaintest.AssistantCallsMessage(domaintest.Call("b1", "sub_agent", map[string]string{"task": "noop"})),
		domaintest.ToolResultMessage("b1", "nothing to do"),
	}
	if directive, ok := ToolLoopBreak(loopResponse(history, taskList)); ok {
		t.Errorf("ToolLoopBreak = (%q, true) on a three-Turn Exchange, want no directive", directive)
	}
}

// The immediate repeat still fires on its own terms — results are not consulted: A-A after an
// A-B-A run is the repeat it always was, even though its two results differ.
func TestToolLoopBreakImmediateRepeatIgnoresResults(t *testing.T) {
	t.Parallel()

	taskList := domaintest.Call("t", "task_list", map[string]string{})
	noop := domaintest.Call("s", "sub_agent", map[string]string{"task": "noop"})
	history := alternationHistory(taskList, noop, [2]string{"before", "after"})
	if _, ok := ToolLoopBreak(loopResponse(history, taskList)); !ok {
		t.Error("ToolLoopBreak returned ok = false on an immediate repeat, want the directive whatever the results")
	}
}
