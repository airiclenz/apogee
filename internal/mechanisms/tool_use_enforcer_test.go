package mechanisms

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// narrationHistory is the canonical stuck-narration lead-up: an action request the model has
// answered twice with prose, never calling a tool. The last user message carries the action intent
// the enforcer keys on; the two prior assistant replies are text-only.
func narrationHistory() []domain.Message {
	return []domain.Message{
		userMsg("please implement feature X"),
		assistantText("I'll implement feature X."),
		userMsg("continue"),
		assistantText("Here is my plan."),
		userMsg("please implement feature X now"),
	}
}

// The model narrates a third time on an action request it never acted on: the enforcer defers a
// "use a tool" correction (the sim's wording) into the next request — ActionDefer, not ActionRetry
// (offramps.go), because the narration reply is committed and the correction rides forward.
func TestToolUseEnforcerDefersCorrectionOnNarration(t *testing.T) {
	t.Parallel()
	resp := offrampResponse(narrationHistory(), toolMenu(), "I would edit main.go to add the parser.")
	decision := postResponse(t, toolUseEnforcerID, resp)

	if decision.Action != domain.ActionDefer {
		t.Fatalf("Action = %q, want %q", decision.Action, domain.ActionDefer)
	}
	if !strings.Contains(decision.Inject, "You MUST use one of the available tools: read_file, write_file") {
		t.Errorf("Inject = %q, want it to name the available tools", decision.Inject)
	}
	if !strings.Contains(decision.Inject, "Respond with a tool call") {
		t.Errorf("Inject = %q, want the sim's tool-use directive", decision.Inject)
	}
}

// The enforcer fires only on its exact trigger; every other shape is a no-op.
func TestToolUseEnforcerNoOpCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		resp *domain.Response
	}{
		{
			name: "the model acted (the response has a tool call)",
			resp: offrampResponse(narrationHistory(), toolMenu(), "", writeCall("c1", "main.go", "package main\n")),
		},
		{
			name: "empty response (the empty-recovery off-ramp's domain)",
			resp: offrampResponse(narrationHistory(), toolMenu(), ""),
		},
		{
			name: "no tools were offered",
			resp: offrampResponse(narrationHistory(), nil, "I would edit main.go."),
		},
		{
			name: "last user asked a question, not an action",
			resp: offrampResponse([]domain.Message{
				userMsg("implement it"),
				assistantText("Sure."),
				userMsg("continue"),
				assistantText("Working through it."),
				userMsg("what is in main.go?"),
			}, toolMenu(), "It contains the entry point."),
		},
		{
			name: "last user asked for analysis",
			resp: offrampResponse([]domain.Message{
				userMsg("implement it"),
				assistantText("Sure."),
				userMsg("continue"),
				assistantText("Working through it."),
				userMsg("review and fix main.go"),
			}, toolMenu(), "The code looks mostly fine."),
		},
		{
			name: "the model wrote a file recently",
			resp: offrampResponse([]domain.Message{
				userMsg("implement it"),
				assistantText("Sure."),
				userMsg("continue"),
				assistantCall(writeCall("c1", "main.go", "package main\n")),
				userMsg("now finish it"),
			}, toolMenu(), "I have written the file, that should do it."),
		},
		{
			name: "fewer than two assistant replies so far",
			resp: offrampResponse([]domain.Message{
				userMsg("implement feature X"),
				assistantText("I'll get started on feature X."),
				userMsg("please implement feature X now"),
			}, toolMenu(), "I would proceed by editing the parser."),
		},
		{
			name: "the model has used a tool before",
			resp: offrampResponse([]domain.Message{
				userMsg("implement it"),
				assistantCall(readCall("c1", "a.go")),
				userMsg("continue"),
				assistantText("I have read the file."),
				userMsg("now implement it"),
			}, toolMenu(), "I would proceed by editing it."),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decision := postResponse(t, toolUseEnforcerID, tt.resp)
			if decision.Action != "" || decision.Inject != "" {
				t.Errorf("decision = %+v, want the no-op zero decision", decision)
			}
		})
	}
}
