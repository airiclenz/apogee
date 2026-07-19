package domaintest

import (
	"encoding/json"
	"fmt"

	"github.com/airiclenz/apogee/internal/domain"
)

// ----------------------------------------------------------------------------
// Canned message constructors
// ----------------------------------------------------------------------------
//
// These are the literal message shapes hook tests build — the same literals the
// package-local helpers across internal/mechanisms produced before they became
// delegates. The ConversationBuilder appends exactly these, so a fixture built
// message-by-message and one built fluently are byte-identical.

// UserMessage is a user message carrying text.
func UserMessage(content string) domain.Message {
	return domain.Message{Role: domain.RoleUser, Content: content}
}

// AssistantTextMessage is a text-only assistant message (no tool calls).
func AssistantTextMessage(content string) domain.Message {
	return domain.Message{Role: domain.RoleAssistant, Content: content}
}

// AssistantCallsMessage is an assistant message issuing tool calls (no text).
func AssistantCallsMessage(calls ...domain.ToolCall) domain.Message {
	return domain.Message{Role: domain.RoleAssistant, ToolCalls: calls}
}

// ToolResultMessage is the tool-result message paired to the call identified by
// callID. The tool name and arguments live only on the originating ToolCall
// (ConversationView's pairing contract), so the result carries just the linkage ID
// and the content.
func ToolResultMessage(callID, content string) domain.Message {
	return domain.Message{Role: domain.RoleTool, ToolCallID: callID, Content: content}
}

// ----------------------------------------------------------------------------
// Canned tool-call builders
// ----------------------------------------------------------------------------

// Call is a tool call with Arguments marshalled from args. It panics on an
// unmarshalable args value: this is fixture construction, and a bad fixture should
// fail loudly at build time rather than thread an error through every test table.
func Call(id, tool string, args any) domain.ToolCall {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(fmt.Sprintf("domaintest.Call(%q, %q): marshal args: %v", id, tool, err))
	}
	return domain.ToolCall{ID: id, Tool: tool, Arguments: raw}
}

// ReadCall is a read_file tool call over path — the read-shaped progress signal the
// read-counting Mechanisms (empty_response_recovery, the read-loop family) inspect.
func ReadCall(id, path string) domain.ToolCall {
	return Call(id, "read_file", map[string]string{"path": path})
}

// ----------------------------------------------------------------------------
// ConversationBuilder
// ----------------------------------------------------------------------------

// ConversationBuilder accumulates a conversation fixture fluently. Build with
// NewConversation, chain the append methods, drain with Messages.
type ConversationBuilder struct {
	messages []domain.Message
}

// NewConversation starts an empty conversation fixture.
func NewConversation() *ConversationBuilder { return &ConversationBuilder{} }

// User appends a user message.
func (b *ConversationBuilder) User(content string) *ConversationBuilder {
	b.messages = append(b.messages, UserMessage(content))
	return b
}

// AssistantText appends a text-only assistant message.
func (b *ConversationBuilder) AssistantText(content string) *ConversationBuilder {
	b.messages = append(b.messages, AssistantTextMessage(content))
	return b
}

// AssistantCalls appends an assistant message issuing the given tool calls.
func (b *ConversationBuilder) AssistantCalls(calls ...domain.ToolCall) *ConversationBuilder {
	b.messages = append(b.messages, AssistantCallsMessage(calls...))
	return b
}

// ToolResult appends the tool-result message paired to callID.
func (b *ConversationBuilder) ToolResult(callID, content string) *ConversationBuilder {
	b.messages = append(b.messages, ToolResultMessage(callID, content))
	return b
}

// Messages returns the built conversation. The slice is a copy, so a test may keep
// appending to b without aliasing a fixture it already handed out.
func (b *ConversationBuilder) Messages() []domain.Message {
	return append([]domain.Message(nil), b.messages...)
}

// ----------------------------------------------------------------------------
// FakeLoopView
// ----------------------------------------------------------------------------

// FakeLoopView is a settable domain.LoopView for hook tests. Set only the fields a
// test cares about; the zero value is usable and reports the documented test-fake
// defaults — an empty conversation, no tools, a zero Budget, Turn 0, Depth 0 (the
// LoopView docstring's "a view built without a depth reports 0"), and Fired 0 for
// every Mechanism.
type FakeLoopView struct {
	Messages    []domain.Message           // the history Conversation() serves
	ToolMenu    []domain.ToolDef           // the menu Tools() returns (as a copy)
	BudgetValue domain.Budget              // what Budget() reports
	TurnIndex   int                        // what Turn() reports
	NestDepth   int                        // what Depth() reports (sub-agent nesting, ADR 0013)
	FireCounts  map[domain.MechanismID]int // per-Mechanism Fired counts; nil reports 0
}

// Conversation serves a real domain conversation view over Messages, built through
// the domain's own engine seam (NewRequest), so LastUser / CallByID / ResultFor
// behave exactly as in the loop and can never drift from the production
// implementation. Each call snapshots the current Messages field.
func (v FakeLoopView) Conversation() domain.ConversationView {
	return domain.NewRequest("", v.Messages, nil, domain.Budget{}, 0, nil).View().Conversation()
}

// Tools returns a copy of ToolMenu, matching the production view's aliasing contract.
func (v FakeLoopView) Tools() []domain.ToolDef {
	return append([]domain.ToolDef(nil), v.ToolMenu...)
}

// Budget reports BudgetValue.
func (v FakeLoopView) Budget() domain.Budget { return v.BudgetValue }

// Turn reports TurnIndex.
func (v FakeLoopView) Turn() int { return v.TurnIndex }

// Depth reports NestDepth.
func (v FakeLoopView) Depth() int { return v.NestDepth }

// Fired reports the FireCounts entry for id (0 when absent or the map is nil).
func (v FakeLoopView) Fired(id domain.MechanismID) int { return v.FireCounts[id] }
