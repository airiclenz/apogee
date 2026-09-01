package stubllm

import (
	"strings"
	"testing"
)

// wrapUpDirective is the kind of system text a `when.system` turn recognises: a directive the
// engine announced on ONE request, which is all that separates that request from the one before
// it when the tool menu is empty and the conversation text is unchanged.
const wrapUpDirective = "Your tools have been withdrawn. Report to the agent that delegated the task."

// systemRequest is a request carrying system text and one identical user message — the shape a
// tool-less wrap-up round arrives in, where only the system text tells the two rounds apart.
func systemRequest(system string) Request {
	return Request{Messages: []Message{
		{Role: "system", Content: system},
		{Role: "user", Content: "list the files"},
	}}
}

// TestMatcherSelectsBySystemText pins why when.system exists: two turns whose `when:` differ
// only in `system:` answer two requests that differ only in their system text. Neither
// `last_message` nor `tool_result` can tell those two requests apart.
func TestMatcherSelectsBySystemText(t *testing.T) {
	t.Parallel()

	m, err := newMatcher(Script{Turns: []Turn{
		{When: &Match{System: "Your tools have been withdrawn"}, Text: "wrapping up"},
		{When: &Match{System: "You may call tools"}, Text: "calling a tool"},
	}})
	if err != nil {
		t.Fatalf("new matcher: %v", err)
	}

	working := m.next(systemRequest("You may call tools freely."))
	wrapUp := m.next(systemRequest(wrapUpDirective))

	if working != 1 {
		t.Errorf("working request took turn %d, want 1", working)
	}
	if wrapUp != 0 {
		t.Errorf("wrap-up request took turn %d, want 0", wrapUp)
	}
}

// TestMatcherRequiresEverySetWhenMember pins the conjunction: a turn setting `system:` beside
// `tool_result:` answers only the request where both hold, so a discriminator can be narrowed
// rather than merely swapped.
func TestMatcherRequiresEverySetWhenMember(t *testing.T) {
	t.Parallel()

	call := Message{Role: "assistant", ToolCalls: []ToolCall{{ID: "tc_1", Name: "list_dir"}}}
	result := Message{Role: "tool", ToolCallID: "tc_1", Content: "one file"}

	for _, tc := range []struct {
		name     string
		request  Request
		wantTurn int
	}{
		{
			name:     "both members hold",
			request:  Request{Messages: []Message{{Role: "system", Content: wrapUpDirective}, call, result}},
			wantTurn: 0,
		},
		{
			name:     "the system text does not hold",
			request:  Request{Messages: []Message{{Role: "system", Content: "You may call tools."}, call, result}},
			wantTurn: 1,
		},
		{
			name:     "the tool result does not hold",
			request:  Request{Messages: []Message{{Role: "system", Content: wrapUpDirective}, {Role: "user", Content: "hi"}}},
			wantTurn: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			m, err := newMatcher(Script{Turns: []Turn{
				{When: &Match{System: "tools have been withdrawn", ToolResult: "list_dir"}, Text: "both"},
				{Text: "the ordered fallback"},
			}})
			if err != nil {
				t.Fatalf("new matcher: %v", err)
			}

			if got := m.next(tc.request); got != tc.wantTurn {
				t.Errorf("request took turn %d, want %d", got, tc.wantTurn)
			}
		})
	}
}

// TestMatcherRejectsAnUncompilableSystemRegexp pins that a broken `system:` is reported at
// construction and names the turn it is on, rather than surfacing later as an unmatched request
// whose real cause is a line of YAML somewhere else.
func TestMatcherRejectsAnUncompilableSystemRegexp(t *testing.T) {
	t.Parallel()

	_, err := newMatcher(Script{Turns: []Turn{
		{Text: "first"},
		{When: &Match{System: "(unclosed"}, Text: "second"},
	}})

	if err == nil {
		t.Fatalf("new matcher = nil error, want a failure naming turn 1")
	}
	if got := err.Error(); !strings.Contains(got, "turn 1: when.system:") {
		t.Errorf("error = %q, want it to name turn 1 and when.system", got)
	}
}

// TestMatchValidationAcceptsSystemAlone pins that `system:` is a discriminator in its own right
// — a fixture matching only on the directive the engine announced needs no second member — while
// a `when:` block setting nothing at all is still refused.
func TestMatchValidationAcceptsSystemAlone(t *testing.T) {
	t.Parallel()

	if err := (Match{System: "tools have been withdrawn"}).validate(); err != nil {
		t.Errorf("validate a system-only when block = %v, want nil", err)
	}

	err := (Match{}).validate()

	if err == nil {
		t.Fatalf("validate an empty when block = nil error, want a refusal")
	}
	if got := err.Error(); !strings.Contains(got, "last_message") ||
		!strings.Contains(got, "tool_result") || !strings.Contains(got, "system") {
		t.Errorf("refusal = %q, want it to name all three members", got)
	}
}
