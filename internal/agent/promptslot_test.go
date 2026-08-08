package agent

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/tools"
)

// ----------------------------------------------------------------------------
// One prompt slot, both kinds (ADR 0039 decision 12 — a Driver draws ONE prompt)
// ----------------------------------------------------------------------------
//
// approvalqueue_test.go pins that approvals queue against approvals, and askqueue_test.go that
// questions queue against questions. Neither pins the pair, and the pair is where a Driver actually
// breaks: the approval pane and the ask_user box are the SAME surface, so a child's approval and a
// sibling's question raised at the same instant would replace one another and orphan a reply
// channel — leaving that child blocked until the Turn was cancelled. These tests pin the surface
// itself: one prompt in front of the human at a time, whatever kind each is.

// promptSurfaceProbe is a host that implements BOTH prompt delegates over ONE surface — which is
// what a Driver is: one pane, one keyboard. It ASSUMES it is never called concurrently through
// either of them (the promise domain.Approver and domain.Asker both make) and measures the
// assumption instead of guarding it: the counters are atomic so an overlap reads as a number rather
// than a corrupt one, and seen is appended WITHOUT a lock, so `go test -race` fails outright the
// instant two prompts of ANY kinds get in together. Each call holds the surface for hold, which is
// the window a queued sibling would collide in.
type promptSurfaceProbe struct {
	inFlight atomic.Int32
	overlaps atomic.Int32
	seen     []string // unguarded on purpose: the race detector is the assertion
	hold     time.Duration
}

// occupy is the one prompt surface both delegates below draw on.
func (p *promptSurfaceProbe) occupy(kind, task string) {
	if p.inFlight.Add(1) != 1 {
		p.overlaps.Add(1)
	}
	p.seen = append(p.seen, kind+": "+task)
	time.Sleep(p.hold)
	p.inFlight.Add(-1)
}

func (p *promptSurfaceProbe) Approve(_ context.Context, req domain.ApprovalRequest) (domain.ApprovalDecision, error) {
	p.occupy("approval", req.SubAgentTask)
	return domain.ApprovalAllow, nil
}

func (p *promptSurfaceProbe) Ask(_ context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	p.occupy("question", req.SubAgentTask)
	return domain.AskAnswer{Text: "answer for " + req.SubAgentTask}, nil
}

// prompts returns what reached the human, sorted, so the assertion does not depend on which of two
// genuinely concurrent children won the slot first.
func (p *promptSurfaceProbe) prompts() []string {
	out := append([]string(nil), p.seen...)
	sort.Strings(out)
	return out
}

// TestFanOut_ApprovalAndQuestionShareOnePrompt is the item in one run: two children run at once (the
// concurrency probe's peak), one reaches an Approval gate while the other calls ask_user, and the
// human's ONE prompt surface still fields them one at a time — each naming its own child, each
// answered separately, and each answer reaching the child that asked for it.
func TestFanOut_ApprovalAndQuestionShareOnePrompt(t *testing.T) {
	sink := &recordingSink{}
	probe := newConcurrencyProbe(2, 3*time.Second)
	// Long enough that an unserialized second prompt would still be on the surface while the first
	// is: the two children are released from the probe at the same instant and reach their gates
	// microseconds apart.
	surface := &promptSurfaceProbe{hold: 100 * time.Millisecond}

	ran := 0
	cfg := subAgentConfig(sink, domain.ModeAskBefore,
		fakeTool{name: "touch_thing", ran: &ran, result: "touched"},
		tools.NewAskUser(surface))
	cfg.Approver = surface
	cfg.ParallelAgents = 2

	up := newRoutedResponder().
		route("delegate two things", nil, fanOutScript([2]string{"c1", "task one"}, [2]string{"c2", "task two"})).
		route("task one", probe.enter, toolCallScript("t1", "touch_thing", `{}`)).
		route("task one", nil, contentScript("child one done")).
		route("task two", probe.enter, askUserCallScript("q1", "which one?")).
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
	if n := surface.overlaps.Load(); n != 0 {
		t.Errorf("%d prompts overlapped on the human's one surface; an approval and a question must queue against each other", n)
	}

	want := []string{"approval: task one", "question: task two"}
	if got := surface.prompts(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("the human saw %v, want %v — one prompt per child, each naming its own task", got, want)
	}

	results := childToolResults(sink.events)
	one, two := results["c1"], results["c2"]
	if len(one) != 1 || len(two) != 1 {
		t.Fatalf("child tool results = %d for c1 and %d for c2, want one each", len(one), len(two))
	}
	if one[0].IsError || !strings.Contains(one[0].Content, "touched") {
		t.Errorf("the approved child's tool result = %+v, want the tool to have run", one[0])
	}
	if two[0].IsError || two[0].Content != "answer for task two" {
		t.Errorf("the asking child's tool result = %+v, want its OWN answer", two[0])
	}
	if ran != 1 {
		t.Errorf("the gated tool ran %d times, want 1", ran)
	}
}

// blockingAsker parks inside Ask until it is released — a stand-in for a human who has not answered
// yet, which is the only state a queue behind them can be observed in. It is the free-text twin of
// approvalqueue_test.go's blockingApprover.
type blockingAsker struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	b.calls.Add(1)
	b.entered <- struct{}{}
	<-b.release
	return domain.AskAnswer{Text: "answered"}, nil
}

// askUserCall is the tool call the ask_user tool is driven with directly, below.
var askUserCall = domain.ToolCall{ID: "q1", Tool: "ask_user", Arguments: []byte(`{"question":"which one?"}`)}

