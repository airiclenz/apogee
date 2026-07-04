package mechanisms

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// toolMenu is a small tool menu: read_file (no required params) and write_file (path + content
// required) — the surface validate checks a call against.
func toolMenu() []domain.ToolDef {
	writeSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
	return []domain.ToolDef{
		{Name: "read_file"},
		{Name: "write_file", Schema: writeSchema},
	}
}

// A call to a tool the model was never shown retries in place (R1) with a correction naming the
// tool and the menu.
func TestValidateUnknownToolRetriesWithCorrection(t *testing.T) {
	t.Parallel()
	resp := responseWith(toolMenu(), domain.ToolCall{ID: "c1", Tool: "frobnicate", Arguments: json.RawMessage(`{}`)})
	decision := postResponse(t, validateID, resp)

	if decision.Action != domain.ActionRetry {
		t.Fatalf("Action = %q, want %q", decision.Action, domain.ActionRetry)
	}
	if !strings.Contains(decision.Inject, `function "frobnicate" not in the tool set`) {
		t.Errorf("Inject = %q, want it to flag the unknown tool", decision.Inject)
	}
	if !strings.Contains(decision.Inject, "Available tools: read_file, write_file") {
		t.Errorf("Inject = %q, want it to list the available tools", decision.Inject)
	}
}

// Empty or non-JSON arguments are the malformed-call case; a missing required parameter is
// reported with the required list.
func TestValidateMalformedAndMissingArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		call domain.ToolCall
		want string
	}{
		{
			name: "empty arguments",
			call: domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: json.RawMessage("")},
			want: "empty arguments",
		},
		{
			name: "invalid JSON",
			call: domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: json.RawMessage(`{"path": `)},
			want: "not valid JSON",
		},
		{
			name: "missing required parameter",
			call: domain.ToolCall{ID: "c1", Tool: "write_file", Arguments: json.RawMessage(`{"path":"x.go"}`)},
			want: `missing required parameter "content"`,
		},
		{
			name: "missing function name",
			call: domain.ToolCall{ID: "c1", Tool: "", Arguments: json.RawMessage(`{}`)},
			want: "missing function name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			decision := postResponse(t, validateID, responseWith(toolMenu(), tt.call))
			if decision.Action != domain.ActionRetry {
				t.Fatalf("Action = %q, want %q", decision.Action, domain.ActionRetry)
			}
			if !strings.Contains(decision.Inject, tt.want) {
				t.Errorf("Inject = %q, want it to contain %q", decision.Inject, tt.want)
			}
		})
	}
}

// A well-formed call to a known tool with every required parameter present is a no-op — no
// correction, no retry.
func TestValidateValidCallIsNoOp(t *testing.T) {
	t.Parallel()
	resp := responseWith(toolMenu(), writeCall("c1", "main.go", "package main\n"))
	decision := postResponse(t, validateID, resp)
	if decision.Action != "" {
		t.Errorf("Action = %q, want the no-op zero decision", decision.Action)
	}
	if decision.Inject != "" {
		t.Errorf("Inject = %q, want empty for a valid call", decision.Inject)
	}
}

// A response with no tool calls (a plain text answer) is never touched by validate.
func TestValidateNoToolCallsIsNoOp(t *testing.T) {
	t.Parallel()
	resp := domain.NewResponse("all done", "", nil, domain.FinishStop, fakeView{tools: toolMenu()})
	decision := postResponse(t, validateID, resp)
	if decision.Action != "" || decision.Inject != "" {
		t.Errorf("decision = %+v, want the no-op zero decision for a text-only response", decision)
	}
}
