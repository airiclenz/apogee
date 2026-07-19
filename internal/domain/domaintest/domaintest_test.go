package domaintest_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// FakeLoopView implements the public hook-view interface as a plain value — the
// compile-time proof behind D6's "internal implementers of the public interface carry
// no semver cost", and the reason the zero value is directly usable.
var _ domain.LoopView = domaintest.FakeLoopView{}

// The builder must produce the exact literal shapes the delegating package-local
// helpers (internal/mechanisms' readCall / userMsg / assistantText / assistantCall)
// produced before they became delegates — asserted against literal domain.Message
// values, so any drift in the canned constructors breaks here, not in a Mechanism test.
func TestConversationBuilderProducesLiteralShapes(t *testing.T) {
	t.Parallel()
	got := domaintest.NewConversation().
		User("open the file").
		AssistantCalls(domaintest.ReadCall("c1", "main.go")).
		ToolResult("c1", "package main").
		AssistantText("done").
		Messages()
	want := []domain.Message{
		{Role: domain.RoleUser, Content: "open the file"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{
			{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"main.go"}`)},
		}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "package main"},
		{Role: domain.RoleAssistant, Content: "done"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("built conversation = %#v, want %#v", got, want)
	}
}

// Messages returns a copy: draining the builder, then appending more, must not grow
// (or reallocate under) the fixture already handed out.
func TestConversationBuilderMessagesIsACopy(t *testing.T) {
	t.Parallel()
	b := domaintest.NewConversation().User("u1")
	first := b.Messages()
	b.AssistantText("a1")
	if len(first) != 1 {
		t.Fatalf("earlier drain grew with the builder: len = %d, want 1", len(first))
	}
	if first[0].Content != "u1" {
		t.Errorf("earlier drain mutated: Content = %q, want %q", first[0].Content, "u1")
	}
}

// Call marshals arbitrary args into the Arguments JSON.
func TestCallMarshalsArgs(t *testing.T) {
	t.Parallel()
	got := domaintest.Call("c9", "write_file", map[string]string{"path": "a.go"})
	want := domain.ToolCall{ID: "c9", Tool: "write_file", Arguments: json.RawMessage(`{"path":"a.go"}`)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Call = %#v, want %#v", got, want)
	}
}

// The zero value reports the documented test-fake defaults.
func TestFakeLoopViewZeroValue(t *testing.T) {
	t.Parallel()
	var v domaintest.FakeLoopView
	if got := v.Conversation().Len(); got != 0 {
		t.Errorf("Conversation().Len() = %d, want 0", got)
	}
	if _, _, ok := v.Conversation().LastUser(); ok {
		t.Error("Conversation().LastUser() ok = true on an empty conversation, want false")
	}
	if got := v.Tools(); len(got) != 0 {
		t.Errorf("Tools() = %v, want empty", got)
	}
	if got := v.Budget(); got != (domain.Budget{}) {
		t.Errorf("Budget() = %+v, want zero", got)
	}
	if got := v.Turn(); got != 0 {
		t.Errorf("Turn() = %d, want 0", got)
	}
	if got := v.Depth(); got != 0 {
		t.Errorf("Depth() = %d, want 0", got)
	}
	if got := v.Fired("anything"); got != 0 {
		t.Errorf("Fired() = %d, want 0 from a nil FireCounts", got)
	}
}

// Set fields come back through the interface, and the conversation view carries the
// production pairing helpers (LastUser / CallByID / ResultFor) over the set history.
func TestFakeLoopViewReportsSetValues(t *testing.T) {
	t.Parallel()
	call := domaintest.ReadCall("c1", "main.go")
	v := domaintest.FakeLoopView{
		Messages: domaintest.NewConversation().
			User("go").
			AssistantCalls(call).
			ToolResult("c1", "package main").
			Messages(),
		ToolMenu:    []domain.ToolDef{{Name: "read_file"}},
		BudgetValue: domain.Budget{ContextLimit: 4096, Used: 128},
		TurnIndex:   3,
		NestDepth:   1,
		FireCounts:  map[domain.MechanismID]int{"read_loop_interceptor": 2},
	}

	conv := v.Conversation()
	if got := conv.Len(); got != 3 {
		t.Fatalf("Conversation().Len() = %d, want 3", got)
	}
	if msg, i, ok := conv.LastUser(); !ok || i != 0 || msg.Content != "go" {
		t.Errorf("LastUser() = (%+v, %d, %t), want the user message at 0", msg, i, ok)
	}
	if got, i, ok := conv.CallByID("c1"); !ok || i != 1 || !reflect.DeepEqual(got, call) {
		t.Errorf("CallByID(c1) = (%+v, %d, %t), want the read call at 1", got, i, ok)
	}
	if msg, i, ok := conv.ResultFor("c1"); !ok || i != 2 || msg.Content != "package main" {
		t.Errorf("ResultFor(c1) = (%+v, %d, %t), want the result at 2", msg, i, ok)
	}

	if got := v.Tools(); len(got) != 1 || got[0].Name != "read_file" {
		t.Errorf("Tools() = %v, want the one-entry menu", got)
	}
	if got := v.Budget(); got.ContextLimit != 4096 || got.Used != 128 {
		t.Errorf("Budget() = %+v, want ContextLimit 4096 / Used 128", got)
	}
	if got := v.Turn(); got != 3 {
		t.Errorf("Turn() = %d, want 3", got)
	}
	if got := v.Depth(); got != 1 {
		t.Errorf("Depth() = %d, want 1", got)
	}
	if got := v.Fired("read_loop_interceptor"); got != 2 {
		t.Errorf("Fired(read_loop_interceptor) = %d, want 2", got)
	}
	if got := v.Fired("other"); got != 0 {
		t.Errorf("Fired(other) = %d, want 0", got)
	}
}
