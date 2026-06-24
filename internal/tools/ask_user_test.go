package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// scriptedAsker answers with a fixed text (or a fixed error), recording the question it saw —
// the hermetic "bench script" responder the P3.11 acceptance calls for.
type scriptedAsker struct {
	answer string
	err    error
	seen   string
}

func (a *scriptedAsker) Ask(_ context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	a.seen = req.Question
	if a.err != nil {
		return domain.AskAnswer{}, a.err
	}
	return domain.AskAnswer{Text: a.answer}, nil
}

func askCall(t *testing.T, question string) domain.ToolCall {
	t.Helper()
	args, err := json.Marshal(map[string]string{"question": question})
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}
	return domain.ToolCall{ID: "c1", Tool: "ask_user", Arguments: args}
}

func TestAskUser_RoundTripsThroughAsker(t *testing.T) {
	t.Parallel()

	asker := &scriptedAsker{answer: "blue"}
	tool := NewAskUser(asker)

	res, err := tool.Execute(context.Background(), askCall(t, "what colour?"))
	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute result is an error: %q", res.Content)
	}
	if res.Content != "blue" {
		t.Errorf("answer = %q, want %q", res.Content, "blue")
	}
	if asker.seen != "what colour?" {
		t.Errorf("asker saw question %q, want %q", asker.seen, "what colour?")
	}
}

func TestAskUser_IsReadOnly(t *testing.T) {
	t.Parallel()
	if !domain.IsReadOnly(NewAskUser(&scriptedAsker{})) {
		t.Error("ask_user must be read-only (runs in Plan, never gates)")
	}
}

func TestAskUser_IsNotExternalEffect(t *testing.T) {
	t.Parallel()
	if _, ok := domain.Tool(NewAskUser(&scriptedAsker{})).(domain.ExternalEffectTool); ok {
		t.Error("ask_user must NOT be an ExternalEffectTool (the human is not a stubbable service)")
	}
}

func TestAskUser_EmptyQuestionIsResultError(t *testing.T) {
	t.Parallel()
	res, err := NewAskUser(&scriptedAsker{}).Execute(context.Background(), askCall(t, "  "))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("an empty question should be a result-level error")
	}
}

func TestAskUser_NilAskerIsGracefulResultError(t *testing.T) {
	t.Parallel()
	res, err := NewAskUser(nil).Execute(context.Background(), askCall(t, "anything?"))
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Error("a nil Asker should yield a graceful result error, not a panic or Go error")
	}
}

func TestAskUser_CancelledCtxIsGoError(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewAskUser(&scriptedAsker{answer: "x"}).Execute(ctx, askCall(t, "q?"))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled ctx should be a Go error (context.Canceled); got %v", err)
	}
}

func TestAskUser_AskerErrorIsResultError(t *testing.T) {
	t.Parallel()
	res, err := NewAskUser(&scriptedAsker{err: errors.New("delegate down")}).
		Execute(context.Background(), askCall(t, "q?"))
	if err != nil {
		t.Fatalf("a delegate error (not a cancellation) should be a result error, not a Go error; got %v", err)
	}
	if !res.IsError {
		t.Error("an Asker error should surface as a result-level error")
	}
}
