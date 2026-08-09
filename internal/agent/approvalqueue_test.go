package agent

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/provider"
)

// ----------------------------------------------------------------------------
// Queued approvals (ADR 0039 decision 12 — one prompt at a time, named by its child)
// ----------------------------------------------------------------------------
//
// Concurrency does not change what an Approval IS; it changes how many can be raised at once and
// how little the call alone says about who raised it. These tests pin both halves: the host
// Approver still sees one request at a time however many children are running, and every request a
// sub-agent raises carries that sub-agent's delegated task.

// queueProbeApprover is a host Approver that ASSUMES it is never called concurrently — the promise
// domain.Approver makes — and measures the assumption instead of guarding it: the counters are
// atomic so an overlap reads as a number rather than a corrupt one, and seen is appended WITHOUT a
// lock, so `go test -race` fails outright the instant two children get in together. Each call holds
// its "prompt" open for hold, which is the window a queued sibling would collide in.
type queueProbeApprover struct {
	inFlight atomic.Int32
	overlaps atomic.Int32
	seen     []domain.ApprovalRequest // unguarded on purpose: the race detector is the assertion
	hold     time.Duration
	allow    func(domain.ApprovalRequest) bool
}

func (q *queueProbeApprover) Approve(_ context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	if q.inFlight.Add(1) != 1 {
		q.overlaps.Add(1)
	}
	q.seen = append(q.seen, req)
	time.Sleep(q.hold)
	q.inFlight.Add(-1)

	if q.allow(req) {
		return domain.ApprovalAllow, nil
	}
	return domain.ApprovalDeny, nil
}

// tasksSeen returns the delegated task each request named, in the order the human was asked.
func (q *queueProbeApprover) tasksSeen() []string {
	out := make([]string, 0, len(q.seen))
	for _, req := range q.seen {
		out = append(out, req.SubAgentTask)
	}
	return out
}

// childToolResults maps each child's spawning call-ID to the tool results IT produced, read off the
// depth-1 stream by the call-ID item 3 stamps. It is how a test asks "what did THIS child see" once
// two children's events are braided into one sink.
func childToolResults(events []domain.Event) map[string][]domain.ToolResult {
	out := map[string][]domain.ToolResult{}
	for _, e := range events {
		re, ok := e.(domain.ToolResultEvent)
		if !ok || re.Depth != 1 {
			continue
		}
		out[re.CallID] = append(out[re.CallID], re.Result)
	}
	return out
}

// TestFanOut_ConcurrentChildApprovalsQueueAndNameTheirChild is the item in one run: two children
// run at once (the probe's peak), both reach an Approval gate, and the host Approver still fields
// their requests ONE AT A TIME — each naming its own child's task, each answered separately, and
// each verdict reaching the child that asked for it.
func TestFanOut_ConcurrentChildApprovalsQueueAndNameTheirChild(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	approver := &queueProbeApprover{
		// Long enough that an unserialized second child would still be inside when the first is:
		// the two are released from the probe at the same instant and reach the gate microseconds
		// apart.
		hold: 100 * time.Millisecond,
		// The verdict is routed BY THE TASK, so an answer delivered to the wrong child shows up as
		// the wrong tool running rather than as a bookkeeping mismatch.
		allow: func(req domain.ApprovalRequest) bool { return req.SubAgentTask == "task one" },
	}

	ran := 0
	cfg := subAgentConfig(sink, domain.ModeAskBefore, fakeTool{name: "touch_thing", ran: &ran, result: "touched"})
	cfg.Approver = approver
	cfg.ParallelAgents = 2

	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", probe.enter, toolCallScript("t1", "touch_thing", `{}`)).
		route("task one", nil, contentScript("child one done")).
		route("task two", probe.enter, toolCallScript("t2", "touch_thing", `{}`)).
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
	if n := approver.overlaps.Load(); n != 0 {
		t.Errorf("%d overlapping Approve calls reached the host; requests must queue one at a time", n)
	}

	tasks := approver.tasksSeen()
	if len(tasks) != 2 {
		t.Fatalf("the human was asked %d times (%v), want once per child", len(tasks), tasks)
	}
	if !(tasks[0] == "task one" && tasks[1] == "task two") && !(tasks[0] == "task two" && tasks[1] == "task one") {
		t.Errorf("prompts named %v, want one prompt per child naming its own task", tasks)
	}

	results := childToolResults(sink.events)
	one, two := results["c1"], results["c2"]
	if len(one) != 1 || len(two) != 1 {
		t.Fatalf("child tool results = %d for c1 and %d for c2, want one each", len(one), len(two))
	}
	if one[0].IsError || !strings.Contains(one[0].Content, "touched") {
		t.Errorf("the approved child's tool result = %+v, want the tool to have run", one[0])
	}
	if !two[0].IsError || !strings.Contains(two[0].Content, "denied") {
		t.Errorf("the denied child's tool result = %+v, want a denial", two[0])
	}
	if ran != 1 {
		t.Errorf("the gated tool ran %d times, want 1 — the deny must have stopped the other child's call", ran)
	}
}

