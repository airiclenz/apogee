package floor

import (
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

// capLines builds an n-line payload each line `line`, so its length is comfortably over maxChars and
// it has enough lines that head+tail (40) genuinely elides a middle.
func capLines(n int, line string) string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = line
	}
	return strings.Join(rows, "\n")
}

// capRequest is a Request carrying msgs and b — the pre-request working value the guard shapes.
func capRequest(msgs []domain.Message, b domain.Budget) *domain.Request {
	return domain.NewRequest("m", msgs, nil, b, 0, nil)
}

// TestCapToolResultsTrimsOversizedResult caps an over-budget tool result to head+tail+marker and
// leaves an under-budget one whole — the core capping behaviour.
func TestCapToolResultsTrimsOversizedResult(t *testing.T) {
	t.Parallel()
	big := capLines(200, "a line of tool output that repeats to blow past the per-result budget ceiling")
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
	req := capRequest(msgs, capBudget())

	before := req.Revision()
	if capped := CapToolResults(req); capped != 1 {
		t.Fatalf("CapToolResults capped %d results, want 1 (the oversized older one)", capped)
	}
	if req.Revision() == before {
		t.Fatal("the guard booked no mutation; the oversized older result should have been capped")
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

// TestCapToolResultsProtectsMostRecentTurn pins that a result from the most recent tool-call Turn is
// never capped even when it is oversized (apogee-sim findMostRecentAssistantTurn protection).
func TestCapToolResultsProtectsMostRecentTurn(t *testing.T) {
	t.Parallel()
	big := capLines(200, "freshest tool output that is oversized but belongs to the most recent turn")
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: big},
	}
	req := capRequest(msgs, capBudget())

	if capped := CapToolResults(req); capped != 0 {
		t.Fatalf("CapToolResults capped %d results; the most recent Turn must be protected", capped)
	}
	if req.Revision() != 0 {
		t.Fatal("the most recent Turn's result was capped; it must be protected")
	}
	if got := req.State().Messages[2].Content; got != big {
		t.Error("protected result content changed")
	}
}

// TestCapToolResultsInertWhenWindowUnknown pins the no-basis case: a zero Budget (no discovered
// window ⇒ a zero Allocation) yields a zero ceiling, so capping is a no-op even for a huge result.
func TestCapToolResultsInertWhenWindowUnknown(t *testing.T) {
	t.Parallel()
	big := capLines(500, "huge output that would be capped if there were a budget to cap against")
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: big},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: "recent"},
	}
	req := capRequest(msgs, domain.Budget{}) // window unknown

	if capped := CapToolResults(req); capped != 0 {
		t.Fatalf("CapToolResults capped %d results with no discovered window; it must be inert", capped)
	}
	if req.Revision() != 0 {
		t.Fatal("capping fired with no discovered window; it must be inert")
	}
	if got := req.State().Messages[2].Content; got != big {
		t.Error("result content changed with an unknown window")
	}
}

// TestCapToolResultsCeiling pins the arithmetic: maxChars = (window - reserve) * charsPerToken *
// fraction, and a non-positive working window yields a zero ceiling.
func TestCapToolResultsCeiling(t *testing.T) {
	t.Parallel()
	if got, want := capMaxChars(capBudget()), 1280; got != want {
		t.Errorf("capMaxChars = %d, want %d ((1000-200)*4*0.4)", got, want)
	}
	if got := capMaxChars(domain.Budget{ContextLimit: 100, ResponseReserve: 100, CharsPerToken: 4}); got != 0 {
		t.Errorf("capMaxChars with no working window = %d, want 0", got)
	}
}