// TestPromptSlot_CancelledTurnReleasesEitherKindOfWaiter pins the cross-kind half of the ctx-aware
// wait: whichever kind of prompt is queued behind the visible one, cancelling the Turn releases it
// — and the request it was carrying never reaches the human, because the Turn it belonged to has
// already rolled back. The two seams live in different packages, so this is the only place the pair
// can be exercised; it drives them directly rather than through a fan-out so the "queued" state is
// deterministic instead of raced.
func TestPromptSlot_CancelledTurnReleasesEitherKindOfWaiter(t *testing.T) {
	t.Run("a question queued behind an approval", func(t *testing.T) {
		slot := domain.NewPromptSlot()
		held := domain.WithPromptSlot(context.Background(), slot)

		approver := &blockingApprover{entered: make(chan struct{}, 1), release: make(chan struct{})}
		visible := queuedApprovals(approver)
		firstDone := make(chan struct{})
		go func() {
			defer close(firstDone)
			_, _ = visible.Approve(held, domain.ApprovalRequest{Tool: "visible"})
		}()
		<-approver.entered // the approval now holds the human's one surface

		asker := &blockingAsker{entered: make(chan struct{}, 1), release: make(chan struct{})}
		tool := tools.NewAskUser(asker)
		ctx, cancel := context.WithCancel(held)
		queued := make(chan error, 1)
		go func() {
			_, err := tool.Execute(ctx, askUserCall)
			queued <- err
		}()

		time.Sleep(20 * time.Millisecond) // long enough for the question to be genuinely waiting
		cancel()

		select {
		case err := <-queued:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("the queued question returned %v, want context.Canceled — the loop rolls the Turn back on it", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("a question queued behind an approval never answered its own cancellation")
		}
		if n := asker.calls.Load(); n != 0 {
			t.Errorf("the host Asker saw %d questions, want 0 — a question for a rolled-back Turn reached the human", n)
		}

		close(approver.release)
		<-firstDone
	})

	t.Run("an approval queued behind a question", func(t *testing.T) {
		slot := domain.NewPromptSlot()
		held := domain.WithPromptSlot(context.Background(), slot)

		asker := &blockingAsker{entered: make(chan struct{}, 1), release: make(chan struct{})}
		tool := tools.NewAskUser(asker)
		firstDone := make(chan struct{})
		go func() {
			defer close(firstDone)
			_, _ = tool.Execute(held, askUserCall)
		}()
		<-asker.entered // the question now holds the human's one surface

		approver := &blockingApprover{entered: make(chan struct{}, 1), release: make(chan struct{})}
		queued := queuedApprovals(approver)
		ctx, cancel := context.WithCancel(held)
		type answer struct {
			decision domain.ApprovalDecision
			err      error
		}
		got := make(chan answer, 1)
		go func() {
			d, err := queued.Approve(ctx, domain.ApprovalRequest{Tool: "queued"})
			got <- answer{d, err}
		}()

		time.Sleep(20 * time.Millisecond) // long enough for the approval to be genuinely waiting
		cancel()

		select {
		case a := <-got:
			if !errors.Is(a.err, context.Canceled) {
				t.Errorf("the queued approval returned %v, want context.Canceled", a.err)
			}
			if a.decision != domain.ApprovalDeny {
				t.Errorf("the queued approval returned %q, want the safe verdict %q", a.decision, domain.ApprovalDeny)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("an approval queued behind a question never answered its own cancellation")
		}
		if n := approver.calls.Load(); n != 0 {
			t.Errorf("the host Approver saw %d requests, want 0 — a request for a rolled-back Turn reached the human", n)
		}

		close(asker.release)
		<-firstDone
	})
}

// TestStep_DesignatesOnePromptSlotForTheWholeTree pins the carrier: a Step designates its Agent's
// prompt surface on the context every tool call runs under, and a sub-agent's Steps — which run
// under a context derived from their parent's — keep the one already there rather than opening a
// private, uncontended one. It is what makes "one prompt at a time" hold across depths as well as
// across kinds, and what lets a tool built long before the Agent (by a host assembling its own
// registry) still queue on the tree's slot.
func TestStep_DesignatesOnePromptSlotForTheWholeTree(t *testing.T) {
	sink := &recordingSink{}
	// Appended from the tool. The parent's call completes before the delegation starts and the
	// child's before the delegation joins, so the two writes are ordered — `-race` says so.
	var seen []*domain.PromptSlot
	record := fakeTool{name: "record_slot", readOnly: true,
		execute: func(ctx context.Context, call domain.ToolCall) (domain.ToolResult, error) {
			seen = append(seen, domain.PromptSlotFromContext(ctx))
			return domain.ToolResult{CallID: call.ID, Content: "recorded"}, nil
		}}
	cfg := subAgentConfig(sink, domain.ModeAskBefore, record)

	up := newRoutedResponder().
		route("delegate one thing", nil, toolCallScript("t0", "record_slot", `{}`)).
		route("delegate one thing", nil, fanOutScript([2]string{"c1", "the only task"})).
		route("the only task", nil, toolCallScript("t1", "record_slot", `{}`)).
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

	if len(seen) != 2 {
		t.Fatalf("the tool ran %d times, want twice (once at each depth)", len(seen))
	}
	if seen[0] == nil {
		t.Fatal("a top-level tool call ran with no prompt slot designated; nothing would queue")
	}
	if seen[0] != a.prompts {
		t.Error("the Step designated a slot other than its own Agent's")
	}
	if seen[1] != seen[0] {
		t.Error("a sub-agent's Step replaced the tree's prompt slot; siblings would stop queueing against each other")
	}
}
