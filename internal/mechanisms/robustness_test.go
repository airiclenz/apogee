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
func (fakeView) ParallelAgents() int                     { return 0 }
func (fakeView) Fired(domain.MechanismID) int            { return 0 }

// toolMenu is a small tool menu: read_file (no required params) and write_file (path + content
// required) — the surface the tool-call validation helpers and the off-ramps read through
// LoopView.Tools().
func toolMenu() []domain.ToolDef {
	writeSchema := json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"},"content":{"type":"string"}},"required":["path","content"]}`)
	return []domain.ToolDef{
		{Name: "read_file"},
		{Name: "write_file", Schema: writeSchema},
	}
}

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

// mustBuild constructs a catalogued Mechanism row from the production table, as the config surface
// does: the row's descriptor and ordering joined with the hook Build constructed.
func mustBuild(t *testing.T, id domain.MechanismID) domain.RegisteredMechanism {
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
	hook, ok := mustBuild(t, id).Hook.(domain.PostResponseHook)
	if !ok {
		t.Fatalf("mechanism %q does not implement PostResponseHook", id)
	}
	decision, err := hook.PostResponse(t.Context(), resp)
	if err != nil {
		t.Fatalf("%q.PostResponse: %v", id, err)
	}
	return decision
}

// The two surviving Wave-1 Mechanisms share the ratified descriptor shape: response-repair (off
// under Bypass) and strikes-3 (self-regulated), each a post-response hook (catalogue Table A). The
// third, the tool-call validator, is a Floor guard now (ADR 0071) and carries no descriptor at all.
func TestWave1Descriptors(t *testing.T) {
	t.Parallel()
	for _, id := range []domain.MechanismID{syntaxID, autofixID} {
		m := mustBuild(t, id)
		d := m.Descriptor
		if d.ID != id {
			t.Errorf("Descriptor.ID = %q, want %q", d.ID, id)
		}
		if d.Capability != domain.CapResponseRepair {
			t.Errorf("%q Capability = %q, want %q", id, d.Capability, domain.CapResponseRepair)
		}
		if d.Suppression != domain.SuppressStrikesThree {
			t.Errorf("%q Suppression = %q, want %q", id, d.Suppression, domain.SuppressStrikesThree)
		}
		if _, ok := m.Hook.(domain.PostResponseHook); !ok {
			t.Errorf("%q does not implement PostResponseHook", id)
		}
	}
}

// Registered together, the two resolve to the deterministic cascade autofix → syntax (catalogue
// Table A ordering — repair precedes correction), independent of registration order, and
// co-register cleanly (no ordering cycle, no incompatibility).
func TestWave1DeterministicOrder(t *testing.T) {
	t.Parallel()
	registry := domain.NewMechanismRegistry()
	// Register out of cascade order to prove the topo-sort — not insertion order — sets dispatch.
	for _, id := range []domain.MechanismID{syntaxID, autofixID} {
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
		got[i] = m.Descriptor.ID
	}
	want := []domain.MechanismID{autofixID, syntaxID}
	if len(got) != len(want) {
		t.Fatalf("Ordered = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Ordered = %v, want %v", got, want)
		}
	}
}

// The two write-detection semantics part company on apogee's own write tools, and the file-operation
// trio (copy_file / move_file / delete_file) is the sharpest case: moving or deleting a file mutates
// the workspace — semantic (b), isFileMutatingTool, must count it — but the call carries no file
// payload to syntax-check or format, so semantic (a), isWriteTool, must not. A trio member counted by
// (a) would send syntax and autofix hunting for content that is not there; one missed by (b) leaves
// the whole history family reading a real write as a non-write (defect a).
func TestWriteDetectionSemanticsSplitOnApogeeWriteTools(t *testing.T) {
	t.Parallel()
	cases := []struct {
		tool      string
		mutating  bool // semantic (b): did this call mutate a file?
		fullWrite bool // semantic (a): does this call carry a full file payload?
	}{
		{"write_file", true, true},               // the sim spelling both semantics share
		{"edit_existing_file", true, false},      // fragment payload — (b) only
		{"single_find_and_replace", true, false}, // patch payload — (b) only
		{"multi_find_and_replace", true, false},  // patch payload — (b) only
		{"copy_file", true, false},               // 2026-08-10 trio: bytes move, no payload
		{"move_file", true, false},               //
		{"delete_file", true, false},             //
		{"read_file", false, false},              // reads mutate nothing
		{"list_dir", false, false},               //
		{"terminal", false, false},               // effects are the model's command, not a named write
	}
	for _, c := range cases {
		t.Run(c.tool, func(t *testing.T) {
			t.Parallel()
			if got := isFileMutatingTool(c.tool); got != c.mutating {
				t.Errorf("isFileMutatingTool(%q) = %v, want %v", c.tool, got, c.mutating)
			}
			if got := isWriteTool(c.tool); got != c.fullWrite {
				t.Errorf("isWriteTool(%q) = %v, want %v", c.tool, got, c.fullWrite)
			}
		})
	}
}
