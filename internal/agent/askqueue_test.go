package agent

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// Queued ask_user questions (ADR 0039 decision 12 — one prompt at a time, named by its child)
// ----------------------------------------------------------------------------
//
// The free-text twin of approvalqueue_test.go. ask_user is registered for sub-agents by default
// (defaultSubAgentTools passes the parent's whole menu down), so once depth-0 delegations fan out,
// two children can put a question to the human at the same instant — and a Driver with ONE prompt
// surface would keep the second and orphan the first child's reply channel. These tests pin both
// halves of the fix: the host Asker still sees one question at a time however many children are
// running, and every question a sub-agent raises carries that sub-agent's delegated task.

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

// tasksSeen returns the delegated task each question named, in the order the human was asked.
func (a *askProbeAsker) tasksSeen() []string {
	out := make([]string, 0, len(a.seen))
	for _, req := range a.seen {
		out = append(out, req.SubAgentTask)
	}
	return out
}

// askUserCallScript emits one ask_user call putting question to the human.
func askUserCallScript(id, question string) []provider.Delta {
	return toolCallScript(id, "ask_user", `{"question":"`+question+`"}`)
}

// TestFanOut_ConcurrentChildQuestionsQueueAndNameTheirChild is the item in one run: two children run
// at once (the probe's peak), both call ask_user, and the host Asker still fields their questions ONE
// AT A TIME — each naming its own child's task, each answered separately, and each answer reaching
// the child that asked for it rather than the sibling whose request replaced it.
func TestFanOut_ConcurrentChildQuestionsQueueAndNameTheirChild(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	asker := &askProbeAsker{
		// Long enough that an unserialized second child would still be inside when the first is:
		// the two are released from the probe at the same instant and reach ask_user microseconds
		// apart.
		hold: 100 * time.Millisecond,
		// The answer is derived FROM the asking child, so an answer delivered to the wrong child
		// shows up as the wrong text in that child's tool result rather than as a count mismatch.
		answer: func(req domain.AskRequest) string { return "answer for " + req.SubAgentTask },
	}

	cfg := subAgentConfig(sink, domain.ModeAskBefore, tools.NewAskUser(asker))
	cfg.ParallelAgents = 2

	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", probe.enter, askUserCallScript("q1", "which one?")).
		route("task one", nil, contentScript("child one done")).
		route("task two", probe.enter, askUserCallScript("q2", "which one?")).
		route("task two", nil, contentScript("child two done")).
		route("delegate two things", nil, contentScript("parent done"))

	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "delegate two things"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	res, err := a.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Status != domain.StatusExchangeComplete {
		t.Fatalf("parent status = %q, want the Exchange to complete", res.Status)
	}

	if peak := probe.peakInFlight(); peak != 2 {
		t.Fatalf("peak children in flight = %d, want 2 (nothing concurrent was exercised)", peak)
	}
	if n := asker.overlaps.Load(); n != 0 {
		t.Errorf("%d overlapping Ask calls reached the host; questions must queue one at a time", n)
	}

	tasks := asker.tasksSeen()
	if len(tasks) != 2 {
		t.Fatalf("the human was asked %d times (%v), want once per child", len(tasks), tasks)
	}
	if !(tasks[0] == "task one" && tasks[1] == "task two") && !(tasks[0] == "task two" && tasks[1] == "task one") {
		t.Errorf("questions named %v, want one question per child naming its own task", tasks)
	}

	results := childToolResults(sink.events)
	one, two := results["c1"], results["c2"]
	if len(one) != 1 || len(two) != 1 {
		t.Fatalf("child tool results = %d for c1 and %d for c2, want one each", len(one), len(two))
	}
	if one[0].IsError || one[0].Content != "answer for task one" {
		t.Errorf("child one's ask_user result = %+v, want its OWN answer", one[0])
	}
	if two[0].IsError || two[0].Content != "answer for task two" {
		t.Errorf("child two's ask_user result = %+v, want its OWN answer", two[0])
	}
}

// TestSubAgent_SingleDelegatedQuestionIsUnchanged is the serial floor beside the fan-out above: one
// child, one question, no queue to observe — it is answered exactly as it was before the seam
// existed, and it names the one child that could have asked it.
func TestSubAgent_SingleDelegatedQuestionIsUnchanged(t *testing.T) {
	sink := &recordingSink{}
	asker := &askProbeAsker{answer: func(domain.AskRequest) string { return "the blue one" }}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, tools.NewAskUser(asker))

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

	if got := asker.tasksSeen(); len(got) != 1 || got[0] != "the only task" {
		t.Fatalf("the human was asked %v, want one question naming the delegated task", got)
	}
	results := childToolResults(sink.events)["c1"]
	if len(results) != 1 || results[0].IsError || results[0].Content != "the blue one" {
		t.Errorf("the child's ask_user result = %+v, want the human's answer", results)
	}
}

// TestAskRequest_TopLevelNamesNoSubAgent pins the other floor: the top-level agent is the only thing
// that could be asking, so its question carries no task and its prompt is unchanged.
func TestAskRequest_TopLevelNamesNoSubAgent(t *testing.T) {
	sink := &recordingSink{}
	asker := &askProbeAsker{answer: func(domain.AskRequest) string { return "yes" }}
	cfg := configWithTools(sink, tools.NewAskUser(asker))

	up := &scriptedResponder{scripts: [][]provider.Delta{
		askUserCallScript("q1", "shall I?"),
		contentScript("done"),
	}}
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
