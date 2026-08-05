package tools

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
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

// askCallRaw builds an ask_user call from a literal JSON argument object, so a test can pin
// what the wire actually carries — including a field being ABSENT rather than explicitly false.
func askCallRaw(t *testing.T, args string) domain.ToolCall {
	t.Helper()
	if !json.Valid([]byte(args)) {
		t.Fatalf("test wrote invalid JSON args: %s", args)
	}
	return domain.ToolCall{ID: "c1", Tool: "ask_user", Arguments: json.RawMessage(args)}
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

func TestAskUser_MultiSelectFlagReachesTheAsker(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args string
		want bool
	}{
		{
			name: "explicit true",
			args: `{"question":"which?","choices":["a","b"],"multi_select":true}`,
			want: true,
		},
		{
			name: "explicit false",
			args: `{"question":"which?","choices":["a","b"],"multi_select":false}`,
			want: false,
		},
		{
			name: "absent is single-select",
			args: `{"question":"which?","choices":["a","b"]}`,
			want: false,
		},
		{
			// multi_select without anything to check is carried through untouched — the Driver
			// simply has nothing to toggle, which is not a validation error.
			name: "true without choices",
			args: `{"question":"which?","multi_select":true}`,
			want: true,
		},
		{
			name: "true with a single choice",
			args: `{"question":"which?","choices":["only"],"multi_select":true}`,
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			asker := &scriptedAsker{answer: "a"}
			res, err := NewAskUser(asker).Execute(context.Background(), askCallRaw(t, tc.args))

			if err != nil {
				t.Fatalf("Execute returned a Go error: %v", err)
			}
			if res.IsError {
				t.Fatalf("Execute result is an error: %q", res.Content)
			}
			if asker.seenReq.MultiSelect != tc.want {
				t.Errorf("asker saw MultiSelect = %v, want %v", asker.seenReq.MultiSelect, tc.want)
			}
		})
	}
}

func TestAskUser_AdvertisesMultiSelect(t *testing.T) {
	t.Parallel()

	tool := NewAskUser(&scriptedAsker{})
	if !strings.Contains(tool.Description(), "set multi_select to true so the human can pick more than one") {
		t.Errorf("description does not offer multi_select to the model: %q", tool.Description())
	}

	var schema struct {
		Required   []string                  `json:"required"`
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	prop, ok := schema.Properties["multi_select"]
	if !ok {
		t.Fatal("schema is missing the multi_select property")
	}
	if prop["type"] != "boolean" {
		t.Errorf("multi_select type = %v, want boolean", prop["type"])
	}
	if len(schema.Required) != 1 || schema.Required[0] != "question" {
		t.Errorf("required = %v, want [question] — multi_select must stay optional", schema.Required)
	}
}

func TestAskUser_MultiLineAnswerRoundTripsVerbatim(t *testing.T) {
	t.Parallel()

	// A multi-select reply is the chosen labels newline-joined (D9 — labels, never indices).
	// The tool forwards AskAnswer.Text byte for byte and never reshapes a multi-line answer.
	const answer = "Fix the nil-check in submitAnswer\nGuard the empty-choices path"
	asker := &scriptedAsker{answer: answer}
	args := `{"question":"which findings?",` +
		`"choices":["Fix the nil-check in submitAnswer","Add the missing layout() call","Guard the empty-choices path"],` +
		`"multi_select":true}`

	res, err := NewAskUser(asker).Execute(context.Background(), askCallRaw(t, args))

	if err != nil {
		t.Fatalf("Execute returned a Go error: %v", err)
	}
	if res.IsError {
		t.Fatalf("Execute result is an error: %q", res.Content)
	}
	if res.Content != answer {
		t.Errorf("answer = %q, want the asker's Text verbatim %q", res.Content, answer)
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