// TestCapToolResultsMarkerIsActionable keeps the tool-result-as-JSON case honest: a capped result is
// still plain text with a marker, not required to stay valid JSON (the model is told to re-read for
// the omitted range).
func TestCapToolResultsMarkerIsActionable(t *testing.T) {
	t.Parallel()
	// A read_file-style result the model would want to re-read a range of. A later tool-call Turn
	// (c2) makes c1's result an OLDER result eligible for capping (the most recent Turn is spared).
	content := capLines(300, `{"line": "some structured output that is long"}`)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "read it"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file", Arguments: json.RawMessage(`{"path":"big.json"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: content},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file", Arguments: json.RawMessage(`{"path":"other.go"}`)}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: "small recent result"},
		{Role: domain.RoleAssistant, Content: "done reading"},
	}
	req := capRequest(msgs, capBudget())
	if capped := CapToolResults(req); capped != 1 {
		t.Fatalf("CapToolResults capped %d results, want 1", capped)
	}
	capped := req.State().Messages[2].Content
	if !strings.Contains(capped, "truncated") || !strings.Contains(capped, "start_line/end_line") {
		t.Errorf("marker not actionable:\n%s", capped)
	}
}

// TestCapMaxCharsFollowsTheWorkingCeiling pins which of the Budget's two ceilings the cap is scaled
// against. On a model advertising a very large window a cap sized to the ADVERTISEMENT is no cap at
// all — the runaway the `working-window:` key exists for — so the ceiling has to be ContextLimit,
// the room the session actually works in, and a bound one must shrink the cap while the advertised
// window beside it stays exactly as large.
func TestCapMaxCharsFollowsTheWorkingCeiling(t *testing.T) {
	t.Parallel()

	const advertised = 1310720

	unbounded := domain.Budget{
		Window: advertised, ContextLimit: advertised, ResponseReserve: 262144, CharsPerToken: 4,
	}
	bounded := domain.Budget{
		Window: advertised, ContextLimit: 200000, ResponseReserve: 40000, CharsPerToken: 4,
	}

	loose, tight := capMaxChars(unbounded), capMaxChars(bounded)

	if want := int(float64(200000-40000) * 4 * toolResultBudgetFraction); tight != want {
		t.Errorf("capMaxChars under a 200k working ceiling = %d, want %d", tight, want)
	}
	if tight >= loose {
		t.Errorf("capMaxChars = %d bounded vs %d unbounded; the working ceiling did not shrink the cap",
			tight, loose)
	}
}

// TestCapToolResultsCountsEveryCappedResult pins the return value the seam gates its event on: two
// oversized older results are both capped and both counted, so one firing is booked for the pass
// rather than one per message.
func TestCapToolResultsCountsEveryCappedResult(t *testing.T) {
	t.Parallel()
	big := capLines(200, "an oversized older tool result that has outgrown its share of the budget")
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: big},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: big},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c3", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c3", Content: "the freshest result, protected"},
	}
	req := capRequest(msgs, capBudget())
	if capped := CapToolResults(req); capped != 2 {
		t.Fatalf("CapToolResults capped %d results, want 2", capped)
	}
}

// TestCapToolResultsNeverGrowsAResult pins the sim's one deliberate departure: a pathological
// few-very-long-lines result the head/tail form cannot shrink is left WHOLE rather than replaced by
// a longer rendering (the sim replaced unconditionally).
func TestCapToolResultsNeverGrowsAResult(t *testing.T) {
	t.Parallel()
	// Two lines, both far over the ceiling: head (20 lines) plus tail (20 lines) covers the whole
	// body, so the elision can only add its marker.
	blob := strings.Repeat("x", 4000) + "\n" + strings.Repeat("y", 4000)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: blob},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c2", Tool: "read_file"}}},
		{Role: domain.RoleTool, ToolCallID: "c2", Content: "recent"},
	}
	req := capRequest(msgs, capBudget())
	if capped := CapToolResults(req); capped != 0 {
		t.Fatalf("CapToolResults capped %d results; a result the elision cannot shrink must be left whole", capped)
	}
	if got := req.State().Messages[2].Content; got != blob {
		t.Errorf("unshrinkable result was rewritten: %d chars, was %d", len(got), len(blob))
	}
}
