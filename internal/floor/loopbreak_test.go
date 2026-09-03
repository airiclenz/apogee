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
	view := domain.NewRequest("m", history, nil, domain.Budget{}, 0, nil).View()
	finish := domain.FinishStop
	if len(calls) > 0 {
		finish = domain.FinishToolCalls
	}
	return domain.NewResponse("", "", calls, finish, view)
}

// A response repeating the previous turn's exact tool calls draws the loop-breaking directive
// (apogee-sim detectToolCallLoop + retryWithToolLoopDirective @pin), which names the repeated tool
// and credits the file already written.
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
