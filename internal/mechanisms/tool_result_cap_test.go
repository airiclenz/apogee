package mechanisms

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// capBudget is a Budget whose working window (ContextLimit - ResponseReserve) times CharsPerToken
// times toolResultBudgetFraction yields maxChars — the per-result ceiling the tests size their
// payloads against. window=1000, reserve=200, cpt=4 ⇒ budgetTokens=800, maxChars=800*4*0.4=1280.
func capBudget() domain.Budget {
	return domain.Budget{ContextLimit: 1000, ResponseReserve: 200, CharsPerToken: 4}
}

// lines builds an n-line payload each line `line`, so its length is comfortably over maxChars and it
// has enough lines that head+tail (40) genuinely elides a middle.
func lines(n int, line string) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = line
	}
	return strings.Join(rows, "\n")
}

// buildRequest is a Request carrying msgs and b — the pre-request working value the Mechanism shapes.
func buildRequest(msgs []domain.Message, b domain.Budget) *domain.Request {
	return domain.NewRequest("m", msgs, nil, b, 0, nil)
}

func TestToolResultCapDescriptorAndOrdering(t *testing.T) {
	t.Parallel()
	m, err := newToolResultCap(Deps{})
	if err != nil {
		t.Fatalf("newToolResultCap: %v", err)
	}
	d := m.Descriptor()
	if d.ID != toolResultCapID {
		t.Errorf("ID = %q, want %q", d.ID, toolResultCapID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge (Bypass-disabled context shaper, footnote 2)", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	if o := m.Ordering(); len(o.Before) != 0 || len(o.After) != 1 || o.After[0] != decomposeID {
		t.Errorf("Ordering = %+v, want After [decompose] (§Ordering seed, ratified into Table A 2026-07-04)", o)
	}
	if _, ok := m.(domain.PreRequestHook); !ok {
		t.Error("tool_result_cap does not implement PreRequestHook")
	}
}

// TestToolResultCapTrimsOversizedResult caps an over-budget tool result to head+tail+marker and
// leaves an under-budget one whole — the core capping behaviour.
func TestToolResultCapTrimsOversizedResult(t *testing.T) {
	t.Parallel()
	big := lines(200, "a line of tool output that repeats to blow past the per-result budget ceiling")
	small := "a short, in-budget tool result"
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: big},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: small},
		{Role: domain.RoleAssistant, Content: "thinking about the next step, no tools"},
	}
	// The most recent tool-call Turn is c2's (index 3); its result (index 4) is protected, so an
	// oversized c2 result would still be spared. Make c1 (older, index 2) the one that gets capped.
	req := buildRequest(msgs, capBudget())

	before := req.Revision()
	if err := (toolResultCapMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before {
		t.Fatal("PreRequest booked no mutation; the oversized older result should have been capped")
	}

	got := req.State().Messages
	capped := got[2].Content
	if len(capped) >= len(big) {
		t.Errorf("capped result not trimmed: %d chars, was %d", len(capped), len(big))
	}
	if !strings.Contains(capped, "start_line/end_line") {
		t.Errorf("capped result missing the elision marker:\n%s", capped)
	}
	// Head and tail are preserved: the first and last lines survive around the marker.
	if !strings.HasPrefix(capped, "a line of tool output") {
		t.Errorf("head not preserved: %.40q", capped)
	}
	if got[4].Content != small {
		t.Errorf("in-budget result was altered: %q", got[4].Content)
	}
}

// TestToolResultCapProtectsMostRecentTurn pins that a result from the most recent tool-call Turn is
// never capped even when it is oversized (apogee-sim findMostRecentAssistantTurn protection).
func TestToolResultCapProtectsMostRecentTurn(t *testing.T) {
	t.Parallel()
	big := lines(200, "freshest tool output that is oversized but belongs to the most recent turn")
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: big},
	}
	req := buildRequest(msgs, capBudget())

	if err := (toolResultCapMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != 0 {
		t.Fatal("the most recent Turn's result was capped; it must be protected")
	}
	if got := req.State().Messages[2].Content; got != big {
		t.Error("protected result content changed")
	}
}

// TestToolResultCapInertWhenWindowUnknown pins the no-basis case: a zero Budget (no discovered
// window ⇒ a zero Allocation) yields a zero ceiling, so capping is a no-op even for a huge result.
func TestToolResultCapInertWhenWindowUnknown(t *testing.T) {
	t.Parallel()
	big := lines(500, "huge output that would be capped if there were a budget to cap against")
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: big},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: "recent"},
	}
	req := buildRequest(msgs, domain.Budget{}) // window unknown

	if err := (toolResultCapMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != 0 {
		t.Fatal("capping fired with no discovered window; it must be inert")
	}
	if got := req.State().Messages[2].Content; got != big {
		t.Error("result content changed with an unknown window")
	}
}

// TestToolResultCapBuildsFromCatalogue proves the constructor table row is wired: Build returns a
// working Mechanism under the canonical ID.
func TestToolResultCapBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(toolResultCapID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", toolResultCapID, err)
	}
	if m.Descriptor().ID != toolResultCapID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, toolResultCapID)
	}
}

// TestToolResultCapCeiling pins the arithmetic: maxChars = (window - reserve) * charsPerToken *
// fraction, and a non-positive working window yields a zero ceiling.
func TestToolResultCapCeiling(t *testing.T) {
	t.Parallel()
	if got, want := capMaxChars(capBudget()), 1280; got != want {
		t.Errorf("capMaxChars = %d, want %d ((1000-200)*4*0.4)", got, want)
	}
	if got := capMaxChars(domain.Budget{ContextLimit: 100, ResponseReserve: 100, CharsPerToken: 4}); got != 0 {
		t.Errorf("capMaxChars with no working window = %d, want 0", got)
	}
}

// jsonResult keeps the tool-result-as-JSON case honest: a capped result is still plain text with a
// marker, not required to stay valid JSON (the model is told to re-read for the omitted range).
func TestToolResultCapMarkerIsActionable(t *testing.T) {
	t.Parallel()
	// A read_file-style result the model would want to re-read a range of. A later tool-call Turn
	// (c2) makes c1's result an OLDER result eligible for capping (the most recent Turn is spared).
	content := lines(300, `{"line": "some structured output that is long"}`)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "read it"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"big.json"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: content},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file", Arguments: json.RawMessage(`{"path":"other.go"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: "small recent result"},
		{Role: domain.RoleAssistant, Content: "done reading"},
	}
	req := buildRequest(msgs, capBudget())
	if err := (toolResultCapMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	capped := req.State().Messages[2].Content
	if !strings.Contains(capped, "truncated") || !strings.Contains(capped, "start_line/end_line") {
		t.Errorf("marker not actionable:\n%s", capped)
	}
}
