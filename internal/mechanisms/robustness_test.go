package mechanisms

import (
	"encoding/json"
	"testing"

	"github.com/airiclenz/apogee/internal/domain"
)

// fakeView is a minimal LoopView for building a *domain.Response in a Mechanism test — only the
// tool menu is load-bearing for the Wave-1 Mechanisms (validate reads it; syntax/autofix ignore
// it), so the rest return zero values.
type fakeView struct{ tools []domain.ToolDef }

func (v fakeView) Conversation() domain.ConversationView { return nil }
func (v fakeView) Tools() []domain.ToolDef               { return v.tools }
func (v fakeView) Budget() domain.Budget                 { return domain.Budget{} }
func (v fakeView) Turn() int                             { return 0 }
func (fakeView) Depth() int                              { return 0 }
func (fakeView) Fired(domain.MechanismID) int            { return 0 }

// responseWith builds a post-response working value carrying calls, produced against the tool menu
// — the shape a post-response Mechanism inspects.
func responseWith(tools []domain.ToolDef, calls ...domain.ToolCall) *domain.Response {
	return domain.NewResponse("", "", calls, domain.FinishToolCalls, fakeView{tools: tools})
}

// writeCall is a write_file tool call over path with content — the input syntax and autofix act on.
func writeCall(id, path, content string) domain.ToolCall {
	args, _ := json.Marshal(map[string]string{"path": path, "content": content})
	return domain.ToolCall{ID: id, Tool: "write_file", Arguments: args}
}

// mustBuild constructs a catalogued Mechanism from the production table, as the config surface does.
func mustBuild(t *testing.T, id domain.MechanismID) domain.Mechanism {
	t.Helper()
	m, err := Build(id, Deps{})
	if err != nil {
		t.Fatalf("Build(%q): %v", id, err)
	}
	return m
}

// postResponse fires a built Mechanism's post-response hook once against resp.
func postResponse(t *testing.T, id domain.MechanismID, resp *domain.Response) domain.PostResponseDecision {
	t.Helper()
	hook, ok := mustBuild(t, id).(domain.PostResponseHook)
	if !ok {
		t.Fatalf("mechanism %q does not implement PostResponseHook", id)
	}
	decision, err := hook.PostResponse(t.Context(), resp)
	if err != nil {
		t.Fatalf("%q.PostResponse: %v", id, err)
	}
	return decision
}

// The three Wave-1 Mechanisms share the ratified descriptor shape: response-repair (off under
// Bypass) and strikes-3 (self-regulated), each a post-response hook (catalogue Table A).
func TestWave1Descriptors(t *testing.T) {
	t.Parallel()
	for _, id := range []domain.MechanismID{validateID, syntaxID, autofixID} {
		m := mustBuild(t, id)
		d := m.Descriptor()
		if d.ID != id {
			t.Errorf("Descriptor().ID = %q, want %q", d.ID, id)
		}
		if d.Capability != domain.CapResponseRepair {
			t.Errorf("%q Capability = %q, want %q", id, d.Capability, domain.CapResponseRepair)
		}
		if d.Suppression != domain.SuppressStrikesThree {
			t.Errorf("%q Suppression = %q, want %q", id, d.Suppression, domain.SuppressStrikesThree)
		}
		if _, ok := m.(domain.PostResponseHook); !ok {
			t.Errorf("%q does not implement PostResponseHook", id)
		}
	}
}

// Registered together, the three resolve to the deterministic cascade validate → autofix → syntax
// (catalogue Table A ordering — repair precedes correction), independent of registration order,
// and co-register cleanly (no ordering cycle, no incompatibility).
func TestWave1DeterministicOrder(t *testing.T) {
	t.Parallel()
	registry := domain.NewMechanismRegistry()
	// Register out of cascade order to prove the topo-sort — not insertion order — sets dispatch.
	for _, id := range []domain.MechanismID{syntaxID, autofixID, validateID} {
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

	ordered := registry.Ordered(domain.HookPostResponse)
	got := make([]domain.MechanismID, len(ordered))
	for i, m := range ordered {
		got[i] = m.Descriptor().ID
	}
	want := []domain.MechanismID{validateID, autofixID, syntaxID}
	if len(got) != len(want) {
		t.Fatalf("Ordered = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Ordered = %v, want %v", got, want)
		}
	}
}
