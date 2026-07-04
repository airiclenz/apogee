package mechanisms

import (
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// An empty reply (no text, no tool calls) mid-task asks the loop to re-stream — ActionRetry. The
// hard attempt cap that stops an always-empty model is the loop's maxPostResponseRetries (verified
// in internal/agent); at the Mechanism level the guarantee is that the trigger returns ActionRetry.
func TestEmptyResponseRecoveryRetriesOnEmptyReply(t *testing.T) {
	t.Parallel()
	history := []domain.Message{
		userMsg("please implement the parser"),
		assistantText("Starting on it."),
	}
	resp := offrampResponse(history, toolMenu(), "") // empty: no text, no calls
	decision := postResponse(t, emptyResponseRecoveryID, resp)

	if decision.Action != domain.ActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, domain.ActionRetry)
	}
	if decision.Inject != "" {
		t.Errorf("Inject = %q, want empty (ActionRetry carries no payload)", decision.Inject)
	}
}

// The off-ramp stands down for every response that is not an empty reply worth recovering.
func TestEmptyResponseRecoveryNoOpCases(t *testing.T) {
	t.Parallel()
	// A progress-bearing, action-request history so only the tweaked condition suppresses the fire.
	progress := []domain.Message{
		userMsg("build the feature"),
		assistantText("On it."),
	}
	tests := []struct {
		name string
		resp *domain.Response
	}{
		{
			name: "response has text (the enforcer's domain, not empty)",
			resp: offrampResponse(progress, toolMenu(), "Here is what I found."),
		},
		{
			name: "response has a tool call",
			resp: offrampResponse(progress, toolMenu(), "", readCall("c1", "main.go")),
		},
		{
			name: "no tools were offered",
			resp: offrampResponse(progress, nil, ""),
		},
		{
			name: "no user message to recover for",
			resp: offrampResponse([]domain.Message{assistantText("hello")}, toolMenu(), ""),
		},
		{
			name: "no recent progress — spinning on one file past the early-turn grace",
			resp: offrampResponse([]domain.Message{
				userMsg("do it"),
				assistantCall(readCall("c1", "a.go")),
				assistantCall(readCall("c2", "a.go")),
				assistantCall(readCall("c3", "a.go")),
				assistantCall(readCall("c4", "a.go")),
			}, toolMenu(), ""),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decision := postResponse(t, emptyResponseRecoveryID, tt.resp)
			if decision.Action != "" || decision.Inject != "" {
				t.Errorf("decision = %+v, want the no-op zero decision", decision)
			}
		})
	}
}
