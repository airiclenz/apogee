package floor

import (
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// guardResponse builds a post-response working value with a FULL view — text, tool calls, the tool
// menu and a conversation history — the shape the two post-response shape checks in this file and
// emptyreply_test.go read (both inspect the history through resp.View().Conversation(), unlike the
// repair guard which needs only the response). The view is a real domain request view so
// Conversation()/Tools()/LastUser() behave exactly as they do in the loop.
func guardResponse(history []domain.Message, tools []domain.ToolDef, text string, calls ...domain.ToolCall) *domain.Response {
	view := domain.NewRequest("m", history, tools, domain.Budget{}, 0, nil).View()
	finish := domain.FinishStop
	if len(calls) > 0 {
		finish = domain.FinishToolCalls
	}
	return domain.NewResponse(text, "", calls, finish, view)
}

// userMsg / assistantText / assistantCall are terse conversation-history builders for the guard
// trigger tables, delegating to the shared hook-seam test adapter (internal/domain/domaintest).
func userMsg(content string) domain.Message       { return domaintest.UserMessage(content) }
func assistantText(content string) domain.Message { return domaintest.AssistantTextMessage(content) }
func assistantCall(c ...domain.ToolCall) domain.Message {
	return domaintest.AssistantCallsMessage(c...)
}

// readCall is a read_file tool call over path — the progress signal RecoverEmpty counts.
func readCall(id, path string) domain.ToolCall { return domaintest.ReadCall(id, path) }

// guardMenu is the small tool menu the two guards read through LoopView.Tools(): read_file and
// write_file, the names the enforcer's correction lists back to the model.
func guardMenu() []domain.ToolDef {
	return []domain.ToolDef{{Name: "read_file"}, {Name: "write_file"}}
}

// narrationHistory is the canonical stuck-narration lead-up: an action request the model has
// answered twice with prose, never calling a tool. The last user message carries the action intent
// the guard keys on; the two prior assistant replies are text-only.
func narrationHistory() []domain.Message {
	return []domain.Message{
		userMsg("please implement feature X"),
		assistantText("I'll implement feature X."),
		userMsg("continue"),
		assistantText("Here is my plan."),
		userMsg("please implement feature X now"),
	}
}

// The model narrates a third time on an action request it never acted on: the guard hands back the
// "use a tool" correction (the sim's wording), which the engine re-streams the Turn with, carrying
// the superseded narration and the correction (the sim's retryForToolUse exchange).
func TestEnforceToolUseCorrectsNarration(t *testing.T) {
	t.Parallel()
	correction, ok := EnforceToolUse(guardResponse(narrationHistory(), guardMenu(), "I would edit main.go to add the parser."))

	if !ok {
		t.Fatalf("ok = false, want the guard to fire on a third narration")
	}
	if !strings.Contains(correction, "You MUST use one of the available tools: read_file, write_file") {
		t.Errorf("correction = %q, want it to name the available tools", correction)
	}
	if !strings.Contains(correction, "Respond with a tool call") {
		t.Errorf("correction = %q, want the sim's tool-use directive", correction)
	}
}

// The guard fires only on its exact trigger; every other shape is the no-op case.
func TestEnforceToolUseNoOpCases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		resp *domain.Response
	}{
		{
			name: "the model acted (the response has a tool call)",
			resp: guardResponse(narrationHistory(), guardMenu(), "", writeCall("c1", "main.go")),
		},
		{
			name: "empty response (the empty-reply guard's domain)",
			resp: guardResponse(narrationHistory(), guardMenu(), ""),
		},
		{
			name: "no tools were offered",
			resp: guardResponse(narrationHistory(), nil, "I would edit main.go."),
		},
		{
			name: "last user asked a question, not an action",
			resp: guardResponse([]domain.Message{
				userMsg("implement it"),
				assistantText("Sure."),
				userMsg("continue"),
				assistantText("Working through it."),
				userMsg("what is in main.go?"),
			}, guardMenu(), "It contains the entry point."),
		},
		{
			name: "last user asked for analysis",
			resp: guardResponse([]domain.Message{
				userMsg("implement it"),
				assistantText("Sure."),
				userMsg("continue"),
				assistantText("Working through it."),
				userMsg("review and fix main.go"),
			}, guardMenu(), "The code looks mostly fine."),
		},
		{
			name: "the model wrote a file recently",
			resp: guardResponse([]domain.Message{
				userMsg("implement it"),
				assistantText("Sure."),
				userMsg("continue"),
				assistantCall(writeCall("c1", "main.go")),
				userMsg("now finish it"),
			}, guardMenu(), "I have written the file, that should do it."),
		},
		{
			name: "fewer than two assistant replies so far",
			resp: guardResponse([]domain.Message{
				userMsg("implement feature X"),
				assistantText("I'll get started on feature X."),
				userMsg("please implement feature X now"),
			}, guardMenu(), "I would proceed by editing the parser."),
		},
		{
			name: "the model has used a tool before",
			resp: guardResponse([]domain.Message{
				userMsg("implement it"),
				assistantCall(readCall("c1", "a.go")),
				userMsg("continue"),
				assistantText("I have read the file."),
				userMsg("now implement it"),
			}, guardMenu(), "I would proceed by editing it."),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			correction, ok := EnforceToolUse(tt.resp)
			if ok || correction != "" {
				t.Errorf("EnforceToolUse = (%q, %v), want the no-op (\"\", false)", correction, ok)
			}
		})
	}
}
