package agent

import (
	"context"
	"errors"
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
//
// The fan-out row of that claim — TestFanOut_ApprovalAndQuestionShareOnePrompt, two children, one
// approving and one asking — was retired on 2026-09-15 (plan 2026-09-14 - 03, item 5): ask_user is
// withheld from every sub-agent, so no child can raise the question half of the pair, and a single
// top-level agent cannot raise a question and an approval at the same instant. The slot stays
// kind-blind and the two remaining tests still drive it with both kinds, one after the other.

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

		waitForQueuedCallers(t, slot, 1, "the question queued behind the visible approval")
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

		waitForQueuedCallers(t, slot, 1, "the approval queued behind the visible question")
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

// ----------------------------------------------------------------------------
// Observing a queued caller (plan 2026-09-22 - 01, item 9)
// ----------------------------------------------------------------------------

// queuedCallerCeiling bounds a poll for a caller to appear inside domain.PromptSlot.Acquire. It is
// deliberately generous: the poll returns the instant the count matches, so the headroom costs a
// healthy run nothing and is only ever spent on a box loaded enough to leave a goroutine
// unscheduled for seconds — which is exactly what the 20ms sleeps it replaced could not survive.
//
// It is NOT the assertion. Each caller of waitForQueuedCallers only reaches its real deadline —
// the 3 * time.Second the test then gives the cancelled caller to answer — once the queue state is
// established, so that deadline keeps its full bite however generous this ceiling is: widening the
// wait for "the caller is queued" never widens the window in which the ctx-aware wait must react.
const queuedCallerCeiling = 10 * time.Second

// waitForQueuedCallers blocks until slot reports want callers inside Acquire, failing the test with
// what — a phrase naming the thing that never queued — if that has not happened within
// queuedCallerCeiling.
//
// Waiting() counts callers INSIDE Acquire, not callers that are stuck, so it must only ever be read
// while a first caller demonstrably holds the slot: every call site here follows a <-entered
// handshake with a blocking delegate, which is what makes a reading of want mean "queued behind the
// visible prompt" rather than merely "somebody called Acquire".
func waitForQueuedCallers(t *testing.T, slot *domain.PromptSlot, want int, what string) {
	t.Helper()

	deadline := time.Now().Add(queuedCallerCeiling)
	for {
		got := slot.Waiting()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s: Waiting() was %d after %s, want %d", what, got, queuedCallerCeiling, want)
		}
		time.Sleep(time.Millisecond)
	}
}
