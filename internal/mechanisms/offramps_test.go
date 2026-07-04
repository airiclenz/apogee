package mechanisms

import (
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// offrampResponse builds a post-response working value with a full LoopView — text, tool calls, the
// tool menu, and a conversation history — the shape the off-ramp shape checks read (they inspect
// the history through resp.View().Conversation(), unlike the Wave-1 repair Mechanisms that need
// only the response). The view is a real domain.Request view so Conversation()/Tools()/LastUser()
// behave exactly as in the loop.
func offrampResponse(history []domain.Message, tools []domain.ToolDef, text string, calls ...domain.ToolCall) *domain.Response {
	view := domain.NewRequest("m", history, tools, domain.Budget{}, 0, nil).View()
	finish := domain.FinishStop
	if len(calls) > 0 {
		finish = domain.FinishToolCalls
	}
	return domain.NewResponse(text, "", calls, finish, view)
}

// readCall is a read_file tool call over path — the progress signal empty_response_recovery counts.
func readCall(id, path string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path})
	return domain.ToolCall{ID: id, Tool: "read_file", Arguments: args}
}

// userMsg / assistantText / assistantCall are terse conversation-history builders for the off-ramp
// trigger tables.
func userMsg(content string) domain.Message {
	return domain.Message{Role: domain.RoleUser, Content: content}
}
func assistantText(content string) domain.Message {
	return domain.Message{Role: domain.RoleAssistant, Content: content}
}
func assistantCall(calls ...domain.ToolCall) domain.Message {
	return domain.Message{Role: domain.RoleAssistant, ToolCalls: calls}
}

// Both off-ramps share the ratified descriptor shape: off-ramp (survives Bypass, ADR 0006 / D5) and
// exempt (ignored by Adaptive Suppression and the Turn Budget, item 3), each a post-response hook
// (catalogue Table A). Asserting the descriptor here is how the mechanism package proves the "both
// fire under Bypass; both ignore self-regulation" guarantees — the dispatch gate that reads these
// fields is exercised in internal/agent.
func TestOffRampDescriptors(t *testing.T) {
	t.Parallel()
	for _, id := range []domain.MechanismID{emptyResponseRecoveryID, toolUseEnforcerID} {
		m := mustBuild(t, id)
		d := m.Descriptor()
		if d.ID != id {
			t.Errorf("Descriptor().ID = %q, want %q", d.ID, id)
		}
		if d.Capability != domain.CapOffRamp {
			t.Errorf("%q Capability = %q, want %q (survives Bypass)", id, d.Capability, domain.CapOffRamp)
		}
		if d.Suppression != domain.SuppressExempt {
			t.Errorf("%q Suppression = %q, want %q (ignores self-regulation)", id, d.Suppression, domain.SuppressExempt)
		}
		if _, ok := m.(domain.PostResponseHook); !ok {
			t.Errorf("%q does not implement PostResponseHook", id)
		}
	}
}

// Registered together, the two off-ramps co-register cleanly — no ordering cycle, no
// incompatibility — and resolve as post-response Mechanisms (catalogue Table A: neither declares an
// ordering constraint, so they fire independently of the response-repair cascade).
func TestOffRampsCoRegister(t *testing.T) {
	t.Parallel()
	registry := domain.NewMechanismRegistry()
	for _, id := range []domain.MechanismID{emptyResponseRecoveryID, toolUseEnforcerID} {
		if err := registry.Add(mustBuild(t, id)); err != nil {
			t.Fatalf("Add(%q): %v", id, err)
		}
	}
	if err := registry.ValidateOrdering(); err != nil {
		t.Fatalf("ValidateOrdering: %v", err)
	}
	if err := registry.ValidateIncompatibilities(); err != nil {
		t.Fatalf("ValidateIncompatibilities: %v", err)
	}
	if got := len(registry.Ordered(domain.HookPostResponse)); got != 2 {
		t.Fatalf("Ordered(post-response) has %d mechanisms, want 2", got)
	}
}
