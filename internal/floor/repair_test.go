package floor

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
	"github.com/airiclenz/apogee/internal/domain/domaintest"
)

// repairMenu is a small tool menu: read_file (no required params) and write_file (path + content
// required) — the surface the repair guard checks a call against.
func repairMenu() []domain.ToolDef {
	writeSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
	return []domain.ToolDef{
		{Name: "read_file"},
		{Name: "write_file", Schema: writeSchema},
	}
}

// callResponse builds a post-response working value carrying calls, produced against the tool menu
// — the shape ToolCallRepair inspects.
func callResponse(tools []domain.ToolDef, calls ...domain.ToolCall) *domain.Response {
	return domain.NewResponse("", "", calls, domain.FinishToolCalls, domaintest.FakeLoopView{ToolMenu: tools})
}

// A call to a tool the model was never shown is repaired with a correction naming the tool and the
// menu.
func TestToolCallRepairUnknownToolYieldsCorrection(t *testing.T) {
	t.Parallel()

	resp := callResponse(repairMenu(), domain.ToolCall{ID: "c1", Tool: "frobnicate", Arguments: json.RawMessage(`{}`)})
	correction, ok := ToolCallRepair(resp, nil)

	if !ok {
		t.Fatal("ToolCallRepair returned ok = false for an unknown tool")
	}
	if !strings.Contains(correction, `function "frobnicate" not in the tool set`) {
		t.Errorf("correction = %q, want it to flag the unknown tool", correction)
	}
	if !strings.Contains(correction, "Available tools: read_file, write_file") {
		t.Errorf("correction = %q, want it to list the available tools", correction)
	}
}

// Empty or non-JSON arguments are the malformed-call case; a missing required parameter is
// reported with the required list; a call with no function name at all short-circuits the rest.
func TestToolCallRepairMalformedAndMissingArgs(t *testing.T) {
	t.Parallel()

	cases := []struct {
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
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			correction, ok := ToolCallRepair(callResponse(repairMenu(), tc.call), nil)
			if !ok {
				t.Fatalf("ToolCallRepair returned ok = false for %s", tc.name)
			}
			if !strings.Contains(correction, tc.want) {
				t.Errorf("correction = %q, want it to contain %q", correction, tc.want)
			}
		})
	}
}

// A well-formed call to a known tool with every required parameter present is a no-op — no
// correction, no retry. So is a plain text answer with no tool calls at all.
func TestToolCallRepairIsANoOpOnAWellFormedResponse(t *testing.T) {
	t.Parallel()

	good := domain.ToolCall{
		ID:        "c1",
		Tool:      "write_file",
		Arguments: json.RawMessage(`{"path":"main.go","content":"package main\n"}`),
	}
	if correction, ok := ToolCallRepair(callResponse(repairMenu(), good), nil); ok || correction != "" {
		t.Errorf("ToolCallRepair(valid call) = (%q, %v), want (\"\", false)", correction, ok)
	}

	textOnly := domain.NewResponse("all done", "", nil, domain.FinishStop, domaintest.FakeLoopView{ToolMenu: repairMenu()})
	if correction, ok := ToolCallRepair(textOnly, nil); ok || correction != "" {
		t.Errorf("ToolCallRepair(text-only) = (%q, %v), want (\"\", false)", correction, ok)
	}
}

// An empty tool menu means the model was shown nothing to check membership against, so an unknown
// NAME is not an issue there — only the arguments are. This is the branch a Driver that ships no
// tools takes, and it must not turn every call into a correction.
func TestToolCallRepairWithNoToolMenuChecksArgumentsOnly(t *testing.T) {
	t.Parallel()

	resp := callResponse(nil, domain.ToolCall{ID: "c1", Tool: "frobnicate", Arguments: json.RawMessage(`{}`)})
	if correction, ok := ToolCallRepair(resp, nil); ok {
		t.Errorf("ToolCallRepair(no menu) = (%q, true), want no correction", correction)
	}
}

// A tool the ENGINE has but this request's menu withdrew is not the guard's business: Plan mode
// offers only what Plan can run, and the mode's own refusal is the answer the model must get. The
// guard stands down on such a call — arguments included — so nothing pre-empts that refusal.
func TestToolCallRepairLeavesAWithdrawnToolToTheMode(t *testing.T) {
	t.Parallel()

	registered := []string{"read_file", "write_file", "shell"}
	cases := []struct {
		name string
		call domain.ToolCall
	}{
		{"well-formed call to a withdrawn tool", domain.ToolCall{ID: "c1", Tool: "shell", Arguments: json.RawMessage(`{"cmd":"ls"}`)}},
		{"withdrawn tool with empty arguments", domain.ToolCall{ID: "c1", Tool: "shell", Arguments: json.RawMessage("")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if correction, ok := ToolCallRepair(callResponse(repairMenu(), tc.call), registered); ok {
				t.Errorf("ToolCallRepair = (%q, true) on a withdrawn tool, want the mode to answer it", correction)
			}
		})
	}
}

// The withdrawal exemption is narrow: a name the engine does not have either is the hallucination
// the guard exists for and is still corrected, and a withdrawn call never shields a malformed
// SIBLING in the same response.
func TestToolCallRepairStillCorrectsAroundAWithdrawnTool(t *testing.T) {
	t.Parallel()

	registered := []string{"read_file", "write_file", "shell"}

	resp := callResponse(repairMenu(), domain.ToolCall{ID: "c1", Tool: "frobnicate", Arguments: json.RawMessage(`{}`)})
	correction, ok := ToolCallRepair(resp, registered)
	if !ok || !strings.Contains(correction, `function "frobnicate" not in the tool set`) {
		t.Errorf("ToolCallRepair(unknown tool) = (%q, %v), want the unknown-tool correction", correction, ok)
	}

	resp = callResponse(repairMenu(),
		domain.ToolCall{ID: "c1", Tool: "shell", Arguments: json.RawMessage(`{"cmd":"ls"}`)},
		domain.ToolCall{ID: "c2", Tool: "write_file", Arguments: json.RawMessage(`{"path":"a.go"}`)},
	)
	correction, ok = ToolCallRepair(resp, registered)
	if !ok || !strings.Contains(correction, `missing required parameter "content"`) {
		t.Errorf("ToolCallRepair(withdrawn + malformed) = (%q, %v), want the sibling's correction", correction, ok)
	}
	if strings.Contains(correction, "shell") {
		t.Errorf("correction = %q, want no complaint about the withdrawn tool", correction)
	}
}
