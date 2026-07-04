package mechanisms

import (
	"context"
	"fmt"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// shaperRequest is a pre-request working value carrying msgs and a tool menu — the shape the
// Wave-3 request shapers narrow/inject against. Budget is zero (these shapers do not read it).
func shaperRequest(msgs []domain.Message, tools []domain.ToolDef) *domain.Request {
	return domain.NewRequest("m", msgs, tools, domain.Budget{}, 0, nil)
}

// genericTools builds n distinctly-named tools with no keyword affinity, so scoring alone leaves
// them tied at zero (the recently-used / analysis-keep paths decide what survives).
func genericTools(n int) []domain.ToolDef {
	ts := make([]domain.ToolDef, n)
	for i := range ts {
		ts[i] = domain.ToolDef{Name: fmt.Sprintf("tool_%02d", i), Description: "a generic tool"}
	}
	return ts
}

func nameSet(tools []domain.ToolDef) map[string]bool {
	names := make(map[string]bool, len(tools))
	for _, t := range tools {
		names[t.Name] = true
	}
	return names
}

func TestToolFilterDescriptorAndOrdering(t *testing.T) {
	t.Parallel()
	m, err := newToolFilter(Deps{})
	if err != nil {
		t.Fatalf("newToolFilter: %v", err)
	}
	d := m.Descriptor()
	if d.ID != toolFilterID {
		t.Errorf("ID = %q, want %q", d.ID, toolFilterID)
	}
	if d.Capability != domain.CapProactiveNudge {
		t.Errorf("Capability = %q, want proactive-nudge", d.Capability)
	}
	if d.Suppression != domain.SuppressStrikesThree {
		t.Errorf("Suppression = %q, want strikes-3", d.Suppression)
	}
	if o := m.Ordering(); len(o.Before) != 1 || o.Before[0] != "decompose" {
		t.Errorf("Ordering = %+v, want Before decompose (catalogue Table A)", o)
	}
	if _, ok := m.(domain.PreRequestHook); !ok {
		t.Error("toolfilter does not implement PreRequestHook")
	}
}

// Below the 30-tool threshold with no hallucination, the filter is inert: the menu is untouched and
// no fire is booked (apogee-sim ToolFilter.Transform Skip path).
func TestToolFilterInertBelowThreshold(t *testing.T) {
	t.Parallel()
	tools := genericTools(12)
	req := shaperRequest([]domain.Message{{Role: domain.RoleUser, Content: "do the thing"}}, tools)
	before := req.Revision()

	if err := (toolFilterMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("filter fired below the threshold with no hallucination; it must be inert")
	}
	if got := len(req.State().Tools); got != 12 {
		t.Errorf("menu size = %d, want 12 (untouched)", got)
	}
}

// A large menu is narrowed to the keep limit, the keyword-relevant tool survives, and the result is
// deterministic across runs (same input ⇒ same output — the item's determinism requirement).
func TestToolFilterNarrowsLargeMenuDeterministically(t *testing.T) {
	t.Parallel()
	tools := append(genericTools(30), domain.ToolDef{Name: "read_file", Description: "read a file from disk"})
	msgs := []domain.Message{{Role: domain.RoleUser, Content: "read the file"}}

	run := func() []domain.ToolDef {
		req := shaperRequest(msgs, tools)
		if err := (toolFilterMechanism{}).PreRequest(context.Background(), req); err != nil {
			t.Fatalf("PreRequest: %v", err)
		}
		return req.State().Tools
	}
	kept := run()
	if len(kept) > toolFilterKeepLimit {
		t.Fatalf("kept %d tools, want <= %d", len(kept), toolFilterKeepLimit)
	}
	if !nameSet(kept)["read_file"] {
		t.Error("the keyword-relevant tool read_file was filtered out")
	}

	second := run()
	if len(second) != len(kept) {
		t.Fatalf("non-deterministic: run1 kept %d, run2 kept %d", len(kept), len(second))
	}
	for i := range kept {
		if kept[i].Name != second[i].Name {
			t.Fatalf("non-deterministic order at %d: %q vs %q", i, kept[i].Name, second[i].Name)
		}
	}
}

// Narrowing is request-scoped: SetTools copies, so the input menu and a second freshly-built request
// still see the full menu (the loop re-sets the menu per request, never mutating it globally).
func TestToolFilterNarrowingIsRequestScoped(t *testing.T) {
	t.Parallel()
	tools := append(genericTools(30), domain.ToolDef{Name: "read_file", Description: "read a file"})
	msgs := []domain.Message{{Role: domain.RoleUser, Content: "read the file"}}

	req := shaperRequest(msgs, tools)
	if err := (toolFilterMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if len(tools) != 31 {
		t.Errorf("input menu mutated: len = %d, want 31", len(tools))
	}
	next := shaperRequest(msgs, tools)
	if got := len(next.View().Tools()); got != 31 {
		t.Errorf("next request sees %d tools, want the full 31 (per-request menu)", got)
	}
}

// A recently-used tool is kept whole even with a zero keyword score, so the model keeps access to
// what it was just using (apogee-sim recentlyUsedTools protection).
func TestToolFilterKeepsRecentlyUsedTool(t *testing.T) {
	t.Parallel()
	tools := genericTools(31)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "keep going"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "tool_05"}}},
		{Role: domain.RoleTool, ToolCallID: "c1", Content: "ok"},
	}
	req := shaperRequest(msgs, tools)
	if err := (toolFilterMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if !nameSet(req.State().Tools)["tool_05"] {
		t.Error("recently-used tool_05 was filtered out; it must be kept")
	}
}

// A hallucinated tool call (a name absent from the menu) activates filtering even below the size
// threshold, provided the menu still exceeds the keep limit (apogee-sim hasToolHallucinations).
func TestToolFilterActivatesOnHallucination(t *testing.T) {
	t.Parallel()
	tools := genericTools(12) // below 30, above the keep limit of 10
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "nonexistent_tool"}}},
	}
	req := shaperRequest(msgs, tools)
	before := req.Revision()
	if err := (toolFilterMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() == before {
		t.Fatal("a hallucinated tool call should have activated filtering")
	}
	if got := len(req.State().Tools); got > toolFilterKeepLimit {
		t.Errorf("kept %d tools, want <= %d", got, toolFilterKeepLimit)
	}
}

// Even with a hallucination, a menu already within the keep limit is left whole (nothing to trim).
func TestToolFilterInertWithinLimit(t *testing.T) {
	t.Parallel()
	tools := genericTools(5)
	msgs := []domain.Message{
		{Role: domain.RoleUser, Content: "go"},
		{Role: domain.RoleAssistant, ToolCalls: []domain.ToolCall{{ID: "c1", Tool: "nonexistent_tool"}}},
	}
	req := shaperRequest(msgs, tools)
	before := req.Revision()
	if err := (toolFilterMechanism{}).PreRequest(context.Background(), req); err != nil {
		t.Fatalf("PreRequest: %v", err)
	}
	if req.Revision() != before {
		t.Fatal("a menu within the keep limit must be left whole")
	}
}

func TestToolFilterBuildsFromCatalogue(t *testing.T) {
	t.Parallel()
	m, err := Build(toolFilterID, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", toolFilterID, err)
	}
	if m.Descriptor().ID != toolFilterID {
		t.Errorf("built ID = %q, want %q", m.Descriptor().ID, toolFilterID)
	}
}
