package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// scriptedAsker answers with a fixed text (or a fixed error), recording the request it saw —
// the hermetic "bench script" responder the P3.11 acceptance calls for.
type scriptedAsker struct {
	answer  string
	err     error
	seen    string            // the question the asker was handed
	seenReq domain.AskRequest // the full request (question + sanitised choices)
}

func (a *scriptedAsker) Ask(_ context.Context, req domain.AskRequest) (domain.AskAnswer, error) {
	a.seen = req.Question
	a.seenReq = req
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

func askCallWithChoices(t *testing.T, question string, choices []string) domain.ToolCall {
	t.Helper()
	args, err := json.Marshal(struct {
		Question string   `json:"question"`
		Choices  []string `json:"choices"`
	}{Question: question, Choices: choices})
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

func TestAskUser_ChoicesPassThroughToAsker(t *testing.T) {
	t.Parallel()

	asker := &scriptedAsker{answer: "green"}
	choices := []string{"red", "green", "blue"}

	res, err := NewAskUser(asker).
		Execute(context.Background(), askCallWithChoices(t, "pick one", choices))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute result is an error: %q", res.Content)
	}
	if !slices.Equal(asker.seenReq.Choices, choices) {
		t.Errorf("asker saw choices %q, want %q", asker.seenReq.Choices, choices)
	}
}

func TestAskUser_WhitespaceOnlyChoicesAreDropped(t *testing.T) {
	t.Parallel()

	asker := &scriptedAsker{answer: "yes"}
	raw := []string{"  yes  ", "   ", "no", ""}

	_, err := NewAskUser(asker).
		Execute(context.Background(), askCallWithChoices(t, "confirm?", raw))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if want := []string{"yes", "no"}; !slices.Equal(asker.seenReq.Choices, want) {
		t.Errorf("sanitised choices = %q, want %q", asker.seenReq.Choices, want)
	}
}

func TestAskUser_AllBlankChoicesYieldNilChoices(t *testing.T) {
	t.Parallel()

	asker := &scriptedAsker{answer: "x"}

	_, err := NewAskUser(asker).
		Execute(context.Background(), askCallWithChoices(t, "q?", []string{"  ", "\t", ""}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if asker.seenReq.Choices != nil {
		t.Errorf("an all-blank choices array should yield nil Choices; got %q", asker.seenReq.Choices)
	}
}

func TestAskUser_AbsentChoicesYieldNilChoices(t *testing.T) {
	t.Parallel()

	asker := &scriptedAsker{answer: "x"}

	_, err := NewAskUser(asker).Execute(context.Background(), askCall(t, "q?"))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if asker.seenReq.Choices != nil {
		t.Errorf("absent choices should behave as today (nil Choices); got %q", asker.seenReq.Choices)
	}
}

func TestAskUser_AnswerIsAskerTextRegardlessOfChoices(t *testing.T) {
	t.Parallel()

	// The human typed a custom answer that matches none of the offered choices; the tool
	// cannot tell a picked label from typed text (D9) and forwards AskAnswer.Text either way.
	asker := &scriptedAsker{answer: "a custom typed reply"}

	res, err := NewAskUser(asker).
		Execute(context.Background(), askCallWithChoices(t, "pick", []string{"a", "b"}))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute result is an error: %q", res.Content)
	}
	if res.Content != "a custom typed reply" {
		t.Errorf("answer = %q, want the asker's Text %q", res.Content, "a custom typed reply")
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
