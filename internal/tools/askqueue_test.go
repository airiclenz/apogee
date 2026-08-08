package tools

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// The ask_user queueing seam (ADR 0039 decision 12 — one question on the screen at a time)
// ----------------------------------------------------------------------------
//
// The behavioural counterpart of these is in internal/agent (askqueue_test.go), where real children
// run concurrently through a real Agent. What is pinned HERE is the seam's own contract, which only
// this package can reach: nil stays nil, a re-wrap re-uses the one slot, and a queued caller answers
// its own cancellation instead of parking until the question ahead of it is answered.

// blockingAsker parks inside Ask until it is released — a stand-in for a human who has not answered
// yet, which is the only state a queue behind them can be observed in.
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

// plainAsker answers immediately; it exists for the wrapping rules, which never reach a human.
type plainAsker struct{}

func (plainAsker) Ask(context.Context, domain.AskRequest) (domain.AskAnswer, error) {
	return domain.AskAnswer{}, nil
}

// TestQueuedAsker_QueuedQuestionAnswersItsOwnCancellation pins why the seam is a channel and not a
// mutex: a sibling waiting behind the visible question must be able to give up when the human
// cancels the Turn, WITHOUT the question it was carrying ever reaching the driver.
func TestQueuedAsker_QueuedQuestionAnswersItsOwnCancellation(t *testing.T) {
	inner := &blockingAsker{entered: make(chan struct{}, 1), release: make(chan struct{})}
	q := queuedAsks(inner)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		if _, err := q.Ask(context.Background(), domain.AskRequest{Question: "visible"}); err != nil {
			t.Errorf("the visible question returned %v, want it answered", err)
		}
	}()
	<-inner.entered // the first question now holds the slot

	ctx, cancel := context.WithCancel(context.Background())
	type reply struct {
		answer domain.AskAnswer
		err    error
	}
	queued := make(chan reply, 1)
	go func() {
		ans, err := q.Ask(ctx, domain.AskRequest{Question: "queued"})
		queued <- reply{ans, err}
	}()

	time.Sleep(20 * time.Millisecond) // long enough for the second question to be genuinely waiting
	cancel()

	select {
	case got := <-queued:
		if !errors.Is(got.err, context.Canceled) {
			t.Errorf("a cancelled queued question returned err %v, want context.Canceled", got.err)
		}
		if got.answer.Text != "" {
			t.Errorf("a cancelled queued question returned answer %q, want none", got.answer.Text)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a queued question never answered its own cancellation — the wait is not ctx-aware")
	}
	if n := inner.calls.Load(); n != 1 {
		t.Errorf("the host Asker saw %d questions, want 1 — a question for a rolled-back Turn reached it", n)
	}

	close(inner.release)
	<-firstDone
}

// TestQueuedAsks_WrappingRules pins the two rules a shared slot rests on: nil stays nil (the tool
// reads "no Asker configured" as a fact), and the wrap is idempotent, so a delegate that has already
// been through the seam is not handed a second, private, uncontended slot in front of the first.
func TestQueuedAsks_WrappingRules(t *testing.T) {
	if got := queuedAsks(nil); got != nil {
		t.Errorf("queuedAsks(nil) = %#v, want nil — a wrapped nil claims a human that is not reachable", got)
	}

	once := queuedAsks(plainAsker{})
	if again := queuedAsks(once); again != once {
		t.Error("wrapping an already-queued Asker stacked a second slot in front of the first")
	}
}

// TestNewAskUser_InstallsTheQueueingSeam pins WHERE the seam lives: at the tool, so every registry —
// the engine's own and the one cmd/apogee builds to register MCP tools on top — gets it from the one
// constructor they share. A nil delegate is still nil afterwards, so Execute's unavailable answer
// stands rather than becoming a call into a forwarder.
func TestNewAskUser_InstallsTheQueueingSeam(t *testing.T) {
	if _, queued := NewAskUser(plainAsker{}).asker.(*queuedAsker); !queued {
		t.Error("NewAskUser did not install the queueing seam; concurrent children would collide on the host")
	}
	if got := NewAskUser(nil).asker; got != nil {
		t.Errorf("NewAskUser(nil).asker = %#v, want nil", got)
	}
}

// TestAskUser_ExecuteNamesTheAskingSubAgent pins the identity carrier: the ask_user TOOL builds the
// request, one boundary away from the Agent that knows the delegated task, so the engine installs it
// on the call's context and the tool stamps it onto the question. Absent context ⇒ absent name, which
// is the top-level agent's case.
func TestAskUser_ExecuteNamesTheAskingSubAgent(t *testing.T) {
	var seen []domain.AskRequest
	tool := NewAskUser(askerFunc(func(_ context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
		seen = append(seen, req)
		return domain.AskAnswer{Text: "ok"}, nil
	}))
	call := domain.ToolCall{ID: "q1", Tool: "ask_user", Arguments: []byte(`{"question":"which one?"}`)}

	if _, err := tool.Execute(context.Background(), call); err != nil {
		t.Fatalf("Execute (top level): %v", err)
	}
	ctx := domain.WithSubAgentTask(context.Background(), "audit the config loader")
	if _, err := tool.Execute(ctx, call); err != nil {
		t.Fatalf("Execute (sub-agent): %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("the host was asked %d times, want twice", len(seen))
	}
	if seen[0].SubAgentTask != "" {
		t.Errorf("a top-level question named sub-agent task %q, want none", seen[0].SubAgentTask)
	}
	if seen[1].SubAgentTask != "audit the config loader" {
		t.Errorf("a sub-agent's question named %q, want its delegated task", seen[1].SubAgentTask)
	}
}

// askerFunc adapts a function to domain.Asker for the tests above.
type askerFunc func(context.Context, domain.AskRequest) (domain.AskAnswer, error)

func (f askerFunc) Ask(ctx context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	return f(ctx, req)
}
