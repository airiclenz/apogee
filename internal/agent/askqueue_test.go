package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/stubllm"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// ask_user is the top-level agent's alone (ADR 0039 decision 12, narrowed 2026-09-15)
// ----------------------------------------------------------------------------
//
// The free-text twin of approvalqueue_test.go — with one half retired. Until 2026-09-15 ask_user
// was registered for sub-agents (defaultSubAgentTools passed the parent's whole menu down), so a
// depth-0 fan-out could put two children's questions to the human at once and these tests pinned
// that they queued on the one prompt surface, each naming its child. Plan 2026-09-14 - 03, item 5
// (owner call, 2026-09-14) withholds ask_user — and present_document — from EVERY child: a
// delegation has no seat at the human's prompt, so the question a child would have asked is
// reported in its result for the parent to put. What survives here is the floor that was always
// true — a top-level question names no sub-agent — and its new counterpart: a child that emits the
// call anyway meets an unknown tool, and the host Asker never hears from it.

// askProbeAsker is a host Asker that ASSUMES it is never called concurrently — the promise
// domain.Asker makes — and measures the assumption instead of guarding it: the counters are atomic
// so an overlap reads as a number rather than a corrupt one, and seen is appended WITHOUT a lock, so
// `go test -race` fails outright the instant two children get in together. Each call holds its
// "prompt" open for hold, which is the window a queued sibling would collide in.
type askProbeAsker struct {
	inFlight atomic.Int32
	overlaps atomic.Int32
	seen     []domain.AskRequest // unguarded on purpose: the race detector is the assertion
	hold     time.Duration
	answer   func(domain.AskRequest) string
}

func (a *askProbeAsker) Ask(_ context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	if a.inFlight.Add(1) != 1 {
		a.overlaps.Add(1)
	}
	a.seen = append(a.seen, req)
	time.Sleep(a.hold)
	a.inFlight.Add(-1)

	return domain.AskAnswer{Text: a.answer(req)}, nil
}

// askUserCallScript emits one ask_user call putting question to the human — the Delta script a
// routedResponder plays; askUserCallTurn is its stubllm twin.
func askUserCallScript(id, question string) []provider.Delta {
	return toolCallScript(id, "ask_user", `{"question":"`+question+`"}`)
}

// askUserCallTurn is a turn that emits one ask_user call putting question to the human.
func askUserCallTurn(id, question string) stubllm.Turn {
	return toolCallTurn(id, "ask_user", `{"question":"`+question+`"}`)
}

// TestSubAgent_ChildQuestionIsRefusedAndNeverReachesTheHuman is the retired fan-out case turned
// around: a child that calls ask_user anyway finds no such tool on its menu — the roster it was
// spawned with withholds it — so the call resolves as an unknown tool inside the delegation and the
// host Asker is never called. The parent's own conversation is untouched by the attempt: it still
// gets the child's final words as the delegation's result.
func TestSubAgent_ChildQuestionIsRefusedAndNeverReachesTheHuman(t *testing.T) {
	sink := &recordingSink{}
	asker := &askProbeAsker{answer: func(domain.AskRequest) string { return "the blue one" }}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, tools.NewAskUser(asker))
	// The tool-call repair Floor guard would answer an off-menu call before the unknown-tool
	// result this test is about; it is off here, as in TestSubAgent_SubsetCannotCallOmittedTool.
	cfg.Floor.DisableToolCallRepair = true

	up := newRoutedResponder().
		route("delegate one thing", nil, fanOutScript([2]string{"c1", "the only task"})).
		route("the only task", nil, askUserCallScript("q1", "which one?")).
		route("the only task", nil, contentScript("child done")).
		route("delegate one thing", nil, contentScript("parent done"))

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate one thing"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(asker.seen) != 0 {
		t.Fatalf("the human was asked %d questions by a child, want none — ask_user is withheld from every sub-agent", len(asker.seen))
	}
	results := childToolResults(sink.events)["c1"]
	if len(results) != 1 || !results[0].IsError || !strings.Contains(results[0].Content, "unknown tool") {
		t.Errorf("the child's ask_user call resolved to %+v, want an unknown-tool error result", results)
	}
	if res, ok := lastSubAgentResult(sink.events); !ok || res.IsError || !strings.Contains(res.Content, "child done") {
		t.Errorf("the delegation's result = %+v, want the child's own final words", res)
	}
}

// TestAskRequest_TopLevelNamesNoSubAgent pins the other floor: the top-level agent is the only thing
// that could be asking, so its question carries no task and its prompt is unchanged.
func TestAskRequest_TopLevelNamesNoSubAgent(t *testing.T) {
	sink := &recordingSink{}
	asker := &askProbeAsker{answer: func(domain.AskRequest) string { return "yes" }}
	cfg := configWithTools(sink, tools.NewAskUser(asker))

	up := scriptedResponder(t,
		askUserCallTurn("q1", "shall I?"),
		contentTurn("done"),
	)
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "ask me something"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(asker.seen) != 1 {
		t.Fatalf("the human was asked %d times, want once", len(asker.seen))
	}
	if got := asker.seen[0].SubAgentTask; got != "" {
		t.Errorf("a top-level question named sub-agent task %q, want none", got)
	}
	if got := asker.seen[0].Question; !strings.Contains(got, "shall I?") {
		t.Errorf("the question reached the host as %q, want the model's own text", got)
	}
}