// TestApprovalRequest_TopLevelNamesNoSubAgent pins the floor: the top-level agent is the only thing
// that could be asking, so its request carries no task and its prompt is unchanged.
func TestApprovalRequest_TopLevelNamesNoSubAgent(t *testing.T) {
	sink := &recordingSink{}
	approver := &queueProbeApprover{allow: func(domain.ApprovalRequest) bool { return true }}
	cfg := configWithTools(sink, fakeTool{name: "touch_thing", result: "touched"})
	cfg.Mode = domain.ModeAskBefore
	cfg.Approver = approver

	up := &scriptedResponder{scripts: [][]provider.Delta{
		toolCallScript("t1", "touch_thing", `{}`),
		contentScript("done"),
	}}
	a, err := newAgent(cfg, up)
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if err := a.Submit(domain.UserInput{Text: "touch it"}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if _, err := a.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(approver.seen) != 1 {
		t.Fatalf("the human was asked %d times, want once", len(approver.seen))
	}
	if got := approver.seen[0].SubAgentTask; got != "" {
		t.Errorf("a top-level request named sub-agent task %q, want none", got)
	}
}

// blockingApprover parks inside Approve until it is released — a stand-in for a human who has not
// answered yet, which is the only state a queue behind it can be observed in.
type blockingApprover struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingApprover) Approve(context.Context, domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	b.calls.Add(1)
	b.entered <- struct{}{}
	<-b.release
	return domain.ApprovalAllow, nil
}

// TestQueuedApprover_QueuedRequestAnswersItsOwnCancellation pins why the seam is a channel and not a
// mutex: a sibling waiting behind the visible prompt must be able to give up when the human cancels
// the Turn, WITHOUT the request it was carrying ever reaching the driver.
func TestQueuedApprover_QueuedRequestAnswersItsOwnCancellation(t *testing.T) {
	inner := &blockingApprover{entered: make(chan struct{}, 1), release: make(chan struct{})}
	q := queuedApprovals(inner)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		if _, err := q.Approve(context.Background(), domain.ApprovalRequest{Tool: "visible"}); err != nil {
			t.Errorf("the visible prompt returned %v, want it answered", err)
		}
	}()
	<-inner.entered // the first request now holds the slot

	ctx, cancel := context.WithCancel(context.Background())
	type answer struct {
		decision domain.ApprovalDecision
		err      error
	}
	queued := make(chan answer, 1)
	go func() {
		d, err := q.Approve(ctx, domain.ApprovalRequest{Tool: "queued"})
		queued <- answer{d, err}
	}()

	time.Sleep(20 * time.Millisecond) // long enough for the second request to be genuinely waiting
	cancel()

	select {
	case got := <-queued:
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("a cancelled queued request returned err %v, want context.Canceled", got.err)
		}
		if got.decision != domain.ApprovalDeny {
			t.Errorf("a cancelled queued request returned %q, want the safe verdict %q", got.decision, domain.ApprovalDeny)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a queued request never answered its own cancellation — the wait is not ctx-aware")
	}
	if n := inner.calls.Load(); n != 1 {
		t.Errorf("the host Approver saw %d requests, want 1 — a request for a rolled-back Turn reached it", n)
	}

	close(inner.release)
	<-firstDone
}

// TestQueuedApprovals_OneSlotPerAgentTree pins the wrapping rules: nil stays nil (the resolver reads
// "no Approver configured" as a fact), the wrap is idempotent, and a child shares its parent's ONE
// slot rather than opening a private, uncontended one at every depth.
func TestQueuedApprovals_OneSlotPerAgentTree(t *testing.T) {
	if got := queuedApprovals(nil); got != nil {
		t.Errorf("queuedApprovals(nil) = %#v, want nil — a wrapped nil claims a human gate that does not exist", got)
	}

	inner := &fakeApprover{decision: domain.ApprovalAllow}
	once := queuedApprovals(inner)
	if again := queuedApprovals(once); again != once {
		t.Error("wrapping an already-queued Approver stacked a second slot in front of the first")
	}

	sink := &recordingSink{}
	cfg := subAgentConfig(sink, domain.ModeAskBefore)
	cfg.Approver = inner
	parent, err := newAgent(cfg, &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent: %v", err)
	}
	if parent.cfg.Approver == domain.Approver(inner) {
		t.Fatal("construction did not install the queueing seam")
	}
	child, err := parent.newChildAgent("c1", "a delegated task", "")
	if err != nil {
		t.Fatalf("newChildAgent: %v", err)
	}
	if child.cfg.Approver != parent.cfg.Approver {
		t.Error("the child got its own slot; a parent and its descendants must queue against one")
	}
	if child.task != "a delegated task" {
		t.Errorf("child.task = %q, want the delegated task the prompt names it by", child.task)
	}

	bare, err := newAgent(subAgentConfig(sink, domain.ModeAskBefore), &scriptedResponder{})
	if err != nil {
		t.Fatalf("newAgent (no Approver): %v", err)
	}
	if bare.cfg.Approver != nil {
		t.Error("a nil Approver was wrapped into a non-nil one; the ladder would see a gate it cannot open")
	}
}
